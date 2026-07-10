package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	usagecache "opencode-dashboard/internal/cache"
	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/codex"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store/fixture"
	"opencode-dashboard/internal/web"
)

func TestParseSourceSelectionAcceptsCodex(t *testing.T) {
	got, err := parseSourceSelection(" codex ")
	if err != nil {
		t.Fatalf("parseSourceSelection(codex) returned error: %v", err)
	}
	if got != source.SourceCodex {
		t.Fatalf("parseSourceSelection(codex) = %q, want %q", got, source.SourceCodex)
	}

	_, err = parseSourceSelection("codex_typo")
	if err == nil {
		t.Fatalf("parseSourceSelection(codex_typo) error = nil, want invalid source error")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("invalid source error = %q, want supported list to mention codex", err.Error())
	}
}

func TestBuildWebRegistryRegistersAvailableCodexSource(t *testing.T) {
	ctx := context.Background()
	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("SampleFixture() failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := openValidatedStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("openValidatedStore() failed: %v", err)
	}

	codexHome := codexFixtureHome()
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		st,
		dbSelection{Path: dbPath, Source: "test OpenCode fixture"},
		source.SourceCodex,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: codexHome, Source: "test Codex fixture"},
		codexHome,
	)
	if err != nil {
		_ = st.Close()
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	infos := registry.List(ctx)
	byID := sourceInfoByID(infos)
	openInfo, ok := byID[source.SourceOpenCode]
	if !ok {
		t.Fatalf("registry missing opencode: %#v", infos)
	}
	if !openInfo.Available {
		t.Errorf("opencode Available = false, want true")
	}

	codexInfo, ok := byID[source.SourceCodex]
	if !ok {
		t.Fatalf("registry missing codex: %#v", infos)
	}
	if !codexInfo.Available {
		t.Fatalf("codex Available = false, want true: %#v", codexInfo.Diagnostics)
	}
	if !codexInfo.Selected {
		t.Errorf("codex Selected = false, want true for startup source")
	}
	if codexInfo.Path != codexHome {
		t.Errorf("codex Path = %q, want %q", codexInfo.Path, codexHome)
	}
	if codexInfo.PathSource != "test Codex fixture" {
		t.Errorf("codex PathSource = %q, want test Codex fixture", codexInfo.PathSource)
	}
	if codexInfo.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) {
		t.Errorf("codex cost policy status = %q, want %q", codexInfo.CostPolicy.Status, stats.CostEstimatedAPIEquivalent)
	}
	if !strings.Contains(strings.ToLower(strings.Join(codexInfo.Warnings, " ")), "plaintext") {
		t.Errorf("codex warnings = %#v, want plaintext warning", codexInfo.Warnings)
	}

	selected, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve(codex) failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("codex Overview(all) failed: %v", err)
	}
	if overview.SourceID != string(source.SourceCodex) {
		t.Errorf("codex overview source_id = %q, want %q", overview.SourceID, source.SourceCodex)
	}
	if overview.Messages != 4 {
		t.Errorf("codex overview messages = %d, want 4 per-request rows", overview.Messages)
	}
}

func TestBuildWebRegistryAllowsCodexStartupWithoutOpenCodeStore(t *testing.T) {
	ctx := context.Background()
	codexHome := codexFixtureHome()
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		nil,
		dbSelection{Path: filepath.Join(t.TempDir(), "missing-opencode.db"), Source: "missing test database"},
		source.SourceCodex,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: codexHome, Source: "test Codex fixture"},
		codexHome,
	)
	if err != nil {
		t.Fatalf("buildWebRegistry(nil store, codex startup) failed: %v", err)
	}
	defer registry.Close()

	infos := registry.List(ctx)
	byID := sourceInfoByID(infos)
	if byID[source.SourceOpenCode].Available {
		t.Errorf("opencode Available = true, want unavailable placeholder when OpenCode store is nil")
	}
	codexInfo := byID[source.SourceCodex]
	if !codexInfo.Available {
		t.Fatalf("codex Available = false, want true: %#v", codexInfo.Diagnostics)
	}
	if !codexInfo.Selected {
		t.Errorf("codex Selected = false, want true for startup source")
	}

	selected, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve(codex) without OpenCode store failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("codex Overview(all) without OpenCode store failed: %v", err)
	}
	if overview.SourceID != string(source.SourceCodex) {
		t.Errorf("codex overview source_id = %q, want %q", overview.SourceID, source.SourceCodex)
	}
	if overview.Messages != 4 {
		t.Errorf("codex overview messages = %d, want 4 per-request rows", overview.Messages)
	}
}

