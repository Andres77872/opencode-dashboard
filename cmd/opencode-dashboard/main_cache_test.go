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
	"opencode-dashboard/internal/web"
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

// TestAutoSyncTickWrapsRawSource: a source left raw (startup sync never ran)
// becomes cache-backed after one periodic tick, and a second tick keeps the
// existing cached wrapper instead of churning it.
func TestAutoSyncTickWrapsRawSource(t *testing.T) {
	cache := newTestCacheRuntime(t)
	registry := source.NewRegistry(source.SourceOpenCode)
	cache.registry = registry

	codexSrc := codex.New(codex.Options{CodexHome: codexFixtureHome(), PathSource: "test fixture"})
	cache.rememberSource(codexSrc)
	if err := registry.Register(codexSrc); err != nil {
		t.Fatalf("register codex source: %v", err)
	}
	cache.queueInitialSync(sourceIDOf(codexSrc))

	cache.runAutoSyncTick()

	if !cache.isCached(source.SourceCodex) {
		t.Fatalf("source not cached after auto-sync tick")
	}
	cache.mu.Lock()
	pending := len(cache.pendingInitial)
	target := cache.job.Target
	running := cache.job.Running
	cache.mu.Unlock()
	if pending != 0 {
		t.Fatalf("auto-sync tick left %d pending initial syncs", pending)
	}
	if target != "auto" || running {
		t.Fatalf("job state after tick = target %q running %v, want finished auto job", target, running)
	}
	wrapped, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if _, ok := wrapped.(*usagecache.CachedSource); !ok {
		t.Fatalf("registry entry is %T, want *usagecache.CachedSource", wrapped)
	}

	// A second tick must not replace the cached wrapper.
	cache.runAutoSyncTick()
	again, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve() after second tick failed: %v", err)
	}
	if again != wrapped {
		t.Fatalf("second tick re-wrapped the cached source (wrapper churn)")
	}
}

// TestRebuildSweepsUnknownSources: "rebuild the entire database" also removes
// cached rows for sources that are no longer registered with the runtime.
func TestRebuildSweepsUnknownSources(t *testing.T) {
	ctx := context.Background()
	cache := newTestCacheRuntime(t)
	registry := source.NewRegistry(source.SourceOpenCode)
	cache.registry = registry

	// Seed rows for a source the runtime does not know about (renamed or
	// de-configured since it was cached).
	ghost := codex.New(codex.Options{CodexHome: codexFixtureHome(), PathSource: "test fixture"})
	if _, err := cache.store.SyncSourceWithOptions(ctx, ghost, usagecache.SyncOptions{}); err != nil {
		t.Fatalf("seed ghost source: %v", err)
	}
	if _, ok, err := cache.store.SourceStatus(ctx, string(source.SourceCodex)); err != nil || !ok {
		t.Fatalf("ghost state not seeded: ok=%v err=%v", ok, err)
	}

	status, err := cache.Sync(ctx, "", "rebuild")
	if err != nil {
		t.Fatalf("Sync(rebuild) failed to start: %v", err)
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
		t.Fatalf("rebuild job never finished: %#v", status.Sync)
	}

	if _, ok, err := cache.store.SourceStatus(ctx, string(source.SourceCodex)); err != nil {
		t.Fatalf("SourceStatus() failed: %v", err)
	} else if ok {
		t.Fatalf("rebuild kept state for an unregistered source")
	}
	var mentioned bool
	for _, entry := range status.Sync.Logs {
		if strings.Contains(entry.Message, "no longer configured") {
			mentioned = true
			break
		}
	}
	if !mentioned {
		t.Fatalf("job log never mentioned the swept sources: %#v", status.Sync.Logs)
	}
}

// TestAutoSyncTickSkipsWhileJobRunning: a manual/startup job wins.
func TestAutoSyncTickSkipsWhileJobRunning(t *testing.T) {
	cache := newTestCacheRuntime(t)
	codexSrc := codex.New(codex.Options{CodexHome: codexFixtureHome(), PathSource: "test fixture"})
	cache.rememberSource(codexSrc)

	cache.mu.Lock()
	cache.job.Running = true
	cache.job.Target = "manual"
	cache.mu.Unlock()

	cache.runAutoSyncTick()

	cache.mu.Lock()
	target := cache.job.Target
	cache.mu.Unlock()
	if target != "manual" {
		t.Fatalf("tick replaced a running job: target = %q", target)
	}
	if cache.isCached(source.SourceCodex) {
		t.Fatalf("tick synced despite a running job")
	}
}

