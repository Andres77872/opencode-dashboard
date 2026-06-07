package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/codex"
	"opencode-dashboard/internal/stats"
)

func TestSourceAwareCodexAPIRoutingFromFixture(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	claudeSource := newHandlerFakeSource(source.SourceClaudeCode, true, 777)
	codexSource := codex.New(codex.Options{
		CodexHome:           filepath.Join("..", "source", "codex", "testdata", "valid_home"),
		PathSource:          "test fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "codex", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, opencodeSource, claudeSource, codexSource)

	t.Run("sources metadata exposes available Codex", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/sources status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body source.SourceListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode sources response: %v", err)
		}
		byID := make(map[source.SourceID]source.SourceInfo, len(body.Sources))
		for _, info := range body.Sources {
			byID[info.ID] = info
		}
		codexInfo, ok := byID[source.SourceCodex]
		if !ok {
			t.Fatalf("sources response missing codex: %#v", body.Sources)
		}
		if !codexInfo.Available {
			t.Fatalf("codex Available = false, want true: %#v", codexInfo.Diagnostics)
		}
		if codexInfo.Kind != "jsonl" || !codexInfo.ReadOnly || !codexInfo.LocalOnly {
			t.Errorf("codex kind/read/local = %q/%v/%v, want jsonl/true/true", codexInfo.Kind, codexInfo.ReadOnly, codexInfo.LocalOnly)
		}
		if codexInfo.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) || codexInfo.CostPolicy.PricingSnapshotID != "openai-codex-gpt-5.5-2026-04-23" {
			t.Errorf("codex cost policy = %#v, want estimated API-equivalent gpt-5.5 snapshot", codexInfo.CostPolicy)
		}
		if !codexInfo.Privacy.PlaintextTranscripts || !codexInfo.Privacy.Redaction || !codexInfo.Privacy.ReadOnly || !codexInfo.Privacy.LocalOnly {
			t.Errorf("codex privacy = %#v, want plaintext/read-only/local/redaction metadata", codexInfo.Privacy)
		}
	})

	t.Run("explicit codex overview routes only to Codex source", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?source=codex&period=all", nil)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /overview?source=codex status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if opencodeSource.overviewCalls != 0 || claudeSource.overviewCalls != 0 {
			t.Errorf("fallback source overview calls opencode/claude = %d/%d, want 0/0", opencodeSource.overviewCalls, claudeSource.overviewCalls)
		}
		var body stats.OverviewStats
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode overview: %v", err)
		}
		if body.SourceID != string(source.SourceCodex) {
			t.Errorf("overview source_id = %q, want %q", body.SourceID, source.SourceCodex)
		}
		if body.Sessions == 999 || body.Sessions == 777 {
			t.Errorf("overview sessions = %d, looks contaminated by OpenCode/Claude fake source", body.Sessions)
		}
		if body.Messages != 1 {
			t.Errorf("overview messages = %d, want exactly 1 grouped Codex interaction", body.Messages)
		}
	})

	t.Run("omitted source still defaults to OpenCode", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?period=all", nil)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /overview default status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if opencodeSource.overviewCalls != 1 {
			t.Errorf("opencode overview calls after omitted source = %d, want 1", opencodeSource.overviewCalls)
		}
		var body stats.OverviewStats
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode default overview: %v", err)
		}
		if body.Sessions != 999 {
			t.Errorf("default overview sessions = %d, want OpenCode fake 999", body.Sessions)
		}
	})
}

