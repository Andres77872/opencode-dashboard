package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/kimicode"
	"opencode-dashboard/internal/stats"
)

func TestSourceAwareKimiCodeAPIRoutingFromFixture(t *testing.T) {
	kimiHome := writeWebKimiFixture(t)
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	kimiSource := kimicode.New(kimicode.Options{KimiHome: kimiHome, PathSource: "test fixture"})
	handler := newSourceTestHandler(t, opencodeSource, kimiSource)

	t.Run("sources metadata exposes available Kimi Code", func(t *testing.T) {
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
		kimiInfo, ok := byID[source.SourceKimiCode]
		if !ok {
			t.Fatalf("sources response missing kimi_code: %#v", body.Sources)
		}
		if !kimiInfo.Available || kimiInfo.Kind != "jsonl" || !kimiInfo.ReadOnly || !kimiInfo.LocalOnly {
			t.Errorf("Kimi source metadata = %#v, want available local read-only JSONL", kimiInfo)
		}
		if kimiInfo.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) ||
			kimiInfo.CostPolicy.PricingSnapshotID != "kimi-api-pricing-2026-07-16" {
			t.Errorf("Kimi cost policy = %#v, want bundled API-equivalent snapshot", kimiInfo.CostPolicy)
		}
		if !kimiInfo.Privacy.PlaintextTranscripts || !kimiInfo.Privacy.Redaction {
			t.Errorf("Kimi privacy metadata = %#v, want plaintext/redaction disclosure", kimiInfo.Privacy)
		}
	})

	t.Run("explicit Kimi overview routes only to Kimi Code", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?source=kimi_code&period=all", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET Kimi overview status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if opencodeSource.overviewCalls != 0 {
			t.Errorf("Kimi overview touched OpenCode fallback %d times", opencodeSource.overviewCalls)
		}
		var body stats.OverviewStats
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode Kimi overview: %v", err)
		}
		if body.SourceID != string(source.SourceKimiCode) || body.Sessions != 1 || body.Messages != 2 {
			t.Errorf("Kimi overview = %#v, want one prompt plus one API request", body)
		}
	})

	t.Run("all endpoint classes remain source scoped and redacted", func(t *testing.T) {
		var projects stats.ProjectStats
		getHandlerJSON(t, handler, "/api/v1/projects?source=kimi_code&period=all", &projects)
		if len(projects.Projects) != 1 || projects.Projects[0].ProjectID == "" {
			t.Fatalf("Kimi projects = %#v, want one collision-resistant project id", projects.Projects)
		}
		projectDetailPath := "/api/v1/projects/" + url.PathEscape(projects.Projects[0].ProjectID) + "?source=kimi_code&period=all"
		paths := []string{
			"/api/v1/daily?source=kimi_code&period=all",
			"/api/v1/daily?source=kimi_code&period=all&dimension=model",
			"/api/v1/models?source=kimi_code&period=all",
			"/api/v1/tools?source=kimi_code&period=all",
			"/api/v1/projects?source=kimi_code&period=all",
			projectDetailPath,
			"/api/v1/sessions?source=kimi_code&period=all",
			"/api/v1/sessions/session-main?source=kimi_code",
			"/api/v1/messages?source=kimi_code&period=all",
			"/api/v1/config?source=kimi_code",
		}
		for _, path := range paths {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d; body: %s", path, rec.Code, http.StatusOK, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s response: %v", path, err)
			}
			if body["source_id"] != string(source.SourceKimiCode) {
				t.Errorf("%s source_id = %#v, want %q", path, body["source_id"], source.SourceKimiCode)
			}
			assertNoKimiWebFixtureSecrets(t, path, rec.Body.String())
		}

		var messages stats.MessageList
		getHandlerJSON(t, handler, "/api/v1/messages?source=kimi_code&period=all", &messages)
		var assistantID string
		for _, message := range messages.Messages {
			if message.Role == "assistant" {
				assistantID = message.ID
				break
			}
		}
		if assistantID == "" {
			t.Fatalf("Kimi assistant request missing from %#v", messages.Messages)
		}

		detailPath := "/api/v1/messages/" + url.PathEscape(assistantID) + "?source=kimi_code"
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, detailPath, nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET Kimi message detail status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var detail stats.MessageDetail
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode Kimi message detail: %v", err)
		}
		if detail.SourceID != string(source.SourceKimiCode) || detail.ID != assistantID {
			t.Errorf("Kimi message detail = %#v, want source-scoped request %q", detail, assistantID)
		}
		assertNoKimiWebFixtureSecrets(t, detailPath, rec.Body.String())
	})
}

func writeWebKimiFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "wd_fixture", "session_session-main")
	agentDir := filepath.Join(sessionDir, "agents", "main")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create Kimi web fixture: %v", err)
	}
	state := `{
  "createdAt": "2026-07-16T10:00:00Z",
  "updatedAt": "2026-07-16T10:01:00Z",
  "title": "Kimi web fixture",
  "workDir": "/private/kimi-web-fixture/kimi-project",
  "agents": {"main": {"type": "main", "parentAgentId": null}}
}`
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write Kimi state: %v", err)
	}
	wire := strings.Join([]string{
		`{"type":"turn.prompt","input":[{"type":"text","text":"Inspect /private/kimi-web-fixture/kimi-project"}],"origin":{"kind":"user"},"time":1784196001000}`,
		`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001100}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.call","uuid":"call-1","toolCallId":"call-1","name":"Read","args":{"path":"/private/kimi-web-fixture/kimi-project/README.md"},"description":"secret=MUST_NOT_LEAK"},"time":1784196001200}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.result","toolCallId":"call-1","result":{"output":"MUST_NOT_LEAK","isError":false}},"time":1784196001300}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":100,"inputCacheRead":200,"inputCacheCreation":10,"output":20},"usageScope":"turn","time":1784196001400}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(agentDir, "wire.jsonl"), []byte(wire), 0o644); err != nil {
		t.Fatalf("write Kimi wire: %v", err)
	}
	config := `
default_model = "kimi-code/k3"
[providers.kimi]
api_key = "MUST_NOT_LEAK"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write Kimi config: %v", err)
	}
	return home
}

func assertNoKimiWebFixtureSecrets(t *testing.T, endpoint, body string) {
	t.Helper()
	for _, forbidden := range []string{"/private/kimi-web-fixture", "MUST_NOT_LEAK"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s leaked %q in %s", endpoint, forbidden, body)
		}
	}
}
