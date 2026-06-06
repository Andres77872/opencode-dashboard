package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/claudecode"
	"opencode-dashboard/internal/stats"
)

func TestSourceAwareClaudeCodeAPIRoutingFromFixture(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	claudeSource := claudecode.New(claudecode.Options{
		ClaudeHome:          filepath.Join("..", "source", "claudecode", "testdata", "valid_home"),
		PathSource:          "test fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "claudecode", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, opencodeSource, claudeSource)

	t.Run("sources metadata exposes available Claude Code", func(t *testing.T) {
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
		claudeInfo, ok := byID[source.SourceClaudeCode]
		if !ok {
			t.Fatalf("sources response missing claude_code: %#v", body.Sources)
		}
		if !claudeInfo.Available {
			t.Fatalf("claude_code Available = false, want true: %#v", claudeInfo.Diagnostics)
		}
		if claudeInfo.Kind != "jsonl" {
			t.Errorf("claude_code Kind = %q, want jsonl", claudeInfo.Kind)
		}
		if !claudeInfo.ReadOnly || !claudeInfo.LocalOnly {
			t.Errorf("claude_code read_only/local_only = %v/%v, want true/true", claudeInfo.ReadOnly, claudeInfo.LocalOnly)
		}
		if !strings.Contains(strings.ToLower(strings.Join(claudeInfo.Warnings, " ")), "plaintext") {
			t.Errorf("claude_code warnings = %#v, want plaintext warning", claudeInfo.Warnings)
		}
	})

	t.Run("explicit claude overview routes only to Claude source", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?source=claude_code&period=all", nil)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /overview?source=claude_code status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if opencodeSource.overviewCalls != 0 {
			t.Errorf("opencode overview calls = %d, want 0 for explicit claude_code route", opencodeSource.overviewCalls)
		}
		var body stats.OverviewStats
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode overview: %v", err)
		}
		if body.SourceID != string(source.SourceClaudeCode) {
			t.Errorf("overview source_id = %q, want %q", body.SourceID, source.SourceClaudeCode)
		}
		if body.Sessions == 999 {
			t.Errorf("overview sessions = %d, looks like OpenCode fake contamination", body.Sessions)
		}
		if body.Messages != 8 {
			t.Errorf("overview messages = %d, want exactly 8 grouped user-facing Claude interactions", body.Messages)
		}
	})

	t.Run("explicit claude messages return source-tagged Claude rows", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?source=claude_code&period=all&page=1&limit=50", nil)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /messages?source=claude_code status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body stats.MessageList
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode messages: %v", err)
		}
		if body.SourceID != string(source.SourceClaudeCode) {
			t.Errorf("message list source_id = %q, want %q", body.SourceID, source.SourceClaudeCode)
		}
		if len(body.Messages) == 0 {
			t.Fatalf("message list empty, want Claude fixture rows")
		}
		if body.Total != 8 {
			t.Errorf("message list total = %d, want exactly 8 grouped user-facing Claude interactions", body.Total)
		}
		if len(body.Messages) != 8 {
			t.Errorf("message list page len = %d, want exactly 8 grouped user-facing Claude interactions", len(body.Messages))
		}
		for _, msg := range body.Messages {
			if msg.SourceID != string(source.SourceClaudeCode) {
				t.Errorf("message %q source_id = %q, want %q", msg.ID, msg.SourceID, source.SourceClaudeCode)
			}
			if strings.HasPrefix(msg.ID, "opencode") || strings.HasPrefix(msg.SessionID, "opencode") {
				t.Errorf("message row appears contaminated by OpenCode IDs: %#v", msg)
			}
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

func TestClaudeCodeAPIEndpointClassesReturnClaudeSourceTaggedPayloads(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	claudeSource := claudecode.New(claudecode.Options{
		ClaudeHome:          filepath.Join("..", "source", "claudecode", "testdata", "valid_home"),
		PathSource:          "test fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "claudecode", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, opencodeSource, claudeSource)

	tests := []struct {
		name string
		path string
	}{
		{name: "overview", path: "/api/v1/overview?source=claude_code&period=all"},
		{name: "daily", path: "/api/v1/daily?source=claude_code&period=all"},
		{name: "daily dimension", path: "/api/v1/daily?source=claude_code&period=all&dimension=model"},
		{name: "models", path: "/api/v1/models?source=claude_code&period=all"},
		{name: "tools", path: "/api/v1/tools?source=claude_code&period=all"},
		{name: "projects", path: "/api/v1/projects?source=claude_code&period=all"},
		{name: "project detail", path: "/api/v1/projects/-home-andres-projects-alpha?source=claude_code&period=all"},
		{name: "sessions", path: "/api/v1/sessions?source=claude_code&period=all"},
		{name: "session detail", path: "/api/v1/sessions/reported-session?source=claude_code"},
		{name: "messages", path: "/api/v1/messages?source=claude_code&period=all"},
		{name: "message detail", path: "/api/v1/messages/claude_code:reported-session:reported-user-1?source=claude_code"},
		{name: "config", path: "/api/v1/config?source=claude_code"},
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
			if body["source_id"] != string(source.SourceClaudeCode) {
				t.Errorf("%s source_id = %#v, want %q", tt.name, body["source_id"], source.SourceClaudeCode)
			}
		})
	}
}

func TestClaudeCodeSafeShapeHTTPDoesNotExposeMetadataRows(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 999)
	claudeSource := claudecode.New(claudecode.Options{
		ClaudeHome:          filepath.Join("..", "source", "claudecode", "testdata", "safe_shape_home"),
		PathSource:          "safe shape fixture",
		PricingSnapshotPath: filepath.Join("..", "source", "claudecode", "testdata", "pricing_snapshot.json"),
	})
	handler := newSourceTestHandler(t, opencodeSource, claudeSource)

	for _, tt := range []struct {
		name string
		path string
		body any
	}{
		{name: "overview", path: "/api/v1/overview?source=claude_code&period=all", body: &stats.OverviewStats{}},
		{name: "daily", path: "/api/v1/daily?source=claude_code&period=all&granularity=day", body: &stats.DailyStats{}},
		{name: "models", path: "/api/v1/models?source=claude_code&period=all", body: &stats.ModelStats{}},
		{name: "sessions", path: "/api/v1/sessions?source=claude_code&period=all", body: &stats.SessionList{}},
		{name: "messages", path: "/api/v1/messages?source=claude_code&period=all&page=1&limit=50", body: &stats.MessageList{}},
		{name: "session detail", path: "/api/v1/sessions/safe-shape-session?source=claude_code", body: &stats.SessionDetail{}},
		{name: "message detail", path: "/api/v1/messages/claude_code:safe-shape-session:safe-user-1?source=claude_code", body: &stats.MessageDetail{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			getHandlerJSON(t, handler, tt.path, tt.body)
			assertHTTPPayloadDoesNotContain(t, tt.body, safeShapeHTTPForbiddenText()...)
		})
	}

	var overview stats.OverviewStats
	getHandlerJSON(t, handler, "/api/v1/overview?source=claude_code&period=all", &overview)
	if overview.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("overview source_id = %q, want %q", overview.SourceID, source.SourceClaudeCode)
	}
	if overview.Messages != 1 || overview.Sessions != 1 {
		t.Errorf("overview messages/sessions = %d/%d, want 1/1 legitimate safe-shape interaction", overview.Messages, overview.Sessions)
	}

	var daily stats.DailyStats
	getHandlerJSON(t, handler, "/api/v1/daily?source=claude_code&period=all&granularity=day", &daily)
	if daily.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("daily source_id = %q, want %q", daily.SourceID, source.SourceClaudeCode)
	}
	if len(daily.Days) != 1 {
		t.Fatalf("daily days len = %d, want 1: %#v", len(daily.Days), daily.Days)
	}
	if daily.Days[0].Messages != 1 || daily.Days[0].Sessions != 1 {
		t.Errorf("daily day messages/sessions = %d/%d, want 1/1", daily.Days[0].Messages, daily.Days[0].Sessions)
	}

	var models stats.ModelStats
	getHandlerJSON(t, handler, "/api/v1/models?source=claude_code&period=all", &models)
	if models.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("models source_id = %q, want %q", models.SourceID, source.SourceClaudeCode)
	}
	if len(models.Models) != 1 {
		t.Fatalf("models len = %d, want 1: %#v", len(models.Models), models.Models)
	}
	if models.Models[0].Messages != 1 || models.Models[0].Sessions != 1 {
		t.Errorf("model messages/sessions = %d/%d, want 1/1", models.Models[0].Messages, models.Models[0].Sessions)
	}

	var sessions stats.SessionList
	getHandlerJSON(t, handler, "/api/v1/sessions?source=claude_code&period=all", &sessions)
	if sessions.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("sessions source_id = %q, want %q", sessions.SourceID, source.SourceClaudeCode)
	}
	if sessions.Total != 1 || len(sessions.Sessions) != 1 {
		t.Fatalf("sessions total/len = %d/%d, want 1/1: %#v", sessions.Total, len(sessions.Sessions), sessions.Sessions)
	}
	if sessions.Sessions[0].MessageCount != 1 {
		t.Errorf("sessions[0].message_count = %d, want 1", sessions.Sessions[0].MessageCount)
	}

	var messages stats.MessageList
	getHandlerJSON(t, handler, "/api/v1/messages?source=claude_code&period=all&page=1&limit=50", &messages)
	if messages.SourceID != string(source.SourceClaudeCode) {
		t.Errorf("messages source_id = %q, want %q", messages.SourceID, source.SourceClaudeCode)
	}
	if messages.Total != 1 || len(messages.Messages) != 1 {
		t.Fatalf("messages total/len = %d/%d, want 1/1: %#v", messages.Total, len(messages.Messages), messages.Messages)
	}
	msg := messages.Messages[0]
	if msg.ID != "claude_code:safe-shape-session:safe-user-1" {
		t.Errorf("messages[0].id = %q, want legitimate prompt ID", msg.ID)
	}
	if msg.Role == "unknown" {
		t.Errorf("messages[0].role = unknown; metadata taxonomy rows must not leak")
	}
	if msg.FoldedAssistantCalls != 2 {
		t.Errorf("messages[0].folded_assistant_calls = %d, want 2", msg.FoldedAssistantCalls)
	}
	if msg.FoldedToolCalls != 1 {
		t.Errorf("messages[0].folded_tool_calls = %d, want 1", msg.FoldedToolCalls)
	}

	var session stats.SessionDetail
	getHandlerJSON(t, handler, "/api/v1/sessions/safe-shape-session?source=claude_code", &session)
	if session.MessageCount != 1 || len(session.Messages) != 1 {
		t.Errorf("session detail message_count/messages len = %d/%d, want 1/1", session.MessageCount, len(session.Messages))
	}
	for _, row := range session.Messages {
		if row.Role == "unknown" {
			t.Errorf("session detail contains unknown row %#v", row)
		}
	}

	var detail stats.MessageDetail
	getHandlerJSON(t, handler, "/api/v1/messages/claude_code:safe-shape-session:safe-user-1?source=claude_code", &detail)
	if detail.ID != "claude_code:safe-shape-session:safe-user-1" {
		t.Errorf("message detail id = %q, want legitimate prompt ID", detail.ID)
	}
	if detail.FoldedAssistantCalls != 2 {
		t.Errorf("message detail folded_assistant_calls = %d, want 2", detail.FoldedAssistantCalls)
	}
	if detail.FoldedToolCalls != 1 {
		t.Errorf("message detail folded_tool_calls = %d, want 1", detail.FoldedToolCalls)
	}
	if !httpDetailContainsToolResult(detail, "toolu_safe_read", "synthetic tool result from redacted fixture") {
		t.Errorf("message detail does not contain completed safe tool_result: %#v", detail.Content.ToolParts)
	}
}

