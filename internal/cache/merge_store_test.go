package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// TestStateMemoNeverPinsStaleWatermarks: reads racing state commits must
// never leave the watermark memo pinned at pre-commit values once writes
// quiesce — the memo seed is generation-guarded against storing a row read
// before a commit after that commit's invalidation ran.
func TestStateMemoNeverPinsStaleWatermarks(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	src := &syncFakeSource{messages: []stats.MessageEntry{testMessage("m", now.Add(-10*time.Hour), 0.01)}}
	store := newTestStore(t)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = store.LastSyncedMS(ctx, syncFakeSourceID)
		}
	}()
	for i := 0; i < 5; i++ {
		if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Mode: SyncModeRebuild}); err != nil {
			t.Fatalf("sync %d failed: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	var want int64
	if err := store.db.QueryRowContext(ctx, `SELECT last_synced_ms FROM source_state WHERE source_id = ?`, syncFakeSourceID).Scan(&want); err != nil {
		t.Fatalf("read state row: %v", err)
	}
	got, err := store.LastSyncedMS(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("LastSyncedMS() failed: %v", err)
	}
	if got != want {
		t.Fatalf("memoized last_synced_ms = %d, DB row = %d (stale memo pinned)", got, want)
	}
}
