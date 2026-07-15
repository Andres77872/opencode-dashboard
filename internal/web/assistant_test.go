package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opencode-dashboard/internal/analyticsagent"
	"opencode-dashboard/internal/source"
)

type fakeAssistantService struct {
	status      analyticsagent.Status
	result      analyticsagent.ChatResult
	err         error
	input       analyticsagent.ChatInput
	calls       int
	statusCalls int
}

func (f *fakeAssistantService) Status(context.Context) analyticsagent.Status {
	f.statusCalls++
	return f.status
}

func (f *fakeAssistantService) Chat(_ context.Context, input analyticsagent.ChatInput) (analyticsagent.ChatResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func assistantTestServer(service AssistantService, logger *slog.Logger) *http.Server {
	return NewServerWithAssistant("", source.NewRegistry(source.SourceOpenCode), logger, nil, nil, service)
}

func validAssistantBody(prompt string) string {
	encoded, _ := json.Marshal(analyticsagent.ChatInput{
		ConsentVersion: analyticsagent.PrivacyConsentVersion,
		Messages: []analyticsagent.BrowserMessage{
			{Role: "user", Content: "Earlier question"},
			{Role: "assistant", Content: "Earlier answer", Signature: "server-signature"},
			{Role: "user", Content: prompt},
		},
		Context: &analyticsagent.BrowserContext{Route: "/models", Source: "codex", Period: "7d", Timezone: "America/Mexico_City"},
	})
	return string(encoded)
}

func newAssistantRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAssistantStatusContractAndNoStore(t *testing.T) {
	status := analyticsagent.BaseStatus()
	status.Available = true
	fake := &fakeAssistantService{status: status}
	server := assistantTestServer(fake, nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
	var body analyticsagent.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Available || body.Provider != "minimax" || body.Model != analyticsagent.MiniMaxM3Model || body.PrivacyNotice == "" || body.ConsentVersion != analyticsagent.PrivacyConsentVersion || len(body.Capabilities) == 0 {
		t.Fatalf("status contract = %#v", body)
	}
}

func TestAssistantStatusWithoutServiceIsUnavailable(t *testing.T) {
	server := assistantTestServer(nil, nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/status", nil))
	var body analyticsagent.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Available || body.Model != analyticsagent.MiniMaxM3Model || body.Reason == "" || body.PrivacyNotice == "" {
		t.Fatalf("status = %#v", body)
	}
}

func TestAssistantStatusRejectsNonlocalOrigin(t *testing.T) {
	fake := &fakeAssistantService{status: analyticsagent.BaseStatus()}
	server := assistantTestServer(fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/assistant/status", nil)
	req.Header.Set("Origin", "https://attacker.example")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || fake.statusCalls != 0 {
		t.Fatalf("status=%d status_calls=%d body=%s", recorder.Code, fake.statusCalls, recorder.Body.String())
	}
}

func TestAssistantStatusRejectsCrossSiteFetchMetadataWithoutProbing(t *testing.T) {
	fake := &fakeAssistantService{status: analyticsagent.BaseStatus()}
	server := assistantTestServer(fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/assistant/status", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || fake.statusCalls != 0 {
		t.Fatalf("status=%d status_calls=%d body=%s", recorder.Code, fake.statusCalls, recorder.Body.String())
	}
}

func TestAssistantChatContractAndStatelessHistory(t *testing.T) {
	fake := &fakeAssistantService{result: analyticsagent.ChatResult{
		Message: analyticsagent.BrowserMessage{Role: "assistant", Content: "Codex usage increased."},
		Model:   analyticsagent.MiniMaxM3Model, ToolsUsed: []string{"get_daily_usage"},
	}}
	server := assistantTestServer(fake, nil)
	recorder := httptest.NewRecorder()
	req := newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat", validAssistantBody("What changed?"))
	server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
	if fake.calls != 1 || len(fake.input.Messages) != 3 || fake.input.Messages[2].Content != "What changed?" || fake.input.Context == nil || fake.input.Context.Source != "codex" || fake.input.ConsentVersion != analyticsagent.PrivacyConsentVersion {
		t.Fatalf("input = %#v, calls=%d", fake.input, fake.calls)
	}
	var result analyticsagent.ChatResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Message.Role != "assistant" || result.Model != analyticsagent.MiniMaxM3Model || len(result.ToolsUsed) != 1 {
		t.Fatalf("response = %#v", result)
	}
}

func TestAssistantChatRejectsNonlocalOriginBeforeService(t *testing.T) {
	fake := &fakeAssistantService{}
	server := assistantTestServer(fake, nil)
	req := newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat", validAssistantBody("Report."))
	req.Header.Set("Origin", "https://attacker.example")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || fake.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, fake.calls, recorder.Body.String())
	}
}

func TestAssistantChatAllowsNoOriginAndLocalOrigin(t *testing.T) {
	tests := []struct {
		origin string
		host   string
	}{
		{host: "example.com"},
		{origin: "http://127.0.0.1:7450", host: "127.0.0.1:7450"},
		{origin: "http://localhost:7451", host: "127.0.0.1:7450"},
		{origin: "http://[::1]:7450", host: "[::1]:7450"},
	}
	for _, test := range tests {
		t.Run(test.origin, func(t *testing.T) {
			fake := &fakeAssistantService{result: analyticsagent.ChatResult{Message: analyticsagent.BrowserMessage{Role: "assistant", Content: "ok"}, Model: analyticsagent.MiniMaxM3Model}}
			server := assistantTestServer(fake, nil)
			req := newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat", validAssistantBody("Report."))
			req.Host = test.host
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			recorder := httptest.NewRecorder()
			server.Handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK || fake.calls != 1 {
				t.Fatalf("origin=%q status=%d calls=%d body=%s", test.origin, recorder.Code, fake.calls, recorder.Body.String())
			}
		})
	}
}

func TestAssistantRejectsUnrelatedLocalOriginAndCrossSiteFetchMetadata(t *testing.T) {
	for _, configure := range []func(*http.Request){
		func(req *http.Request) { req.Header.Set("Origin", "http://localhost:8000") },
		func(req *http.Request) { req.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		fake := &fakeAssistantService{}
		server := assistantTestServer(fake, nil)
		req := newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat", validAssistantBody("Report."))
		req.Host = "127.0.0.1:7450"
		configure(req)
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden || fake.calls != 0 {
			t.Fatalf("status=%d calls=%d body=%s", recorder.Code, fake.calls, recorder.Body.String())
		}
	}
}

func TestAssistantChatValidatesJSONContentAndSize(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"missing content type", "", validAssistantBody("Report."), http.StatusUnsupportedMediaType},
		{"text content type", "text/plain", validAssistantBody("Report."), http.StatusUnsupportedMediaType},
		{"malformed", "application/json", `{`, http.StatusBadRequest},
		{"trailing JSON", "application/json", validAssistantBody("Report.") + `{}`, http.StatusBadRequest},
		{"unknown field", "application/json", `{"messages":[{"role":"user","content":"x"}],"unknown":true}`, http.StatusBadRequest},
		{"invalid role", "application/json", `{"messages":[{"role":"tool","content":"x"}]}`, http.StatusBadRequest},
		{"oversized", "application/json", `{"messages":[{"role":"user","content":"` + strings.Repeat("x", maxAssistantRequestBytes) + `"}]}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAssistantService{}
			server := assistantTestServer(fake, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/chat", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			recorder := httptest.NewRecorder()
			server.Handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.want || fake.calls != 0 {
				t.Fatalf("status=%d want=%d calls=%d body=%s", recorder.Code, tt.want, fake.calls, recorder.Body.String())
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("error Cache-Control = %q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestAssistantChatMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid", analyticsagent.ErrInvalidChat, http.StatusBadRequest},
		{"unavailable", analyticsagent.ErrUnavailable, http.StatusServiceUnavailable},
		{"busy", analyticsagent.ErrBusy, http.StatusTooManyRequests},
		{"provider", analyticsagent.ErrProviderFailure, http.StatusBadGateway},
		{"loop", analyticsagent.ErrLoopLimit, http.StatusBadGateway},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"canceled", context.Canceled, http.StatusRequestTimeout},
		{"unknown", errors.New("unknown"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAssistantService{err: tt.err}
			server := assistantTestServer(fake, nil)
			recorder := httptest.NewRecorder()
			server.Handler.ServeHTTP(recorder, newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat", validAssistantBody("Report.")))
			if recorder.Code != tt.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), tt.err.Error()) && tt.name == "unknown" {
				t.Fatalf("internal error leaked: %s", recorder.Body.String())
			}
		})
	}
}

func TestAssistantPromptIsNotLogged(t *testing.T) {
	const sentinel = "USER_PROMPT_LOG_SENTINEL"
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	fake := &fakeAssistantService{result: analyticsagent.ChatResult{Message: analyticsagent.BrowserMessage{Role: "assistant", Content: "safe answer"}, Model: analyticsagent.MiniMaxM3Model}}
	server := assistantTestServer(fake, logger)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat", validAssistantBody(sentinel)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(logs.String(), sentinel) {
		t.Fatalf("prompt leaked to request log: %s", logs.String())
	}
}
