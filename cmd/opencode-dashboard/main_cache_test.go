package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	usagecache "opencode-dashboard/internal/cache"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/codex"
	"opencode-dashboard/internal/stats"
)

func newTestCache(t *testing.T) *usagecache.Store {
	t.Helper()
	store, err := usagecache.Open(context.Background(), filepath.Join(t.TempDir(), "usage-cache.sqlite"))
	if err != nil {
		t.Fatalf("open test cache: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newTestCacheRuntime(t *testing.T) *cacheRuntime {
	t.Helper()
	store := newTestCache(t)
	return &cacheRuntime{
		store:  store,
		path:   store.Path(),
		source: "test cache",
		live:   make(map[source.SourceID]source.Source),
		cached: make(map[source.SourceID]bool),
	}
}

func TestSyncJobContinuesPastFailingSource(t *testing.T) {
	ctx := context.Background()
	cache := newTestCacheRuntime(t)
	registry := source.NewRegistry(source.SourceOpenCode)
	cache.registry = registry

	failing := &failingTestSource{}
	codexSrc := codex.New(codex.Options{CodexHome: codexFixtureHome(), PathSource: "test fixture"})
	cache.rememberSource(failing)
	cache.rememberSource(codexSrc)
	if err := registry.Register(failing); err != nil {
		t.Fatalf("register failing source: %v", err)
	}
	if err := registry.Register(codexSrc); err != nil {
		t.Fatalf("register codex source: %v", err)
	}

	status, err := cache.Sync(ctx, "all", "")
	if err != nil {
		t.Fatalf("Sync(all) failed to start: %v", err)
	}
	if status.Sync == nil || !status.Sync.Running {
		t.Fatalf("sync job did not start: %#v", status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err = cache.Status(ctx)
		if err != nil {
			t.Fatalf("Status() failed: %v", err)
		}
		if status.Sync != nil && !status.Sync.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Sync == nil || status.Sync.Running {
		t.Fatalf("sync job never finished: %#v", status.Sync)
	}
	if status.Sync.Status != "error" || !strings.Contains(status.Sync.Error, "Failing Test") {
		t.Fatalf("job status = %q error = %q, want error naming the failing source", status.Sync.Status, status.Sync.Error)
	}
	if status.Sync.Completed != 2 {
		t.Fatalf("job completed = %d, want 2 (failing source must not abort the rest)", status.Sync.Completed)
	}
	if !cache.isCached(source.SourceCodex) {
		t.Fatalf("codex was not cached after job; one failing source aborted the rest")
	}
}

type failingTestSource struct{}

func (s *failingTestSource) Info(context.Context) source.SourceInfo {
	return source.SourceInfo{ID: "failing_test", Label: "Failing Test", Kind: "jsonl", Available: true}
}

func (s *failingTestSource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	return stats.OverviewStats{}, nil
}

func (s *failingTestSource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return stats.DailyStats{}, nil
}

func (s *failingTestSource) DailyDimension(context.Context, string, stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	return stats.DailyDimensionStats{}, nil
}

func (s *failingTestSource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return stats.ModelStats{}, nil
}

func (s *failingTestSource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return stats.ToolStats{}, nil
}

func (s *failingTestSource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return stats.ProjectStats{}, nil
}

func (s *failingTestSource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}

func (s *failingTestSource) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return stats.SessionList{}, nil
}

func (s *failingTestSource) SessionByID(context.Context, string) (*stats.SessionDetail, error) {
	return nil, nil
}

func (s *failingTestSource) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	return stats.MessageList{}, fmt.Errorf("synthetic collect failure")
}

func (s *failingTestSource) MessageByID(context.Context, string) (*stats.MessageDetail, error) {
	return nil, nil
}

func (s *failingTestSource) Config(context.Context) (stats.ConfigView, error) {
	return stats.ConfigView{}, nil
}