func getHandlerJSON(t *testing.T, handler http.Handler, path string, dest any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d; body: %s", path, rec.Code, http.StatusOK, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatalf("decode %s response: %v; body: %s", path, err, rec.Body.String())
	}
}

func assertHTTPPayloadDoesNotContain(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal HTTP payload: %v", err)
	}
	body := string(encoded)
	for _, needle := range forbidden {
		if strings.Contains(body, needle) {
			t.Errorf("HTTP payload leaked %q in %s", needle, body)
		}
	}
}

func safeShapeHTTPForbiddenText() []string {
	return []string{
		"METADATA PROMPT-LIKE TEXT",
		"metadata mode event must not leak",
		"metadata title event must not leak",
		"metadata last prompt event must not leak",
		"metadata permission event must not leak",
		"metadata file history event must not leak",
		"metadata agent event must not leak",
		"metadata attachment event must not leak",
		"metadata queue event must not leak",
		"metadata system event must not leak",
		"Nested subagent prompt must be skipped",
		"Nested tool-results prompt must be skipped",
		"Nested debug prompt must be skipped",
		`"role":"unknown"`,
	}
}

func httpDetailContainsToolResult(detail stats.MessageDetail, callID, outputContains string) bool {
	for _, tool := range detail.Content.ToolParts {
		if tool.CallID == callID && tool.State.Status == "completed" && strings.Contains(tool.State.Output, outputContains) {
			return true
		}
	}
	return false
}
