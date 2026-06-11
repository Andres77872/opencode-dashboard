package cache

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestIncrementalSyncConsolidatesOnlyFinalizedWindow(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldMessage := testMessage("old-message", base.Add(-8*time.Hour), 0.01)
	recentMessage := testMessage("recent-message", base.Add(-4*time.Hour), 0.02)
	src := &syncFakeSource{messages: []stats.MessageEntry{oldMessage, recentMessage}}
	store := newTestStore(t)

	first, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{
		Mode:   SyncModeIncremental,
		Cutoff: base.Add(-6 * time.Hour),
	})
	if err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}
	if first.Messages != 1 {
		t.Fatalf("first sync report = %#v, want only the finalized message cached", first)
	}
	assertCachedMessageCount(t, store, 1)

	status, ok, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if !ok {
		t.Fatalf("SourceStatus() missing %s", syncFakeSourceID)
	}
	if status.LastSafeCutoff != base.Add(-6*time.Hour).UnixMilli() {
		t.Fatalf("LastSafeCutoff = %d, want %d", status.LastSafeCutoff, base.Add(-6*time.Hour).UnixMilli())
	}
	if status.FreshThrough != status.LastSafeCutoff {
		t.Fatalf("FreshThrough = %d, want == LastSafeCutoff %d", status.FreshThrough, status.LastSafeCutoff)
	}

	// The cutoff advances; the previously recent message becomes finalized.
	src.scannedFiles++
	second, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{
		Mode:   SyncModeIncremental,
		Cutoff: base.Add(-3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("second SyncSourceWithOptions() failed: %v", err)
	}
	if !second.Since.Equal(base.Add(-6 * time.Hour)) {
		t.Fatalf("second sync since = %s, want last safe cutoff %s", second.Since, base.Add(-6*time.Hour))
	}
	if second.Messages != 1 {
		t.Fatalf("second sync report = %#v, want the newly finalized message collected", second)
	}
	assertCachedMessageCount(t, store, 2)

	status, _, err = store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if status.LastSafeCutoff != base.Add(-3*time.Hour).UnixMilli() {
		t.Fatalf("LastSafeCutoff = %d, want advanced to %d", status.LastSafeCutoff, base.Add(-3*time.Hour).UnixMilli())
	}
}

// TestCollectSkipsRowsOutsideWindow exercises the in-Go [Since, Cutoff)
// guards with a live source that ignores window hints entirely.
func TestCollectSkipsRowsOutsideWindow(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := &syncFakeSource{ignoreWindows: true, messages: []stats.MessageEntry{
		testMessage("old", base.Add(-8*time.Hour), 0.01),
		testMessage("mid", base.Add(-4*time.Hour), 0.02),
		testMessage("new", base.Add(-1*time.Hour), 0.04),
	}}
	store := newTestStore(t)

	first, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-6 * time.Hour)})
	if err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}
	if first.Messages != 1 || first.SkippedRecent != 2 || first.SkippedOld != 0 {
		t.Fatalf("first sync report = %#v, want 1 cached and 2 skipped recent", first)
	}
	assertCachedMessageCount(t, store, 1)

	src.scannedFiles++
	second, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-3 * time.Hour)})
	if err != nil {
		t.Fatalf("second SyncSourceWithOptions() failed: %v", err)
	}
	if second.Messages != 1 || second.SkippedOld != 1 || second.SkippedRecent != 1 {
		t.Fatalf("second sync report = %#v, want mid collected, old skipped-old, new skipped-recent", second)
	}
	assertCachedMessageCount(t, store, 2)
}

// TestBoundaryMessageExcludedThenConsolidated pins the half-open [Since,
// Cutoff) convention: a message exactly at the cutoff belongs to the live gap
// until the cutoff passes it, then consolidates exactly once.
func TestBoundaryMessageExcludedThenConsolidated(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	src := &syncFakeSource{messages: []stats.MessageEntry{
		testMessage("older", base.Add(-8*time.Hour), 0.01),
		testMessage("at-cutoff", cutoff, 0.02),
	}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}
	assertCachedMessageCount(t, store, 1)

	src.scannedFiles++
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-5 * time.Hour)}); err != nil {
		t.Fatalf("second SyncSourceWithOptions() failed: %v", err)
	}
	assertCachedMessageCount(t, store, 2)

	// Further syncs never duplicate or drop the boundary row.
	for i := 0; i < 2; i++ {
		src.scannedFiles++
		if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-4 * time.Hour)}); err != nil {
			t.Fatalf("sync %d failed: %v", i, err)
		}
		assertCachedMessageCount(t, store, 2)
	}
}