func TestCodexAPIEndpointClassesReturnCodexSourceTaggedPayloads(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	claudeSource := newHandlerFakeSource(source.SourceClaudeCode, true, 777)
	codexSource := codex.New(codex.Options{
		CodexHome:           filepath.Join("..", "source", "codex", "testdata", "valid_home"),
		PathSource:          "test fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "codex", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, opencodeSource, claudeSource, codexSource)

	tests := []struct {
		name string
		path string
	}{
		{name: "overview", path: "/api/v1/overview?source=codex&period=all"},
		{name: "daily", path: "/api/v1/daily?source=codex&period=all"},
		{name: "daily dimension", path: "/api/v1/daily?source=codex&period=all&dimension=model"},
		{name: "models", path: "/api/v1/models?source=codex&period=all"},
		{name: "tools", path: "/api/v1/tools?source=codex&period=all"},
		{name: "projects", path: "/api/v1/projects?source=codex&period=all"},
		{name: "project detail", path: "/api/v1/projects/synthetic-project?source=codex&period=all"},
		{name: "sessions", path: "/api/v1/sessions?source=codex&period=all"},
		{name: "session detail", path: "/api/v1/sessions/synthetic-session?source=codex"},
		{name: "messages", path: "/api/v1/messages?source=codex&period=all"},
		{name: "message detail", path: "/api/v1/messages/codex:synthetic-session:turn-1?source=codex"},
		{name: "config", path: "/api/v1/config?source=codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d; body: %s", tt.path, rec.Code, http.StatusOK, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s response: %v", tt.name, err)
			}
			if body["source_id"] != string(source.SourceCodex) {
				t.Errorf("%s source_id = %#v, want %q", tt.name, body["source_id"], source.SourceCodex)
			}
			encoded := rec.Body.String()
			for _, forbidden := range []string{"opencode", "claude_code", "SYNTHETIC_AUTH_SENTINEL_MUST_NOT_LEAK", "SYNTHETIC_TOOL_OUTPUT_SECRET_MUST_NOT_LEAK"} {
				if strings.Contains(encoded, forbidden) && forbidden != string(source.SourceCodex) {
					t.Errorf("%s response leaked cross-source/private marker %q in %s", tt.name, forbidden, encoded)
				}
			}
		})
	}
}

func TestCodexInvalidUnavailableAndDetailCollisionDoNotFallback(t *testing.T) {
	t.Run("invalid codex-like source id is rejected without fallback", func(t *testing.T) {
		opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
		codexSource := newHandlerFakeSource(source.SourceCodex, true, 111)
		handler := newSourceTestHandler(t, opencodeSource, codexSource)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?source=codex_typo&period=all", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
		}
		if opencodeSource.overviewCalls != 0 || codexSource.overviewCalls != 0 {
			t.Errorf("invalid source touched fallback sources opencode/codex = %d/%d", opencodeSource.overviewCalls, codexSource.overviewCalls)
		}
	})

	t.Run("unavailable Codex returns 503 without OpenCode fallback", func(t *testing.T) {
		opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
		codexSource := newHandlerFakeSource(source.SourceCodex, false, 111)
		handler := newSourceTestHandler(t, opencodeSource, codexSource)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?source=codex&period=all", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body: %s", rec.Code, rec.Body.String())
		}
		if opencodeSource.overviewCalls != 0 {
			t.Errorf("unavailable Codex touched OpenCode fallback %d times", opencodeSource.overviewCalls)
		}
	})

	t.Run("Codex detail collision stays source scoped", func(t *testing.T) {
		opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
		claudeSource := newHandlerFakeSource(source.SourceClaudeCode, true, 777)
		codexSource := codex.New(codex.Options{
			CodexHome:           filepath.Join("..", "source", "codex", "testdata", "valid_home"),
			PathSource:          "test fixture",
			PricingSnapshotPath: filepath.Join("..", "source", "codex", "testdata", "pricing_snapshot.json"),
		})
		handler := newSourceTestHandler(t, opencodeSource, claudeSource, codexSource)

		var detail stats.MessageDetail
		getHandlerJSON(t, handler, "/api/v1/messages/codex:synthetic-session:turn-1?source=codex", &detail)
		if detail.SourceID != string(source.SourceCodex) {
			t.Errorf("detail source_id = %q, want %q", detail.SourceID, source.SourceCodex)
		}
		if strings.HasPrefix(detail.ID, "opencode") || strings.HasPrefix(detail.ID, "claude_code") {
			t.Errorf("detail ID = %q, want Codex-scoped ID only", detail.ID)
		}
	})
}
