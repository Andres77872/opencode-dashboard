package cache

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

type participationSource struct {
	*syncFakeSource
	started, cancelled, resumed chan struct{}
	calls                       atomic.Int32
}

func (s *participationSource) ConsolidationData(ctx context.Context, _ stats.PeriodQuery) (source.ConsolidationData, error) {
	if s.calls.Add(1) == 1 {
		close(s.started)
		<-ctx.Done()
		close(s.cancelled)
		return source.ConsolidationData{}, ctx.Err()
	}
	close(s.resumed)
	return source.ConsolidationData{}, nil
}

func TestSourceParticipationCancelsAndResumesDetachedFills(t *testing.T) {
	live := &participationSource{syncFakeSource: &syncFakeSource{}, started: make(chan struct{}), cancelled: make(chan struct{}), resumed: make(chan struct{})}
	cached := WrapSource(newTestStore(t), live)
	defer cached.Close()
	var enabled atomic.Bool
	enabled.Store(true)
	cached.SetEnabledCheck(enabled.Load)
	cached.ensureFresh(context.Background())
	select {
	case <-live.started:
	case <-time.After(3 * time.Second):
		t.Fatal("fill did not start")
	}
	enabled.Store(false)
	cached.SetParticipation(false)
	select {
	case <-live.cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("fill was not cancelled")
	}
	cached.freshMu.Lock()
	done := cached.fillDone
	cached.freshMu.Unlock()
	if done != nil {
		<-done
	}
	cached.ensureFresh(context.Background())
	if live.calls.Load() != 1 {
		t.Fatal("disabled fill ran")
	}
	enabled.Store(true)
	cached.SetParticipation(true)
	cached.ensureFresh(context.Background())
	select {
	case <-live.resumed:
	case <-time.After(3 * time.Second):
		t.Fatal("reenabled fill did not resume")
	}
}

func TestAntigravityFingerprintIncludesWALAndAccountingLayout(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "conversations"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "conversations", "parent.db"), []byte("snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	info := source.SourceInfo{ID: source.SourceAntigravity, Path: home, Kind: "sqlite", DataLayout: "antigravity-sqlite-pb-v1"}
	a, err := sourceFingerprint(context.Background(), info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "conversations", "parent.db-wal"), []byte("new persisted step"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := sourceFingerprint(context.Background(), info)
	if err != nil || a == b {
		t.Fatal("WAL change did not invalidate fingerprint", err)
	}
	info.DataLayout = "antigravity-sqlite-pb-v2"
	c, err := sourceFingerprint(context.Background(), info)
	if err != nil || !pricingIdentityChanged(b, c, rebuildIdentity(info)) {
		t.Fatal("accounting layout did not require recollection", err)
	}
}