func TestSyncNeverCachesRowsNewerThanCutoff(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	src := &syncFakeSource{messages: []stats.MessageEntry{
		testMessage("finalized", now.Add(-10*time.Hour), 0.01),
		testMessage("in-flight", now.Add(-30*time.Minute), 0.02),
	}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{}); err != nil {
		t.Fatalf("SyncSourceWithOptions() failed: %v", err)
	}
	status, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if status.LastSafeCutoff%3600000 != 0 {
		t.Fatalf("cutoff %d is not hour-aligned", status.LastSafeCutoff)
	}
	var maxMs int64
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(time_created_ms), 0) FROM message_index WHERE source_id = ?`, syncFakeSourceID).Scan(&maxMs); err != nil {
		t.Fatalf("max time query failed: %v", err)
	}
	if maxMs >= status.LastSafeCutoff {
		t.Fatalf("cached row at %d is not strictly before cutoff %d", maxMs, status.LastSafeCutoff)
	}
	if maxMs == 0 {
		t.Fatalf("finalized message was not cached")
	}
}

// TestFirstSyncPrunesLegacyUnfinalizedRows covers the v3→v4 migration: caches
// that mirrored the un-finalized recent window lose those rows on the first
// fingerprint-mismatch consolidation.
func TestFirstSyncPrunesLegacyUnfinalizedRows(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("finalized", base.Add(-8*time.Hour), 0.01)}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	// Simulate a pre-v4 cache: a mirrored un-finalized row beyond the cutoff
	// and a stale fingerprint.
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO message_index (source_id, message_id, session_id, session_title, role, time_created_ms, cost, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens)
		VALUES (?, 'legacy-unfinalized', 'session-1', 't', 'assistant', ?, 0.5, 1, 1, 0, 0, 0)
	`, syncFakeSourceID, base.Add(-1*time.Hour).UnixMilli()); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE source_state SET fingerprint = 'stale-v3' WHERE source_id = ?`, syncFakeSourceID); err != nil {
		t.Fatalf("stale fingerprint: %v", err)
	}
	store.invalidateStateMemo(syncFakeSourceID)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("migration sync failed: %v", err)
	}
	var maxMs int64
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(time_created_ms), 0) FROM message_index WHERE source_id = ?`, syncFakeSourceID).Scan(&maxMs); err != nil {
		t.Fatalf("max time query failed: %v", err)
	}
	if maxMs >= cutoff.UnixMilli() {
		t.Fatalf("legacy un-finalized row survived migration: max %d >= cutoff %d", maxMs, cutoff.UnixMilli())
	}
	assertCachedMessageCount(t, store, 1)
}

func TestShortCircuitAdvancesWatermarksWithoutCollect(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("only", base.Add(-8*time.Hour), 0.01)}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-6 * time.Hour)}); err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}
	collected := src.messagesCalls

	// Raw unchanged: the second sync must not re-collect, only advance watermarks.
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-5 * time.Hour)}); err != nil {
		t.Fatalf("second SyncSourceWithOptions() failed: %v", err)
	}
	if src.messagesCalls != collected {
		t.Fatalf("messages collected again (%d calls, was %d) despite unchanged fingerprint", src.messagesCalls, collected)
	}
	status, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	want := base.Add(-5 * time.Hour).UnixMilli()
	if status.LastSafeCutoff != want || status.FreshThrough != want {
		t.Fatalf("watermarks = %d/%d, want both advanced to %d", status.LastSafeCutoff, status.FreshThrough, want)
	}
}

