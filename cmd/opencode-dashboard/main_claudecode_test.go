package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store/fixture"
	"opencode-dashboard/internal/web"
)

func TestBuildWebRegistryRegistersAvailableClaudeCodeSource(t *testing.T) {
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

	claudeHome := filepath.Join("..", "..", "internal", "source", "claudecode", "testdata", "valid_home")
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		st,
		dbSelection{Path: dbPath, Source: "test OpenCode fixture"},
		source.SourceClaudeCode,
		config.PathSelection{Path: claudeHome, Source: "test Claude fixture"},
		claudeHome,
		missingCodexSelection(t),
		"",
	)
	if err != nil {
		_ = st.Close()
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	infos := registry.List(ctx)
	byID := make(map[source.SourceID]source.SourceInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	openInfo, ok := byID[source.SourceOpenCode]
	if !ok {
		t.Fatalf("registry missing opencode: %#v", infos)
	}
	if !openInfo.Available {
		t.Errorf("opencode Available = false, want true")
	}
	claudeInfo, ok := byID[source.SourceClaudeCode]
	if !ok {
		t.Fatalf("registry missing claude_code: %#v", infos)
	}
	if !claudeInfo.Available {
		t.Fatalf("claude_code Available = false, want true: %#v", claudeInfo.Diagnostics)
	}
	if !claudeInfo.Selected {
		t.Errorf("claude_code Selected = false, want true for startup source")
	}
	if claudeInfo.Path != claudeHome {
		t.Errorf("claude_code Path = %q, want %q", claudeInfo.Path, claudeHome)
	}
	if claudeInfo.PathSource != "test Claude fixture" {
		t.Errorf("claude_code PathSource = %q, want test Claude fixture", claudeInfo.PathSource)
	}
	if !strings.Contains(strings.ToLower(strings.Join(claudeInfo.Warnings, " ")), "plaintext") {
		t.Errorf("claude_code warnings = %#v, want plaintext warning", claudeInfo.Warnings)
	}

	selected, err := registry.Resolve(string(source.SourceClaudeCode))
	if err != nil {
		t.Fatalf("Resolve(claude_code) failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("claude Overview(all) failed: %v", err)
	}
	if overview.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("claude overview source_id = %q, want %q", overview.SourceID, source.SourceClaudeCode)
	}
	if overview.Messages != 14 {
		t.Errorf("claude overview messages = %d, want exactly 14 Claude API-request/prompt rows", overview.Messages)
	}
}

func TestBuildWebRegistryAllowsClaudeCodeStartupWithoutOpenCodeStore(t *testing.T) {
	ctx := context.Background()
	claudeHome := filepath.Join("..", "..", "internal", "source", "claudecode", "testdata", "valid_home")
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		nil,
		dbSelection{Path: filepath.Join(t.TempDir(), "missing-opencode.db"), Source: "missing test database"},
		source.SourceClaudeCode,
		config.PathSelection{Path: claudeHome, Source: "test Claude fixture"},
		claudeHome,
		missingCodexSelection(t),
		"",
	)
	if err != nil {
		t.Fatalf("buildWebRegistry(nil store, claude startup) failed: %v", err)
	}
	defer registry.Close()

	infos := registry.List(ctx)
	byID := make(map[source.SourceID]source.SourceInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	if byID[source.SourceOpenCode].Available {
		t.Errorf("opencode Available = true, want unavailable placeholder when OpenCode store is nil")
	}
	claudeInfo := byID[source.SourceClaudeCode]
	if !claudeInfo.Available {
		t.Fatalf("claude_code Available = false, want true: %#v", claudeInfo.Diagnostics)
	}
	if !claudeInfo.Selected {
		t.Errorf("claude_code Selected = false, want true for startup source")
	}

	selected, err := registry.Resolve(string(source.SourceClaudeCode))
	if err != nil {
		t.Fatalf("Resolve(claude_code) without OpenCode store failed: %v", err)
	}
	overview, err := selected.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("claude Overview(all) without OpenCode store failed: %v", err)
	}
	if overview.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("claude overview source_id = %q, want %q", overview.SourceID, source.SourceClaudeCode)
	}
	if overview.Messages != 14 {
		t.Errorf("claude overview messages = %d, want exactly 14 Claude API-request/prompt rows", overview.Messages)
	}
}

func TestBuildWebRegistrySourcesEndpointReportsClaudeStartupAndOpenCodeDefault(t *testing.T) {
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

	claudeHome := filepath.Join("..", "..", "internal", "source", "claudecode", "testdata", "valid_home")
	registry, err := buildWebRegistry(
		newTestCacheRuntime(t),
		st,
		dbSelection{Path: dbPath, Source: "test OpenCode fixture"},
		source.SourceClaudeCode,
		config.PathSelection{Path: claudeHome, Source: "test Claude fixture"},
		claudeHome,
		missingCodexSelection(t),
		"",
	)
	if err != nil {
		_ = st.Close()
		t.Fatalf("buildWebRegistry() failed: %v", err)
	}
	defer registry.Close()

	server := web.NewServer(web.ServerOptions{Registry: registry})
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
	if body.StartupSourceID != source.SourceClaudeCode {
		t.Errorf("startup_source_id = %q, want %q", body.StartupSourceID, source.SourceClaudeCode)
	}

	byID := make(map[source.SourceID]source.SourceInfo, len(body.Sources))
	for _, info := range body.Sources {
		byID[info.ID] = info
	}
	openInfo, ok := byID[source.SourceOpenCode]
	if !ok {
		t.Fatalf("sources response missing opencode: %#v", body.Sources)
	}
	if !openInfo.Default {
		t.Errorf("opencode Default = false, want true because OpenCode remains the backend default")
	}
	if openInfo.Selected {
		t.Errorf("opencode Selected = true, want false when startup source is Claude Code")
	}

	claudeInfo, ok := byID[source.SourceClaudeCode]
	if !ok {
		t.Fatalf("sources response missing claude_code: %#v", body.Sources)
	}
	if !claudeInfo.Selected {
		t.Errorf("claude_code Selected = false, want true for startup source")
	}
	if claudeInfo.Default {
		t.Errorf("claude_code Default = true, want false because omitted/empty API source still resolves to OpenCode")
	}
	if !claudeInfo.Available {
		t.Errorf("claude_code Available = false, want true: %#v", claudeInfo.Diagnostics)
	}
}