func TestBuildWebRegistryStartsInitialSyncForEmptyCache(t *testing.T) {
	ctx := context.Background()
	codexHome := codexFixtureHome()
	cache := newTestCacheRuntime(t)
	registry, err := buildWebRegistry(
		cache,
		nil,
		dbSelection{Path: filepath.Join(t.TempDir(), "missing-opencode.db"), Source: "missing test database"},
		source.SourceCodex,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: codexHome, Source: "test Codex fixture"},
		codexHome,
	)
	if err != nil {
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	// Views work immediately (served live until the startup sync lands).
	selected, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve(codex) failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("codex Overview(all) failed during startup sync: %v", err)
	}
	if overview.Messages != 4 {
		t.Errorf("codex overview messages = %d, want 4", overview.Messages)
	}

	// The startup background sync activates the cache without a manual resync.
	deadline := time.Now().Add(5 * time.Second)
	for !cache.hasCachedSources() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !cache.hasCachedSources() {
		t.Fatalf("startup sync did not activate the cached source")
	}
	selected, err = registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve(codex) after startup sync failed: %v", err)
	}
	overview, err = selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("cached codex Overview(all) failed: %v", err)
	}
	if overview.Messages != 4 {
		t.Errorf("cached codex overview messages = %d, want 4", overview.Messages)
	}
}

func TestBuildWebRegistryServesReadyCacheDespiteFingerprintMismatchAndHealsOnRead(t *testing.T) {
	ctx := context.Background()
	codexHome := copyDirToTemp(t, codexFixtureHome())
	cache := newTestCacheRuntime(t)
	liveSeed := codex.New(codex.Options{CodexHome: codexHome, PathSource: "test Codex fixture"})
	if _, err := cache.store.SyncSourceWithOptions(ctx, liveSeed, usagecache.SyncOptions{}); err != nil {
		t.Fatalf("seed sync failed: %v", err)
	}

	// New raw activity after the sync changes the source fingerprint.
	writeRecentCodexRollout(t, codexHome)
	liveFresh := codex.New(codex.Options{CodexHome: codexHome, PathSource: "test Codex fixture"})
	wantOverview, err := liveFresh.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("live Overview(all) failed: %v", err)
	}
	if wantOverview.Messages <= 4 {
		t.Fatalf("live message count = %d, want new rollout to add rows", wantOverview.Messages)
	}

	registry, err := buildWebRegistry(
		cache,
		nil,
		dbSelection{Path: filepath.Join(t.TempDir(), "missing-opencode.db"), Source: "missing test database"},
		source.SourceCodex,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: codexHome, Source: "test Codex fixture"},
		codexHome,
	)
	if err != nil {
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	// A ready cache is registered directly — no startup job needed.
	if !cache.hasCachedSources() {
		t.Fatalf("ready cache with stale fingerprint was not registered as cached")
	}
	if snapshot := cache.jobSnapshot(); snapshot != nil && snapshot.Running {
		t.Fatalf("startup sync job started for a ready cache: %#v", snapshot)
	}

	// The first read gap-fills from raw, so no information is lost.
	selected, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve(codex) failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("cached Overview(all) failed: %v", err)
	}
	if overview.Messages != wantOverview.Messages {
		t.Fatalf("cached overview messages = %d, want %d (cache plus raw gap)", overview.Messages, wantOverview.Messages)
	}
}

func copyDirToTemp(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture home: %v", err)
	}
	return dst
}