func TestLegacyRowWithZeroFreshThroughForcesCollect(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := &syncFakeSource{messages: []stats.MessageEntry{
		testMessage("final", base.Add(-8*time.Hour), 0.01),
	}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-6 * time.Hour)}); err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}
	// Simulate a cache written before fresh_through_ms existed.
	if _, err := store.db.ExecContext(ctx, `UPDATE source_state SET fresh_through_ms = 0 WHERE source_id = ?`, syncFakeSourceID); err != nil {
		t.Fatalf("reset fresh_through_ms: %v", err)
	}
	store.invalidateStateMemo(syncFakeSourceID)
	collected := src.messagesCalls

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-6 * time.Hour)}); err != nil {
		t.Fatalf("second SyncSourceWithOptions() failed: %v", err)
	}
	if src.messagesCalls == collected {
		t.Fatalf("legacy row with fresh_through_ms = 0 must force a collect")
	}
	status, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if status.FreshThrough != base.Add(-6*time.Hour).UnixMilli() {
		t.Fatalf("FreshThrough = %d, want healed to %d", status.FreshThrough, base.Add(-6*time.Hour).UnixMilli())
	}
}

func TestReadTriggeredSyncNeverWipesOrFailsState(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := &syncFakeSource{messages: []stats.MessageEntry{
		testMessage("final", base.Add(-8*time.Hour), 0.01),
		testMessage("recent", base.Add(-7*time.Hour), 0.02),
	}}
	store := newTestStore(t)

	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-6 * time.Hour)}); err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}

	// Transient unavailability on the read path must not touch cached rows.
	src.available = boolPtr(false)
	src.scannedFiles++
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{ReadTriggered: true, Cutoff: base.Add(-6 * time.Hour)}); err != nil {
		t.Fatalf("read-triggered sync on unavailable source errored: %v", err)
	}
	assertCachedMessageCount(t, store, 2)
	status, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if status.Status != "ready" {
		t.Fatalf("status after read-triggered unavailable = %q, want ready", status.Status)
	}

	// A collect failure on the read path must not record an error state.
	src.available = boolPtr(true)
	src.messagesErr = fmt.Errorf("transient parse failure")
	src.scannedFiles++
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{ReadTriggered: true, Cutoff: base.Add(-6 * time.Hour)}); err == nil {
		t.Fatalf("read-triggered sync should surface the collect error")
	}
	assertCachedMessageCount(t, store, 2)
	status, _, err = store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if status.Status != "ready" {
		t.Fatalf("status after read-triggered failure = %q, want ready", status.Status)
	}

	// An explicit sync failure records the error but keeps the watermarks.
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: base.Add(-5 * time.Hour)}); err == nil {
		t.Fatalf("explicit sync should surface the collect error")
	}
	status, _, err = store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	}
	if status.Status != "error" {
		t.Fatalf("status after explicit failure = %q, want error", status.Status)
	}
	if status.LastSafeCutoff != base.Add(-6*time.Hour).UnixMilli() {
		t.Fatalf("failure reset cutoff to %d, want preserved %d", status.LastSafeCutoff, base.Add(-6*time.Hour).UnixMilli())
	}
}

func TestCollectPassesWindowHintsToLiveSource(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("final", base.Add(-8*time.Hour), 0.01)}}
	store := newTestStore(t)

	cutoff := base.Add(-6 * time.Hour)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("first SyncSourceWithOptions() failed: %v", err)
	}
	if len(src.messageQueries) == 0 || src.messageQueries[0].Period != "all" || src.messageQueries[0].From != "" {
		t.Fatalf("first sync message query = %#v, want full history (period all)", src.messageQueries)
	}
	if !src.messageQueries[0].ToTime.Equal(cutoff) {
		t.Fatalf("first sync ToTime = %s, want bounded at cutoff %s", src.messageQueries[0].ToTime, cutoff)
	}

	src.messages = append(src.messages, testMessage("recent", base.Add(-5*time.Hour), 0.02))
	src.scannedFiles++
	newCutoff := base.Add(-4 * time.Hour)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: newCutoff}); err != nil {
		t.Fatalf("second SyncSourceWithOptions() failed: %v", err)
	}
	wantFrom := cutoff.Format("2006-01-02")
	lastPQ := src.messageQueries[len(src.messageQueries)-1]
	if lastPQ.From != wantFrom || lastPQ.Period != "" {
		t.Fatalf("incremental message query = %#v, want From=%s", lastPQ, wantFrom)
	}
	if !lastPQ.FromTime.Equal(cutoff) || !lastPQ.ToTime.Equal(newCutoff) {
		t.Fatalf("incremental message window = [%s, %s), want [%s, %s)", lastPQ.FromTime, lastPQ.ToTime, cutoff, newCutoff)
	}
	lastSQ := src.sessionQueries[len(src.sessionQueries)-1]
	if lastSQ.From != wantFrom || !lastSQ.FromTime.Equal(cutoff) {
		t.Fatalf("incremental session query = %#v, want From=%s/FromTime=%s", lastSQ, wantFrom, cutoff)
	}
	assertCachedMessageCount(t, store, 2)
}

