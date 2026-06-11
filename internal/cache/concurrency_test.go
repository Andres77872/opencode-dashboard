package cache

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// TestPoolConnectionsHavePragmas guards the modernc DSN syntax: _pragma
// parameters apply per connection, so every pooled connection must carry
// busy_timeout and foreign_keys, not just the one that ran a PRAGMA statement.
func TestPoolConnectionsHavePragmas(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Hold all four connections simultaneously (pool max is 4) so each
	// assertion runs on a distinct connection.
	var conns []*sql.Conn
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		conn, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatalf("checkout connection %d: %v", i, err)
		}
		conns = append(conns, conn)
	}

	for i, conn := range conns {
		var busy int64
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatalf("conn %d: read busy_timeout: %v", i, err)
		}
		if busy != busyTimeout.Milliseconds() {
			t.Errorf("conn %d: busy_timeout = %d, want %d", i, busy, busyTimeout.Milliseconds())
		}
		var foreignKeys int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("conn %d: read foreign_keys: %v", i, err)
		}
		if foreignKeys != 1 {
			t.Errorf("conn %d: foreign_keys = %d, want 1", i, foreignKeys)
		}
		var journalMode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("conn %d: read journal_mode: %v", i, err)
		}
		if journalMode != "wal" {
			t.Errorf("conn %d: journal_mode = %q, want wal", i, journalMode)
		}
	}
}

// TestConcurrentSourceSyncsDoNotCollide reproduces the production failure:
// per-source locks let different sources write concurrently, and without a
// working busy timeout plus the store write semaphore the second writer dies
// with SQLITE_BUSY, discarding its collected payload.
func TestConcurrentSourceSyncsDoNotCollide(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t)

	const sourceCount = 4
	const rounds = 5
	errCh := make(chan error, sourceCount*rounds*2)
	var wg sync.WaitGroup
	for i := 0; i < sourceCount; i++ {
		id := fmt.Sprintf("%s_%d", syncFakeSourceID, i)
		src := &syncFakeSource{id: id, messages: []stats.MessageEntry{
			testMessage("final-"+id, base.Add(-8*time.Hour), 0.01),
			testMessage("recent-"+id, base.Add(-2*time.Hour), 0.02),
		}}
		// Wrapping spawns read-triggered background fills alongside the
		// explicit syncs, matching the production collision.
		cached := WrapSource(store, src)
		t.Cleanup(func() { _ = cached.Close() })

		wg.Add(2)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				mode := SyncModeIncremental
				if r%2 == 0 {
					mode = SyncModeRebuild
				}
				if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{
					Mode:   mode,
					Cutoff: base,
				}); err != nil {
					errCh <- fmt.Errorf("sync %s round %d (%s): %w", id, r, mode, err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				if _, err := cached.Overview(ctx, stats.PeriodQuery{Period: "all"}); err != nil {
					errCh <- fmt.Errorf("read %s round %d: %w", id, r, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}