// writeRecentCodexRollout adds a minimal rollout transcript stamped within the
// last hour, i.e. inside the gap region after the seed sync's finality cutoff.
func writeRecentCodexRollout(t *testing.T, codexHome string) {
	t.Helper()
	now := time.Now().UTC().Add(-30 * time.Minute)
	dir := filepath.Join(codexHome, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create rollout dir: %v", err)
	}
	stamp := func(offset time.Duration) string {
		return now.Add(offset).Format("2006-01-02T15:04:05Z")
	}
	lines := []string{
		`{"timestamp":"` + stamp(0) + `","type":"session_meta","payload":{"id":"recent-session","cli_version":"codex-synthetic-1.0.0","source":"codex_cli","model_provider":"openai","thread_source":"local_jsonl","cwd":"/tmp/recent-project"}}`,
		`{"timestamp":"` + stamp(time.Second) + `","type":"turn_context","payload":{"turn_id":"turn-1","model":"gpt-5.5","model_provider":"openai","cwd":"/tmp/recent-project"}}`,
		`{"timestamp":"` + stamp(2*time.Second) + `","type":"event_msg","payload":{"type":"user_message","turn_id":"turn-1","message":"recent question"}}`,
		`{"timestamp":"` + stamp(3*time.Second) + `","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-1","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":135},"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":135},"model_context_window":300000}}}`,
		`{"timestamp":"` + stamp(4*time.Second) + `","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","status":"success"}}`,
	}
	path := filepath.Join(dir, "rollout-"+now.Format("2006-01-02T15-04-05Z")+"-recent-session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write recent rollout: %v", err)
	}
}