func TestSyncEmitsProgressPhases(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := &syncFakeSource{messages: []stats.MessageEntry{
		testMessage("a", base.Add(-8*time.Hour), 0.01),
		testMessage("b", base.Add(-4*time.Hour), 0.02),
	}}
	store := newTestStore(t)

	var events []SyncProgress
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{
		Cutoff:   base.Add(-3 * time.Hour),
		Progress: func(p SyncProgress) { events = append(events, p) },
	}); err != nil {
		t.Fatalf("SyncSourceWithOptions() failed: %v", err)
	}

	var phases []string
	for _, e := range events {
		if e.SourceID != syncFakeSourceID {
			t.Fatalf("progress source = %q, want %q", e.SourceID, syncFakeSourceID)
		}
		if len(phases) == 0 || phases[len(phases)-1] != e.Phase {
			phases = append(phases, e.Phase)
		}
	}
	want := []string{"sessions", "messages", "write"}
	if len(phases) != len(want) {
		t.Fatalf("progress phases = %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("progress phases = %v, want %v", phases, want)
		}
	}

	var lastMessages SyncProgress
	for _, e := range events {
		if e.Phase == "messages" {
			lastMessages = e
		}
	}
	if lastMessages.Done != int64(len(src.messages)) || lastMessages.Total != int64(len(src.messages)) {
		t.Fatalf("final messages progress = %d/%d, want %d/%d", lastMessages.Done, lastMessages.Total, len(src.messages), len(src.messages))
	}
}

