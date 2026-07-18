package web

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
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
		if codexInfo.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) || codexInfo.CostPolicy.PricingSnapshotID != "openai-codex-api-pricing-2026-07-17" {
			t.Errorf("codex cost policy = %#v, want current estimated API-equivalent snapshot", codexInfo.CostPolicy)
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
		if body.Messages != 4 {
			t.Errorf("overview messages = %d, want 4 per-request Codex rows", body.Messages)
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
		{name: "message detail", path: "/api/v1/messages/codex:synthetic-session:turn-1:r0?source=codex"},
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
		getHandlerJSON(t, handler, "/api/v1/messages/codex:synthetic-session:turn-1:r0?source=codex", &detail)
		if detail.SourceID != string(source.SourceCodex) {
			t.Errorf("detail source_id = %q, want %q", detail.SourceID, source.SourceCodex)
		}
		if strings.HasPrefix(detail.ID, "opencode") || strings.HasPrefix(detail.ID, "claude_code") {
			t.Errorf("detail ID = %q, want Codex-scoped ID only", detail.ID)
		}
	})
}

func TestCodexRequestedProcessingModeFlowsThroughAPI(t *testing.T) {
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "2026", "07", "17")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create Codex fixture directory: %v", err)
	}
	lines := []string{
		`{"timestamp":"2026-07-17T09:00:00Z","type":"session_meta","payload":{"id":"api-tier-session","model_provider":"openai","cwd":"[REDACTED_PATH]/api-tier-project"}}`,
		`{"timestamp":"2026-07-17T09:00:01Z","type":"turn_context","payload":{"turn_id":"api-tier-turn","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"2026-07-17T09:00:02Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}`,
		`{"timestamp":"2026-07-17T09:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"api-tier-turn","message":"[REDACTED_API_TIER_PROMPT]"}}`,
		`{"timestamp":"2026-07-17T09:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"api-tier-turn","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}`,
		`{"timestamp":"2026-07-17T09:00:05Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"default"}}}`,
		`{"timestamp":"2026-07-17T09:00:06Z","type":"event_msg","payload":{"type":"token_count","turn_id":"api-tier-turn","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":50,"output_tokens":50,"reasoning_output_tokens":10,"total_tokens":350}}}}`,
	}
	rollout := filepath.Join(sessionDir, "rollout-2026-07-17T09-00-00Z-api-tier-session.jsonl")
	if err := os.WriteFile(rollout, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write Codex fixture: %v", err)
	}

	codexSource := codex.New(codex.Options{
		CodexHome:           home,
		PathSource:          "test fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "codex", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, newHandlerFakeSource(source.SourceOpenCode, true, 999), codexSource)

	var messages stats.MessageList
	getHandlerJSON(t, handler, "/api/v1/messages?source=codex&period=all", &messages)
	if messages.Total != 3 {
		t.Fatalf("Codex messages total = %d, want one user and two assistant requests", messages.Total)
	}
	assertAPITierMessage := func(id, tier string, mode stats.ProcessingMode) {
		t.Helper()
		for _, message := range messages.Messages {
			if message.ID != id {
				continue
			}
			if message.ServiceTier != tier || message.ProcessingMode != mode {
				t.Errorf("message %q tier/mode = %q/%q, want %q/%q", id, message.ServiceTier, message.ProcessingMode, tier, mode)
			}
			return
		}
		t.Errorf("messages response missing %q", id)
	}
	assertAPITierMessage("codex:api-tier-session:api-tier-turn:r0", "priority", stats.ProcessingModeFast)
	assertAPITierMessage("codex:api-tier-session:api-tier-turn:r1", "default", stats.ProcessingModeStandard)
	for _, message := range messages.Messages {
		switch message.ID {
		case "codex:api-tier-session:api-tier-turn:r0":
			if math.Abs(message.Cost-0.002525) > 1e-12 {
				t.Errorf("Fast/Priority API cost = %.9f, want 0.002525", message.Cost)
			}
		case "codex:api-tier-session:api-tier-turn:r1":
			if math.Abs(message.Cost-0.001765) > 1e-12 {
				t.Errorf("Standard API cost = %.9f, want 0.001765", message.Cost)
			}
		}
	}

	var detail stats.MessageDetail
	getHandlerJSON(t, handler, "/api/v1/messages/codex:api-tier-session:api-tier-turn:r0?source=codex", &detail)
	if detail.ServiceTier != "priority" || detail.ProcessingMode != stats.ProcessingModeFast {
		t.Errorf("message detail tier/mode = %q/%q, want priority/fast", detail.ServiceTier, detail.ProcessingMode)
	}

	var session stats.SessionDetail
	getHandlerJSON(t, handler, "/api/v1/sessions/api-tier-session?source=codex", &session)
	if len(session.Messages) != 3 {
		t.Fatalf("session messages = %d, want 3", len(session.Messages))
	}
	for _, message := range session.Messages {
		if message.Role == "user" && (message.ServiceTier != "" || message.ProcessingMode != "") {
			t.Errorf("user session row tier/mode = %q/%q, want omitted", message.ServiceTier, message.ProcessingMode)
		}
	}

	var dimension stats.DailyDimensionStats
	getHandlerJSON(t, handler, "/api/v1/daily?source=codex&period=all&dimension=processing_mode", &dimension)
	if dimension.Dimension != "processing_mode" {
		t.Errorf("dimension = %q, want processing_mode", dimension.Dimension)
	}
	type modeTotal struct {
		messages int64
		tokens   int64
	}
	got := make(map[string]modeTotal)
	for _, row := range dimension.Days {
		got[row.Dimension] = modeTotal{
			messages: row.Messages,
			tokens:   row.Tokens.Input + row.Tokens.Cache.Read + row.Tokens.Cache.Write + row.Tokens.Output + row.Tokens.Reasoning,
		}
	}
	want := map[string]modeTotal{
		"fast":     {messages: 1, tokens: 120},
		"standard": {messages: 1, tokens: 230},
	}
	for mode, expected := range want {
		if got[mode] != expected {
			t.Errorf("processing mode %q = %+v, want %+v", mode, got[mode], expected)
		}
	}
}

// TestDailyDimensionGranularityParam proves the dimension route honors the
// same granularity query parameter as the plain daily route, end to end
// through a real source adapter.
func TestDailyDimensionGranularityParam(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	claudeSource := newHandlerFakeSource(source.SourceClaudeCode, true, 777)
	codexSource := codex.New(codex.Options{
		CodexHome:           filepath.Join("..", "source", "codex", "testdata", "valid_home"),
		PathSource:          "test fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "codex", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, opencodeSource, claudeSource, codexSource)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/daily?source=codex&period=all&dimension=model&granularity=hour", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("granularity=hour dimension status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body stats.DailyDimensionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode hourly dimension payload: %v", err)
	}
	if body.Granularity != stats.GranularityHour {
		t.Errorf("granularity = %q, want %q", body.Granularity, stats.GranularityHour)
	}
	if len(body.Days) == 0 {
		t.Fatal("hourly model dimension returned no rows, want fixture data")
	}
	for _, day := range body.Days {
		if len(day.Date) != len("2006-01-02T15:04:05Z") {
			t.Errorf("hourly dimension date = %q, want an hour bucket key", day.Date)
		}
	}
}