func TestCacheRuntimeSyncActivatesCachedSource(t *testing.T) {
	ctx := context.Background()
	codexHome := codexFixtureHome()
	cache := newTestCacheRuntime(t)
	registry, err := buildWebRegistry(
		cache,
		nil,
		dbSelection{Path: filepath.Join(t.TempDir(), "missing-opencode.db"), Source: "missing test database"},
		source.SourceCodex,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: codexHome, Source: "test Codex fixture"},
		codexHome,
	)
	if err != nil {
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	status, err := cache.Sync(ctx, string(source.SourceCodex), "")
	if err != nil {
		t.Fatalf("cache Sync(codex) failed: %v", err)
	}
	if status.Sync == nil || !status.Sync.Running {
		t.Fatalf("cache status after starting sync = %#v, want running sync job", status)
	}
	if status.Sync.Mode != string(usagecache.SyncModeIncremental) {
		t.Fatalf("cache sync mode = %q, want incremental", status.Sync.Mode)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err = cache.Status(ctx)
		if err != nil {
			t.Fatalf("cache Status() failed: %v", err)
		}
		if status.Sync != nil && !status.Sync.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Sync == nil || status.Sync.Running || status.Sync.Status != "complete" || status.Sync.Completed != 1 {
		t.Fatalf("cache status after sync = %#v, want complete job", status)
	}
	if !status.Active || !cache.hasCachedSources() {
		t.Fatalf("cache status after sync = %#v, want active cached source", status)
	}
	if len(status.Sync.Logs) == 0 {
		t.Fatalf("cache sync logs are empty, want progress log entries")
	}

	selected, err := registry.Resolve(string(source.SourceCodex))
	if err != nil {
		t.Fatalf("Resolve(codex) after cache sync failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("cached codex Overview(all) failed: %v", err)
	}
	if overview.Messages != 4 {
		t.Errorf("cached codex overview messages = %d, want 4", overview.Messages)
	}
}

func TestBuildWebRegistrySourcesEndpointReportsCodexStartupAndOpenCodeDefault(t *testing.T) {
	ctx := context.Background()
	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("SampleFixture() failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := openValidatedStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("openValidatedStore() failed: %v", err)
	}

	codexHome := codexFixtureHome()
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		st,
		dbSelection{Path: dbPath, Source: "test OpenCode fixture"},
		source.SourceCodex,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: codexHome, Source: "test Codex fixture"},
		codexHome,
	)
	if err != nil {
		_ = st.Close()
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	server := web.NewServer("", registry, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/sources status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body source.SourceListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sources response: %v; body: %s", err, rec.Body.String())
	}
	if body.DefaultSourceID != source.SourceOpenCode {
		t.Errorf("default_source_id = %q, want %q", body.DefaultSourceID, source.SourceOpenCode)
	}
	if body.StartupSourceID != source.SourceCodex {
		t.Errorf("startup_source_id = %q, want %q", body.StartupSourceID, source.SourceCodex)
	}

	byID := sourceInfoByID(body.Sources)
	openInfo, ok := byID[source.SourceOpenCode]
	if !ok {
		t.Fatalf("sources response missing opencode: %#v", body.Sources)
	}
	if !openInfo.Default {
		t.Errorf("opencode Default = false, want true because OpenCode remains the backend default")
	}
	if openInfo.Selected {
		t.Errorf("opencode Selected = true, want false when startup source is Codex")
	}

	codexInfo, ok := byID[source.SourceCodex]
	if !ok {
		t.Fatalf("sources response missing codex: %#v", body.Sources)
	}
	if !codexInfo.Selected {
		t.Errorf("codex Selected = false, want true for startup source")
	}
	if codexInfo.Default {
		t.Errorf("codex Default = true, want false because omitted/empty API source still resolves to OpenCode")
	}
	if !codexInfo.Available {
		t.Errorf("codex Available = false, want true: %#v", codexInfo.Diagnostics)
	}
}

func TestBuildWebRegistryRegistersUnavailableConfiguredCodexSource(t *testing.T) {
	ctx := context.Background()
	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("SampleFixture() failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := openValidatedStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("openValidatedStore() failed: %v", err)
	}

	missingCodexHome := filepath.Join(t.TempDir(), "configured-missing-codex-home")
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		st,
		dbSelection{Path: dbPath, Source: "test OpenCode fixture"},
		source.SourceOpenCode,
		missingClaudeSelection(t),
		"",
		config.PathSelection{Path: missingCodexHome, Source: "--codex-home"},
		missingCodexHome,
	)
	if err != nil {
		_ = st.Close()
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	infos := registry.List(ctx)
	byID := sourceInfoByID(infos)
	codexInfo, ok := byID[source.SourceCodex]
	if !ok {
		t.Fatalf("registry missing configured unavailable codex: %#v", infos)
	}
	if codexInfo.Available {
		t.Fatalf("codex Available = true, want false for missing configured home")
	}
	if codexInfo.Path != missingCodexHome {
		t.Errorf("codex Path = %q, want %q", codexInfo.Path, missingCodexHome)
	}
	if codexInfo.PathSource != "--codex-home" {
		t.Errorf("codex PathSource = %q, want --codex-home", codexInfo.PathSource)
	}
	if codexInfo.Diagnostics.Reason == "" || !strings.Contains(strings.ToLower(codexInfo.Diagnostics.Reason), "not found") {
		t.Errorf("codex diagnostics = %#v, want not-found reason", codexInfo.Diagnostics)
	}

	selected, err := registry.Resolve(string(source.SourceCodex))
	if err == nil {
		t.Fatalf("Resolve(codex) for unavailable configured source returned %#v, want error", selected)
	}
	if !errors.Is(err, source.ErrUnavailableSource) {
		t.Fatalf("Resolve(codex) error = %v, want errors.Is(..., ErrUnavailableSource)", err)
	}

	server := web.NewServer("", registry, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/sources status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body source.SourceListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sources response: %v; body: %s", err, rec.Body.String())
	}
	endpointCodex, ok := sourceInfoByID(body.Sources)[source.SourceCodex]
	if !ok {
		t.Fatalf("/api/v1/sources missing configured unavailable codex: %#v", body.Sources)
	}
	if endpointCodex.Available {
		t.Fatalf("/api/v1/sources codex Available = true, want false for missing configured home")
	}
}

func codexFixtureHome() string {
	return filepath.Join("..", "..", "internal", "source", "codex", "testdata", "valid_home")
}

func missingClaudeSelection(t *testing.T) config.PathSelection {
	t.Helper()
	return config.PathSelection{Path: filepath.Join(t.TempDir(), "missing-claude-home"), Source: "test missing Claude fixture"}
}

func missingCodexSelection(t *testing.T) config.PathSelection {
	t.Helper()
	return config.PathSelection{Path: filepath.Join(t.TempDir(), "missing-codex-home"), Source: "test missing Codex fixture"}
}

func sourceInfoByID(infos []source.SourceInfo) map[source.SourceID]source.SourceInfo {
	byID := make(map[source.SourceID]source.SourceInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	return byID
}