// TestCacheRuntimeCloseReapsAutoSyncLoop: Close cancels the loop and waits it
// out instead of leaking the goroutine.
func TestCacheRuntimeCloseReapsAutoSyncLoop(t *testing.T) {
	cache := newTestCacheRuntime(t)
	cache.lifeCtx, cache.lifeCancel = context.WithCancel(context.Background())
	cache.startAutoSync()

	done := make(chan error, 1)
	go func() { done <- cache.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Close() never returned; auto-sync loop not reaped")
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

func TestSyncRebuildAlwaysTargetsAllSources(t *testing.T) {
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

	status, err := cache.Sync(ctx, string(source.SourceCodex), "rebuild")
	if err != nil {
		t.Fatalf("Sync(codex, rebuild) failed to start: %v", err)
	}
	if status.Sync == nil {
		t.Fatalf("sync job did not start: %#v", status)
	}
	if status.Sync.Target != "all" {
		t.Fatalf("rebuild target = %q, want %q (rebuild must cover the whole database)", status.Sync.Target, "all")
	}
	if status.Sync.Total != 2 {
		t.Fatalf("rebuild total = %d, want 2 (every registered source)", status.Sync.Total)
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
}

func TestNotifyPricingChangeStartsAndQueuesTargetedSync(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled and unknown source states", func(t *testing.T) {
		disabled := &cacheRuntime{disabled: true}
		if got := disabled.NotifyPricingChange(ctx, source.SourceCodex); got != web.PricingChangeStatusDisabled {
			t.Fatalf("disabled status = %q, want %q", got, web.PricingChangeStatusDisabled)
		}
		cache := newTestCacheRuntime(t)
		if got := cache.NotifyPricingChange(ctx, source.SourceCodex); got != web.PricingChangeStatusUnavailable {
			t.Fatalf("unknown source status = %q, want %q", got, web.PricingChangeStatusUnavailable)
		}
	})

	t.Run("starts immediately", func(t *testing.T) {
		cache := newTestCacheRuntime(t)
		codexSrc := codex.New(codex.Options{CodexHome: codexFixtureHome(), PathSource: "test fixture"})
		cache.rememberSource(codexSrc)
		if got := cache.NotifyPricingChange(ctx, source.SourceCodex); got != web.PricingChangeStatusStarted {
			t.Fatalf("status = %q, want %q", got, web.PricingChangeStatusStarted)
		}
		waitForCacheJob(t, cache)
		cache.mu.Lock()
		target := cache.job.Target
		cache.mu.Unlock()
		if target != string(source.SourceCodex) {
			t.Fatalf("target = %q, want %q", target, source.SourceCodex)
		}
	})

	t.Run("queues behind running job", func(t *testing.T) {
		cache := newTestCacheRuntime(t)
		codexSrc := codex.New(codex.Options{CodexHome: codexFixtureHome(), PathSource: "test fixture"})
		cache.rememberSource(codexSrc)
		cache.mu.Lock()
		cache.job = cacheJobState{Running: true, Status: "running"}
		cache.mu.Unlock()

		if got := cache.NotifyPricingChange(ctx, source.SourceCodex); got != web.PricingChangeStatusQueued {
			t.Fatalf("status = %q, want %q", got, web.PricingChangeStatusQueued)
		}
		cache.finishJob(nil)
		cache.startPendingPricingSync()
		waitForCacheJob(t, cache)
		cache.mu.Lock()
		pending := len(cache.pendingPricing)
		target := cache.job.Target
		cache.mu.Unlock()
		if pending != 0 || target != string(source.SourceCodex) {
			t.Fatalf("queued sync state: pending=%d target=%q", pending, target)
		}
	})
}

func waitForCacheJob(t *testing.T, cache *cacheRuntime) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cache.mu.Lock()
		running := cache.job.Running
		cache.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cache job did not finish")
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

func (s *failingTestSource) DailyDimension(context.Context, string, stats.PeriodQuery, ...stats.Granularity) (stats.DailyDimensionStats, error) {
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