func assertCachedMessageCount(t *testing.T, store *Store, want int64) {
	t.Helper()
	var got int64
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM message_index WHERE source_id = ?`, syncFakeSourceID).Scan(&got); err != nil {
		t.Fatalf("count cached messages: %v", err)
	}
	if got != want {
		t.Fatalf("cached message count = %d, want %d", got, want)
	}
}

func testMessage(id string, created time.Time, cost float64) stats.MessageEntry {
	return stats.MessageEntry{
		ID:          id,
		SessionID:   "session-1",
		Role:        "assistant",
		TimeCreated: created,
		Cost:        cost,
		Tokens:      &stats.TokenStats{Input: 10, Output: 5},
		ModelID:     "gpt-test",
		ProviderID:  "openai",
	}
}

func boolPtr(v bool) *bool {
	return &v
}

const syncFakeSourceID = "cache_sync_test"

type syncFakeSource struct {
	id            string // overrides syncFakeSourceID, so one store can host several fakes
	messages      []stats.MessageEntry
	available     *bool
	scannedFiles  int64
	messagesErr   error
	messagesGate  chan struct{} // when set, Messages blocks until closed
	ignoreWindows bool          // when set, window hints are ignored (everything is returned)

	mu             sync.Mutex // guards the call-tracking fields; reads are racy with concurrent calls otherwise
	detailCalls    []string
	messagesCalls  int
	messageQueries []stats.PeriodQuery
	sessionQueries []stats.SessionQuery
}

func (s *syncFakeSource) sourceID() string {
	if s.id != "" {
		return s.id
	}
	return syncFakeSourceID
}

// fakeWindow resolves the time-precision hints the way live sources do; the
// date-only From/To strings are ignored (FromTime accompanies From in every
// caller that matters).
func (s *syncFakeSource) fakeWindow(pq stats.PeriodQuery) (time.Time, time.Time) {
	if s.ignoreWindows {
		return time.Time{}, time.Time{}
	}
	return pq.FromTime, pq.ToTime
}

func inFakeWindow(t time.Time, start, end time.Time) bool {
	if !start.IsZero() && t.Before(start) {
		return false
	}
	if !end.IsZero() && !t.Before(end) {
		return false
	}
	return true
}

func (s *syncFakeSource) Info(context.Context) source.SourceInfo {
	available := true
	if s.available != nil {
		available = *s.available
	}
	return source.SourceInfo{
		ID:        source.SourceID(s.sourceID()),
		Label:     "Cache Sync Test",
		Kind:      "jsonl",
		Available: available,
		ReadOnly:  true,
		LocalOnly: true,
		Diagnostics: source.SourceDiagnostics{
			ScannedFiles: s.scannedFiles,
		},
	}
}

func (s *syncFakeSource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	return stats.OverviewStats{}, nil
}

func (s *syncFakeSource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return stats.DailyStats{}, nil
}

func (s *syncFakeSource) DailyDimension(context.Context, string, stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	return stats.DailyDimensionStats{}, nil
}

func (s *syncFakeSource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return stats.ModelStats{}, nil
}

func (s *syncFakeSource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return stats.ToolStats{}, nil
}

func (s *syncFakeSource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return stats.ProjectStats{}, nil
}

func (s *syncFakeSource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *syncFakeSource) Sessions(_ context.Context, query stats.SessionQuery) (stats.SessionList, error) {
	s.mu.Lock()
	s.sessionQueries = append(s.sessionQueries, query)
	s.mu.Unlock()
	start, end := s.fakeWindow(stats.PeriodQuery{FromTime: query.FromTime, ToTime: query.ToTime})
	sessions := make(map[string][]stats.MessageEntry)
	for _, msg := range s.messages {
		if !inFakeWindow(msg.TimeCreated, start, end) {
			continue
		}
		sessions[msg.SessionID] = append(sessions[msg.SessionID], msg)
	}
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]stats.SessionEntry, 0, len(ids))
	for _, id := range ids {
		// Whole-session metadata and rollups, like the live JSONL sources.
		var all []stats.MessageEntry
		for _, msg := range s.messages {
			if msg.SessionID == id {
				all = append(all, msg)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].TimeCreated.Before(all[j].TimeCreated) })
		var cost float64
		for _, msg := range all {
			cost += msg.Cost
		}
		entries = append(entries, stats.SessionEntry{
			SourceID:     s.sourceID(),
			ID:           id,
			Title:        "Session " + id,
			ProjectID:    "project-1",
			ProjectName:  "Project 1",
			TimeCreated:  all[0].TimeCreated,
			TimeUpdated:  all[len(all)-1].TimeCreated,
			MessageCount: int64(len(all)),
			Cost:         cost,
		})
	}
	return stats.SessionList{
		SourceID: s.sourceID(),
		Sessions: entries,
		Total:    int64(len(entries)),
		Page:     1,
		PageSize: 100,
	}, nil
}

func (s *syncFakeSource) SessionByID(context.Context, string) (*stats.SessionDetail, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *syncFakeSource) Messages(_ context.Context, pq stats.PeriodQuery, _ int, _ int, _ stats.MessageSort) (stats.MessageList, error) {
	s.mu.Lock()
	s.messagesCalls++
	s.messageQueries = append(s.messageQueries, pq)
	s.mu.Unlock()
	if s.messagesGate != nil {
		<-s.messagesGate
	}
	if s.messagesErr != nil {
		return stats.MessageList{}, s.messagesErr
	}
	start, end := s.fakeWindow(pq)
	messages := make([]stats.MessageEntry, 0, len(s.messages))
	for _, msg := range s.messages {
		if inFakeWindow(msg.TimeCreated, start, end) {
			messages = append(messages, msg)
		}
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].TimeCreated.Before(messages[j].TimeCreated)
	})
	for i := range messages {
		messages[i].SessionTitle = "Session " + messages[i].SessionID
	}
	return stats.MessageList{
		SourceID: s.sourceID(),
		Messages: messages,
		Total:    int64(len(messages)),
		Page:     1,
		PageSize: 100,
	}, nil
}

func (s *syncFakeSource) MessageByID(_ context.Context, id string) (*stats.MessageDetail, error) {
	s.mu.Lock()
	s.detailCalls = append(s.detailCalls, id)
	s.mu.Unlock()
	for _, entry := range s.messages {
		if entry.ID == id {
			return &stats.MessageDetail{
				MessageEntry: entry,
				Content: stats.MessageContent{
					ToolParts: []stats.ToolPart{{
						Type:   "tool",
						CallID: "call-" + id,
						Tool:   "shell",
						State:  stats.ToolState{Status: "completed"},
					}},
				},
			}, nil
		}
	}
	return nil, nil
}

func (s *syncFakeSource) Config(context.Context) (stats.ConfigView, error) {
	return stats.ConfigView{}, nil
}
