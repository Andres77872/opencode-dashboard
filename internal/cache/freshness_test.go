package cache

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// backdateLastSynced rewrites the source's last_synced_ms so a read sees the
// cache as stale and triggers a background consolidation.
func backdateLastSynced(t *testing.T, store *Store, sourceID string, age time.Duration) {
	t.Helper()
	ms := time.Now().Add(-age).UnixMilli()
	if _, err := store.db.ExecContext(context.Background(), `UPDATE source_state SET last_synced_ms = ? WHERE source_id = ?`, ms, sourceID); err != nil {
		t.Fatalf("backdate last_synced_ms: %v", err)
	}
	store.invalidateStateMemo(sourceID)
}

// TestReadsServeRecentActivityFromLiveGap is the core hybrid-read contract:
// activity newer than the finality cutoff is served live and merged, without
// consolidating anything.
func TestReadsServeRecentActivityFromLiveGap(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("finalized", now.Add(-10*time.Hour), 0.01)}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	statusBefore, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	cached := WrapSource(store, src)
	t.Cleanup(func() { _ = cached.Close() })

	// New raw activity appears inside the un-finalized window.
	src.messages = append(src.messages, testMessage("in-flight", now.Add(-30*time.Minute), 0.02))
	src.scannedFiles++

	overview, err := cached.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	if overview.Messages != 2 {
		t.Fatalf("merged message count = %d, want 2 (cache + live gap)", overview.Messages)
	}
	if overview.Sessions != 1 {
		t.Fatalf("merged session count = %d, want 1 (session spans the cutoff, deduped)", overview.Sessions)
	}

	// The cache itself must not have consolidated the in-flight message, and
	// no consolidation may have been triggered by the fresh read.
	assertCachedMessageCount(t, store, 1)
	statusAfter, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if statusAfter.LastSynced != statusBefore.LastSynced {
		t.Fatalf("fresh read triggered a consolidation (last_synced %d -> %d)", statusBefore.LastSynced, statusAfter.LastSynced)
	}
}

// TestStaleCacheTriggersBackgroundConsolidation: when the last sync is older
// than consolidationStaleness, a read spawns a background consolidation and
// still returns immediately with cache + gap data.
func TestStaleCacheTriggersBackgroundConsolidation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	oldCutoff := now.Add(-10 * time.Hour).Truncate(time.Hour)
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("ancient", now.Add(-12*time.Hour), 0.01)}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: oldCutoff}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	cached := WrapSource(store, src)
	t.Cleanup(func() { _ = cached.Close() })

	// New finalizable activity appears after the old cutoff; the cache is stale.
	src.messages = append(src.messages, testMessage("finalizable", now.Add(-8*time.Hour), 0.02))
	src.scannedFiles++
	backdateLastSynced(t, store, syncFakeSourceID, consolidationStaleness+time.Hour)

	overview, err := cached.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	if overview.Messages != 2 {
		t.Fatalf("merged message count = %d, want 2 (gap covers the stale region)", overview.Messages)
	}

	// The background consolidation lands without any further reads.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_index WHERE source_id = ?`, syncFakeSourceID).Scan(&count); err != nil {
			t.Fatalf("count cached messages: %v", err)
		}
		if count == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background consolidation never landed")
}

// TestGapReadFailureDegradesToCacheOnly: a failing live source must not fail
// the read; the consolidated data is served and the error surfaces via the
// fill-state status API.
func TestGapReadFailureDegradesToCacheOnly(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("finalized", now.Add(-10*time.Hour), 0.01)}}
	store := newTestStore(t)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	cached := WrapSource(store, src)
	t.Cleanup(func() { _ = cached.Close() })

	src.messagesErr = fmt.Errorf("transient parse failure")
	overview, err := cached.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview() must not fail when the live gap read fails: %v", err)
	}
	if overview.Messages != 1 {
		t.Fatalf("degraded message count = %d, want 1 (cache only)", overview.Messages)
	}
	state, ok := store.FillState(syncFakeSourceID)
	if !ok || state.ErrMsg == "" {
		t.Fatalf("gap read failure never surfaced via FillState: %#v", state)
	}
	if state.AttemptMS == 0 {
		t.Fatalf("fill state missing attempt timestamp: %#v", state)
	}
}

func TestCachedSourceConcurrentReadsDuringSync(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-12 * time.Hour).Truncate(time.Hour)
	src := &syncFakeSource{messages: []stats.MessageEntry{
		testMessage("a", base.Add(-2*time.Hour), 0.01),
		testMessage("b", base.Add(-90*time.Minute), 0.02),
	}}
	store := newTestStore(t)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	cached := WrapSource(store, src)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if _, err := cached.Overview(ctx, stats.PeriodQuery{Period: "all"}); err != nil {
					t.Errorf("concurrent Overview() failed: %v", err)
				}
				if _, err := cached.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 10, syncSort); err != nil {
					t.Errorf("concurrent Messages() failed: %v", err)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 3; j++ {
			if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Mode: SyncModeRebuild}); err != nil {
				t.Errorf("concurrent rebuild failed: %v", err)
			}
		}
	}()
	wg.Wait()
	if err := cached.Close(); err != nil {
		t.Fatalf("Close() after concurrent reads failed: %v", err)
	}
}

func TestOpenMigratesLegacySourceState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE source_state (
			source_id TEXT PRIMARY KEY,
			label TEXT NOT NULL,
			kind TEXT NOT NULL,
			path TEXT,
			path_source TEXT,
			available INTEGER NOT NULL DEFAULT 0,
			diagnostics_json TEXT,
			cost_policy_json TEXT,
			privacy_json TEXT,
			source_info_json TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			status TEXT NOT NULL,
			reason TEXT,
			last_synced_ms INTEGER NOT NULL,
			last_safe_cutoff_ms INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO source_state (
			source_id, label, kind, source_info_json, fingerprint, status, last_synced_ms, last_safe_cutoff_ms
		) VALUES ('legacy', 'Legacy', 'jsonl', '{}', 'fp', 'ready', 1000, 123);
	`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() on legacy db failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	status, ok, err := store.SourceStatus(ctx, "legacy")
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if !ok {
		t.Fatalf("legacy source_state row missing after migration")
	}
	if status.LastSafeCutoff != 123 {
		t.Fatalf("LastSafeCutoff = %d, want 123 preserved", status.LastSafeCutoff)
	}
	if status.FreshThrough != 0 {
		t.Fatalf("FreshThrough = %d, want 0 for legacy rows (forces a heal collect)", status.FreshThrough)
	}
}
