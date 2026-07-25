package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/analyticsagent"
	"opencode-dashboard/internal/chatstore"
	"opencode-dashboard/internal/source"
)

type fakeAssistantService struct {
	status       analyticsagent.Status
	result       analyticsagent.ChatResult
	err          error
	input        analyticsagent.ChatInput
	calls        int
	statusCalls  int
	streamResult analyticsagent.ChatResult
	streamErr    error
	streamEvents []analyticsagent.StreamEvent
	streamInput  analyticsagent.ChatInput
	streamCalls  int
	streamFunc   func(context.Context, analyticsagent.ChatInput, func(analyticsagent.StreamEvent) error) (analyticsagent.ChatResult, error)
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

func (f *fakeAssistantService) ChatStream(ctx context.Context, input analyticsagent.ChatInput, emit func(analyticsagent.StreamEvent) error) (analyticsagent.ChatResult, error) {
	f.streamCalls++
	f.streamInput = input
	if f.streamFunc != nil {
		return f.streamFunc(ctx, input, emit)
	}
	for _, event := range f.streamEvents {
		if err := emit(event); err != nil {
			return analyticsagent.ChatResult{}, err
		}
	}
	return f.streamResult, f.streamErr
}

type bufferedOnlyAssistantService struct {
	status analyticsagent.Status
}

func (f *bufferedOnlyAssistantService) Status(context.Context) analyticsagent.Status {
	return f.status
}

func (f *bufferedOnlyAssistantService) Chat(context.Context, analyticsagent.ChatInput) (analyticsagent.ChatResult, error) {
	return analyticsagent.ChatResult{}, nil
}

type assistantStreamTestFrame struct {
	Type      string          `json:"type"`
	Delta     string          `json:"delta"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	OK        *bool           `json:"ok"`
	Model     string          `json:"model"`
	Message   json.RawMessage `json:"message"`
	ToolsUsed []string        `json:"tools_used"`
}

func decodeAssistantStreamFrames(t *testing.T, reader io.Reader) []assistantStreamTestFrame {
	t.Helper()
	var frames []assistantStreamTestFrame
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var frame assistantStreamTestFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatalf("decode stream frame %q: %v", scanner.Text(), err)
		}
		frames = append(frames, frame)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}
	return frames
}

type flushCountingRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (w *flushCountingRecorder) Flush() {
	w.flushes++
	w.ResponseRecorder.Flush()
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

func TestAssistantChatStreamContractAndFlushesEveryFrame(t *testing.T) {
	toolOK := false
	result := analyticsagent.ChatResult{
		Message: analyticsagent.BrowserMessage{
			Role:      "assistant",
			Content:   "Final report.",
			Signature: "signed-final-report",
		},
		Model:     analyticsagent.MiniMaxM3Model,
		ToolsUsed: []string{"list_sources"},
	}
	fake := &fakeAssistantService{
		streamResult: result,
		streamEvents: []analyticsagent.StreamEvent{
			{Type: analyticsagent.StreamEventContentDelta, Delta: "Checking sources..."},
			{Type: analyticsagent.StreamEventToolStart, CallID: "tool-1", Name: "list_sources"},
			{Type: analyticsagent.StreamEventToolFinish, CallID: "tool-1", Name: "list_sources", OK: &toolOK},
			{Type: analyticsagent.StreamEventContentReset},
			{Type: analyticsagent.StreamEventContentDelta, Delta: "Final report."},
		},
	}
	server := assistantTestServer(fake, nil)
	recorder := &flushCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat/stream", validAssistantBody("Stream a report."))
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stream safety headers = %#v", recorder.Header())
	}
	if fake.streamCalls != 1 || fake.streamInput.Messages[2].Content != "Stream a report." || fake.streamInput.Context == nil || fake.streamInput.Context.Source != "codex" {
		t.Fatalf("stream input = %#v, calls=%d", fake.streamInput, fake.streamCalls)
	}

	frames := decodeAssistantStreamFrames(t, strings.NewReader(recorder.Body.String()))
	wantTypes := []string{"start", "content_delta", "tool_start", "tool_finish", "content_reset", "content_delta", "complete"}
	if len(frames) != len(wantTypes) {
		t.Fatalf("frames = %d, want %d: %s", len(frames), len(wantTypes), recorder.Body.String())
	}
	for index, want := range wantTypes {
		if frames[index].Type != want {
			t.Fatalf("frame %d type = %q, want %q: %s", index, frames[index].Type, want, recorder.Body.String())
		}
	}
	if recorder.flushes != len(frames) {
		t.Fatalf("flushes = %d, want one per %d frames", recorder.flushes, len(frames))
	}
	if frames[0].Model != analyticsagent.MiniMaxM3Model {
		t.Errorf("start model = %q", frames[0].Model)
	}
	if frames[1].Delta != "Checking sources..." || frames[5].Delta != "Final report." {
		t.Errorf("content frames = %#v, %#v", frames[1], frames[5])
	}
	if frames[2].CallID != "tool-1" || frames[2].Name != "list_sources" {
		t.Errorf("tool_start = %#v", frames[2])
	}
	if frames[3].OK == nil || *frames[3].OK || frames[3].CallID != "tool-1" || frames[3].Name != "list_sources" {
		t.Errorf("tool_finish = %#v, want explicit ok=false", frames[3])
	}
	var complete analyticsagent.BrowserMessage
	if err := json.Unmarshal(frames[6].Message, &complete); err != nil {
		t.Fatalf("decode complete message: %v", err)
	}
	if complete != result.Message || frames[6].Model != result.Model || strings.Join(frames[6].ToolsUsed, ",") != "list_sources" {
		t.Fatalf("complete frame = %#v, message=%#v", frames[6], complete)
	}
	for _, forbidden := range []string{"tool_args", "tool_result", "reasoning", "provider-call"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("private provider/tool data %q leaked in stream: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAssistantChatStreamDeliversContentBeforeServiceCompletes(t *testing.T) {
	release := make(chan struct{}, 1)
	serviceFinished := make(chan struct{})
	fake := &fakeAssistantService{}
	fake.streamFunc = func(_ context.Context, _ analyticsagent.ChatInput, emit func(analyticsagent.StreamEvent) error) (analyticsagent.ChatResult, error) {
		defer close(serviceFinished)
		if err := emit(analyticsagent.StreamEvent{Type: analyticsagent.StreamEventContentDelta, Delta: "first chunk"}); err != nil {
			return analyticsagent.ChatResult{}, err
		}
		<-release
		return analyticsagent.ChatResult{
			Message: analyticsagent.BrowserMessage{Role: "assistant", Content: "first chunk", Signature: "signed"},
			Model:   analyticsagent.MiniMaxM3Model,
		}, nil
	}
	server := assistantTestServer(fake, nil)
	httpServer := httptest.NewServer(server.Handler)
	t.Cleanup(func() {
		select {
		case release <- struct{}{}:
		default:
		}
		httpServer.Close()
	})

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/assistant/chat/stream", strings.NewReader(validAssistantBody("Stream now.")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}

	scanner := bufio.NewScanner(response.Body)
	for index, wantType := range []string{"start", "content_delta"} {
		if !scanner.Scan() {
			t.Fatalf("stream ended before frame %d: %v", index, scanner.Err())
		}
		var frame assistantStreamTestFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Type != wantType {
			t.Fatalf("frame %d type = %q, want %q", index, frame.Type, wantType)
		}
		if wantType == "content_delta" && frame.Delta != "first chunk" {
			t.Fatalf("first streamed delta = %q", frame.Delta)
		}
	}
	select {
	case <-serviceFinished:
		t.Fatal("service completed before the client observed streamed content")
	default:
	}
	release <- struct{}{}
	if !scanner.Scan() {
		t.Fatalf("stream ended before complete frame: %v", scanner.Err())
	}
	var complete assistantStreamTestFrame
	if err := json.Unmarshal(scanner.Bytes(), &complete); err != nil {
		t.Fatal(err)
	}
	if complete.Type != "complete" {
		t.Fatalf("terminal frame type = %q, want complete", complete.Type)
	}
	if complete.ToolsUsed == nil {
		t.Fatal("complete tools_used must be an empty array, not null")
	}
}

func TestAssistantChatStreamRetainsJSONErrorsBeforeCommit(t *testing.T) {
	tests := []struct {
		name        string
		service     AssistantService
		body        string
		contentType string
		wantStatus  int
		wantMessage string
	}{
		{name: "unsupported streaming", service: &bufferedOnlyAssistantService{}, body: validAssistantBody("Report."), contentType: "application/json", wantStatus: http.StatusServiceUnavailable},
		{name: "malformed request", service: &fakeAssistantService{}, body: `{`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "busy before emit", service: &fakeAssistantService{streamErr: analyticsagent.ErrBusy}, body: validAssistantBody("Report."), contentType: "application/json", wantStatus: http.StatusTooManyRequests},
		{name: "unavailable before emit", service: &fakeAssistantService{streamErr: analyticsagent.ErrUnavailable}, body: validAssistantBody("Report."), contentType: "application/json", wantStatus: http.StatusServiceUnavailable},
		{
			name: "usage exhausted before emit",
			service: &fakeAssistantService{streamErr: errors.Join(
				analyticsagent.ErrProviderFailure,
				&analyticsagent.RateLimitError{Operation: "stream chat completion", ProviderCode: 2056},
			)},
			body:        validAssistantBody("Report."),
			contentType: "application/json",
			wantStatus:  http.StatusTooManyRequests,
			wantMessage: "MiniMax usage is temporarily limited; try again later",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := assistantTestServer(tt.service, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/chat/stream", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			recorder := httptest.NewRecorder()
			server.Handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type=%q, want application/json before stream commit", got)
			}
			var apiErr APIError
			if err := json.Unmarshal(recorder.Body.Bytes(), &apiErr); err != nil || apiErr.Code != tt.wantStatus || apiErr.Message == "" {
				t.Fatalf("API error=%#v decode_err=%v body=%s", apiErr, err, recorder.Body.String())
			}
			if tt.wantMessage != "" && apiErr.Message != tt.wantMessage {
				t.Fatalf("API error message = %q, want %q", apiErr.Message, tt.wantMessage)
			}
		})
	}
}

func TestAssistantChatStreamSanitizesErrorsAfterCommit(t *testing.T) {
	const secret = "PRIVATE_PROVIDER_FAILURE_SENTINEL"
	fake := &fakeAssistantService{
		streamEvents: []analyticsagent.StreamEvent{{Type: analyticsagent.StreamEventContentDelta, Delta: "Partial report"}},
		streamErr:    errors.Join(analyticsagent.ErrProviderFailure, errors.New(secret)),
	}
	server := assistantTestServer(fake, nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat/stream", validAssistantBody("Report.")))

	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	frames := decodeAssistantStreamFrames(t, strings.NewReader(recorder.Body.String()))
	if len(frames) != 3 || frames[0].Type != "start" || frames[1].Type != "content_delta" || frames[2].Type != "error" {
		t.Fatalf("frames=%#v body=%s", frames, recorder.Body.String())
	}
	var publicMessage string
	if err := json.Unmarshal(frames[2].Message, &publicMessage); err != nil {
		t.Fatalf("decode error message: %v", err)
	}
	if publicMessage != "MiniMax could not complete the analytics report" || strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("unsafe streamed error message=%q body=%s", publicMessage, recorder.Body.String())
	}
}

func TestAssistantChatStreamMapsUsageExhaustionAfterCommit(t *testing.T) {
	const secret = "PRIVATE_USAGE_LIMIT_MESSAGE_SENTINEL"
	fake := &fakeAssistantService{
		streamEvents: []analyticsagent.StreamEvent{{Type: analyticsagent.StreamEventContentDelta, Delta: "Partial report"}},
		streamErr: errors.Join(
			analyticsagent.ErrProviderFailure,
			&analyticsagent.RateLimitError{Operation: "stream chat completion", ProviderCode: 2056, Message: secret},
		),
	}
	server := assistantTestServer(fake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat/stream", validAssistantBody("Report.")))

	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	frames := decodeAssistantStreamFrames(t, strings.NewReader(recorder.Body.String()))
	if len(frames) != 3 || frames[2].Type != "error" {
		t.Fatalf("frames=%#v body=%s", frames, recorder.Body.String())
	}
	var publicMessage string
	if err := json.Unmarshal(frames[2].Message, &publicMessage); err != nil {
		t.Fatalf("decode error message: %v", err)
	}
	if publicMessage != "MiniMax usage is temporarily limited; try again later" || strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("unsafe streamed usage error message=%q body=%s", publicMessage, recorder.Body.String())
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
		{"authentication", &analyticsagent.AuthenticationError{Operation: "list models", StatusCode: http.StatusUnauthorized, ProviderCode: 2049}, http.StatusServiceUnavailable},
		{"model unavailable", &analyticsagent.ModelUnavailableError{Model: analyticsagent.MiniMaxM3Model}, http.StatusServiceUnavailable},
		{"busy", analyticsagent.ErrBusy, http.StatusTooManyRequests},
		{"provider", analyticsagent.ErrProviderFailure, http.StatusBadGateway},
		{"typed provider", &analyticsagent.ProviderError{Operation: "chat completion", StatusCode: http.StatusBadGateway}, http.StatusBadGateway},
		{"provider usage limited", errors.Join(analyticsagent.ErrProviderFailure, &analyticsagent.RateLimitError{Operation: "chat completion", ProviderCode: 2056}), http.StatusTooManyRequests},
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

func TestAssistantFailureLoggingIsStructuredAndPrivacySafe(t *testing.T) {
	const (
		promptSentinel          = "PRIVATE_PROMPT_SENTINEL"
		providerMessageSentinel = "PRIVATE_PROVIDER_MESSAGE_SENTINEL"
		reasoningSentinel       = "PRIVATE_REASONING_SENTINEL"
		credentialSentinel      = "PRIVATE_CREDENTIAL_SENTINEL"
		toolArgumentsSentinel   = "PRIVATE_TOOL_ARGUMENTS_SENTINEL"
		toolResultSentinel      = "PRIVATE_TOOL_RESULT_SENTINEL"
		providerCallIDSentinel  = "PRIVATE_PROVIDER_CALL_ID_SENTINEL"
	)
	providerErr := &analyticsagent.ProviderError{
		Operation:    "stream chat completion",
		StatusCode:   http.StatusBadGateway,
		ProviderCode: 1008,
		Message: strings.Join([]string{
			providerMessageSentinel,
			reasoningSentinel,
			credentialSentinel,
			toolArgumentsSentinel,
			toolResultSentinel,
			providerCallIDSentinel,
		}, " "),
	}
	fake := &fakeAssistantService{
		streamEvents: []analyticsagent.StreamEvent{{Type: analyticsagent.StreamEventContentDelta, Delta: "Safe partial report"}},
		streamErr:    errors.Join(analyticsagent.ErrProviderFailure, providerErr),
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := assistantTestServer(fake, logger)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat/stream", validAssistantBody(promptSentinel)))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{
		promptSentinel,
		providerMessageSentinel,
		reasoningSentinel,
		credentialSentinel,
		toolArgumentsSentinel,
		toolResultSentinel,
		providerCallIDSentinel,
	} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("private value %q leaked to assistant logs: %s", forbidden, logs.String())
		}
	}

	decoder := json.NewDecoder(strings.NewReader(logs.String()))
	var failure map[string]any
	for {
		var record map[string]any
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode log record: %v\n%s", err, logs.String())
		}
		if record["msg"] == "analytics assistant failure" {
			failure = record
		}
	}
	if failure == nil {
		t.Fatalf("assistant failure log missing: %s", logs.String())
	}
	wantFields := map[string]any{
		"endpoint_mode":        "stream",
		"committed":            true,
		"error_class":          "provider",
		"operation":            "stream chat completion",
		"provider_http_status": float64(http.StatusBadGateway),
		"provider_code":        float64(1008),
	}
	for name, want := range wantFields {
		if got := failure[name]; got != want {
			t.Errorf("failure log field %s = %#v, want %#v; record=%#v", name, got, want, failure)
		}
	}
	allowedFields := map[string]struct{}{
		"time": {}, "level": {}, "msg": {},
		"endpoint_mode": {}, "committed": {}, "error_class": {}, "operation": {},
		"provider_http_status": {}, "provider_code": {},
	}
	for name := range failure {
		if _, ok := allowedFields[name]; !ok {
			t.Errorf("unexpected assistant failure log field %q in %#v", name, failure)
		}
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

func assistantTestServerWithChatLog(t *testing.T, service AssistantService) (*http.Server, *chatstore.Store) {
	t.Helper()
	store, err := chatstore.Open(context.Background(), filepath.Join(t.TempDir(), "assistant-chat.sqlite"))
	if err != nil {
		t.Fatalf("open chat store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := NewServerWithChatLog("", source.NewRegistry(source.SourceOpenCode), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, service, store)
	return server, store
}

func TestAssistantChatStreamPersistsTurnWithToolCalls(t *testing.T) {
	okValue := true
	service := &fakeAssistantService{
		streamEvents: []analyticsagent.StreamEvent{
			{Type: analyticsagent.StreamEventToolStart, CallID: "tool-1", Name: "get_overview", Arguments: json.RawMessage(`{"source":"opencode"}`)},
			{Type: analyticsagent.StreamEventToolFinish, CallID: "tool-1", Name: "get_overview", OK: &okValue, Result: json.RawMessage(`{"ok":true,"data":{}}`), DurationMS: 25},
			{Type: analyticsagent.StreamEventContentDelta, Delta: "Report body."},
		},
		streamResult: analyticsagent.ChatResult{
			Message:   analyticsagent.BrowserMessage{Role: "assistant", Content: "Report body.", Signature: "sig"},
			Model:     "MiniMax-M3",
			ToolsUsed: []string{"get_overview"},
			ToolCalls: []analyticsagent.ToolCallRecord{{
				CallID: "tool-1", Name: "get_overview",
				Arguments: json.RawMessage(`{"source":"opencode"}`),
				Result:    json.RawMessage(`{"ok":true,"data":{}}`),
				OK:        true, DurationMS: 25,
			}},
		},
	}
	server, store := assistantTestServerWithChatLog(t, service)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/chat/stream", strings.NewReader(validAssistantBody("Summarize.")))
	request.Header.Set("Content-Type", "application/json")
	recorder := &flushCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var sessionID string
	var sawToolStartArgs, sawToolFinishResult bool
	scanner := bufio.NewScanner(bytes.NewReader(recorder.Body.Bytes()))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("frame %q: %v", line, err)
		}
		switch frame["type"] {
		case "tool_start":
			if arguments, ok := frame["arguments"].(map[string]any); ok && arguments["source"] == "opencode" {
				sawToolStartArgs = true
			}
		case "tool_finish":
			if result, ok := frame["result"].(map[string]any); ok && result["ok"] == true {
				sawToolFinishResult = true
			}
			if frame["duration_ms"] != float64(25) {
				t.Fatalf("tool_finish duration = %v", frame["duration_ms"])
			}
		case "complete":
			sessionID, _ = frame["session_id"].(string)
			calls, ok := frame["tool_calls"].([]any)
			if !ok || len(calls) != 1 {
				t.Fatalf("complete tool_calls = %v", frame["tool_calls"])
			}
		}
	}
	if !sawToolStartArgs || !sawToolFinishResult {
		t.Fatalf("tool frames missing arguments/result: args=%v result=%v body=%s", sawToolStartArgs, sawToolFinishResult, recorder.Body.String())
	}
	if !chatstore.IsValidSessionID(sessionID) {
		t.Fatalf("complete frame session_id = %q", sessionID)
	}

	detail, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if len(detail.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(detail.Messages))
	}
	if detail.Messages[0].Content != "Summarize." || detail.Messages[1].Content != "Report body." {
		t.Fatalf("persisted contents = %q, %q", detail.Messages[0].Content, detail.Messages[1].Content)
	}
	if detail.Messages[1].Signature != "sig" {
		t.Fatalf("persisted signature = %q", detail.Messages[1].Signature)
	}
	toolCalls := detail.Messages[1].ToolCalls
	if len(toolCalls) != 1 || toolCalls[0].Name != "get_overview" || !toolCalls[0].OK {
		t.Fatalf("persisted tool calls = %+v", toolCalls)
	}
	if string(toolCalls[0].Arguments) != `{"source":"opencode"}` || !strings.Contains(string(toolCalls[0].Result), `"ok":true`) {
		t.Fatalf("persisted tool IO = %s / %s", toolCalls[0].Arguments, toolCalls[0].Result)
	}

	// A follow-up turn addressed to the same session must append to it.
	followBody, _ := json.Marshal(analyticsagent.ChatInput{
		ConsentVersion: analyticsagent.PrivacyConsentVersion,
		SessionID:      sessionID,
		Messages: []analyticsagent.BrowserMessage{
			{Role: "user", Content: "Follow up."},
		},
	})
	request = httptest.NewRequest(http.MethodPost, "/api/v1/assistant/chat/stream", bytes.NewReader(followBody))
	request.Header.Set("Content-Type", "application/json")
	recorder = &flushCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	detail, err = store.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession follow-up: %v", err)
	}
	if len(detail.Messages) != 4 {
		t.Fatalf("messages after follow-up = %d, want 4", len(detail.Messages))
	}
}

func TestAssistantChatStreamForwardsSpecialistProgressAndPersistsIt(t *testing.T) {
	okValue := true
	usage := analyticsagent.Usage{Requests: 2, InputTokens: 600, OutputTokens: 90, TotalTokens: 690}
	service := &fakeAssistantService{
		streamEvents: []analyticsagent.StreamEvent{
			{Type: analyticsagent.StreamEventRoundStart, Agent: analyticsagent.AgentLead, Round: 1},
			{
				Type: analyticsagent.StreamEventSubagentStart, Agent: analyticsagent.AgentLead, CallID: "tool-1",
				Subagent: &analyticsagent.SubagentEvent{
					Agent: analyticsagent.AgentTrend, Title: "Trend analyst", Task: "Explain the 7-day token trend for opencode.",
				},
			},
			{
				Type: analyticsagent.StreamEventToolStart, Agent: analyticsagent.AgentTrend, ParentCallID: "tool-1",
				Round: 1, CallID: "tool-2", Name: "get_daily_usage", Arguments: json.RawMessage(`{"source":"opencode"}`),
			},
			{
				Type: analyticsagent.StreamEventToolFinish, Agent: analyticsagent.AgentTrend, ParentCallID: "tool-1",
				Round: 1, CallID: "tool-2", Name: "get_daily_usage", OK: &okValue,
				Result: json.RawMessage(`{"ok":true,"data":{}}`), DurationMS: 11,
			},
			{
				Type: analyticsagent.StreamEventSubagentFinish, Agent: analyticsagent.AgentLead, CallID: "tool-1",
				OK: &okValue, DurationMS: 320,
				Subagent: &analyticsagent.SubagentEvent{
					Agent: analyticsagent.AgentTrend, Title: "Trend analyst", Task: "Explain the 7-day token trend for opencode.",
					Status: analyticsagent.SubagentStatusComplete, Report: "Tokens rose 40%.", Rounds: 2,
					ToolsUsed: []string{"get_daily_usage"}, Usage: &usage,
				},
			},
			{Type: analyticsagent.StreamEventContentDelta, Delta: "Tokens rose."},
		},
		streamResult: analyticsagent.ChatResult{
			Message:   analyticsagent.BrowserMessage{Role: "assistant", Content: "Tokens rose.", Signature: "sig"},
			Model:     "MiniMax-M3",
			Provider:  analyticsagent.ProviderMiniMax,
			Agent:     analyticsagent.AgentLead,
			Rounds:    2,
			Usage:     analyticsagent.Usage{Requests: 4, InputTokens: 1200, OutputTokens: 200, TotalTokens: 1400},
			ToolsUsed: []string{"delegate_to_specialist", "get_daily_usage"},
			ToolCalls: []analyticsagent.ToolCallRecord{{
				CallID: "tool-2", Name: "get_daily_usage", Agent: analyticsagent.AgentTrend, ParentCallID: "tool-1", Round: 1,
				Arguments: json.RawMessage(`{"source":"opencode"}`), Result: json.RawMessage(`{"ok":true,"data":{}}`),
				OK: true, DurationMS: 11,
			}},
			Subagents: []analyticsagent.SubagentRunRecord{{
				CallID: "tool-1", Agent: analyticsagent.AgentTrend, Title: "Trend analyst",
				Task: "Explain the 7-day token trend for opencode.", Status: analyticsagent.SubagentStatusComplete,
				Report: "Tokens rose 40%.", Rounds: 2, ToolsUsed: []string{"get_daily_usage"},
				Usage: usage, DurationMS: 320,
			}},
			DurationMS: 900,
		},
	}
	server, store := assistantTestServerWithChatLog(t, service)

	request := newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat/stream", validAssistantBody("Why did tokens grow?"))
	recorder := &flushCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var sessionID string
	var types []string
	var sawSubagentStart, sawSubagentReport, sawNestedTool bool
	scanner := bufio.NewScanner(bytes.NewReader(recorder.Body.Bytes()))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatalf("frame %q: %v", scanner.Text(), err)
		}
		frameType, _ := frame["type"].(string)
		types = append(types, frameType)
		switch frameType {
		case "round_start":
			if frame["round"] != float64(1) || frame["agent"] != string(analyticsagent.AgentLead) {
				t.Fatalf("round_start frame = %v", frame)
			}
		case "subagent_start":
			subagent, _ := frame["subagent"].(map[string]any)
			sawSubagentStart = subagent["agent"] == string(analyticsagent.AgentTrend) && subagent["task"] != ""
		case "tool_start":
			if frame["parent_call_id"] == "tool-1" && frame["agent"] == string(analyticsagent.AgentTrend) {
				sawNestedTool = true
			}
		case "subagent_finish":
			subagent, _ := frame["subagent"].(map[string]any)
			usageFrame, _ := subagent["usage"].(map[string]any)
			sawSubagentReport = subagent["report"] == "Tokens rose 40%." && usageFrame["total_tokens"] == float64(690)
		case "complete":
			sessionID, _ = frame["session_id"].(string)
			completeUsage, ok := frame["usage"].(map[string]any)
			if !ok || completeUsage["total_tokens"] != float64(1400) || completeUsage["requests"] != float64(4) {
				t.Fatalf("complete usage = %v", frame["usage"])
			}
			if runs, ok := frame["subagents"].([]any); !ok || len(runs) != 1 {
				t.Fatalf("complete subagents = %v", frame["subagents"])
			}
			if frame["session_title"] != "Why did tokens grow?" {
				t.Fatalf("complete session_title = %v", frame["session_title"])
			}
			if sessionUsage, ok := frame["session_usage"].(map[string]any); !ok || sessionUsage["total_tokens"] != float64(1400) {
				t.Fatalf("complete session_usage = %v", frame["session_usage"])
			}
		}
	}
	if !sawSubagentStart || !sawSubagentReport || !sawNestedTool {
		t.Fatalf("specialist frames incomplete (start=%v report=%v nested=%v): %s", sawSubagentStart, sawSubagentReport, sawNestedTool, recorder.Body.String())
	}
	if types[0] != "start" {
		t.Fatalf("frame order = %v", types)
	}

	detail, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	answer := detail.Messages[1]
	if answer.Usage == nil || answer.Usage.TotalTokens != 1400 || answer.Rounds != 2 {
		t.Fatalf("persisted turn accounting = %+v", answer)
	}
	if len(answer.Subagents) != 1 {
		t.Fatalf("persisted specialist runs = %+v", answer.Subagents)
	}
	run := answer.Subagents[0]
	if run.Agent != string(analyticsagent.AgentTrend) || run.Report != "Tokens rose 40%." || run.Usage.TotalTokens != 690 {
		t.Fatalf("persisted specialist run = %+v", run)
	}
	if len(answer.ToolCalls) != 1 || answer.ToolCalls[0].ParentCallRef != "tool-1" || answer.ToolCalls[0].Agent != string(analyticsagent.AgentTrend) {
		t.Fatalf("persisted nested tool call = %+v", answer.ToolCalls)
	}
	if answer.Context == nil || answer.Context.Route != "/models" || answer.Context.Period != "7d" {
		t.Fatalf("persisted request context = %+v", answer.Context)
	}
}

func TestAssistantChatStreamRejectsMalformedProgressEvents(t *testing.T) {
	tests := []struct {
		name  string
		event analyticsagent.StreamEvent
	}{
		{"round without a number", analyticsagent.StreamEvent{Type: analyticsagent.StreamEventRoundStart}},
		{"subagent start without a specialist", analyticsagent.StreamEvent{Type: analyticsagent.StreamEventSubagentStart, CallID: "tool-1"}},
		{
			"subagent finish without an outcome",
			analyticsagent.StreamEvent{
				Type: analyticsagent.StreamEventSubagentFinish, CallID: "tool-1",
				Subagent: &analyticsagent.SubagentEvent{Agent: analyticsagent.AgentTrend},
			},
		},
		{"tool finish without an outcome", analyticsagent.StreamEvent{Type: analyticsagent.StreamEventToolFinish, CallID: "tool-1", Name: "list_sources"}},
		{"unknown event", analyticsagent.StreamEvent{Type: "reasoning_delta", Delta: "private"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var emitErr error
			service := &fakeAssistantService{
				streamFunc: func(_ context.Context, _ analyticsagent.ChatInput, emit func(analyticsagent.StreamEvent) error) (analyticsagent.ChatResult, error) {
					emitErr = emit(test.event)
					return analyticsagent.ChatResult{
						Message: analyticsagent.BrowserMessage{Role: "assistant", Content: "Report.", Signature: "sig"},
						Model:   "MiniMax-M3",
					}, emitErr
				},
			}
			server := assistantTestServer(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
			recorder := &flushCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
			server.Handler.ServeHTTP(recorder, newAssistantRequest(http.MethodPost, "/api/v1/assistant/chat/stream", validAssistantBody("Report.")))
			if emitErr == nil {
				t.Fatalf("malformed event %#v was forwarded", test.event)
			}
			if strings.Contains(recorder.Body.String(), "private") {
				t.Fatalf("unknown event content reached the browser: %s", recorder.Body.String())
			}
		})
	}
}

func TestAssistantChatStreamRejectsUnknownSessionBeforeService(t *testing.T) {
	service := &fakeAssistantService{}
	server, _ := assistantTestServerWithChatLog(t, service)
	body, _ := json.Marshal(analyticsagent.ChatInput{
		ConsentVersion: analyticsagent.PrivacyConsentVersion,
		SessionID:      "cs_00000000000000000000000000000000",
		Messages:       []analyticsagent.BrowserMessage{{Role: "user", Content: "Hi."}},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/chat/stream", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if service.streamCalls != 0 {
		t.Fatalf("service was invoked %d times for a missing session", service.streamCalls)
	}
}

func TestAssistantSessionEndpointsListGetDelete(t *testing.T) {
	service := &fakeAssistantService{}
	server, store := assistantTestServerWithChatLog(t, service)
	receipt, err := store.AppendTurn(context.Background(), chatstore.Turn{
		UserContent: "How is usage?", AssistantContent: "Usage report.", AssistantSignature: "sig",
		Model: "MiniMax-M3", Provider: "minimax", Rounds: 2, DurationMS: 1500,
		Usage:     chatstore.Usage{Requests: 2, InputTokens: 400, OutputTokens: 80, TotalTokens: 480},
		ToolCalls: []chatstore.ToolCall{{Name: "list_sources", Arguments: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`), OK: true}},
		Subagents: []chatstore.SubagentRun{{
			Agent: "trend_analyst", Title: "Trend analyst", Task: "Explain the 7-day trend.",
			Status: "complete", Report: "Usage rose.", Rounds: 2,
			Usage: chatstore.Usage{Requests: 2, TotalTokens: 200},
		}},
	})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	sessionID := receipt.SessionID

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/sessions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d", recorder.Code)
	}
	if cache := recorder.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("list Cache-Control = %q", cache)
	}
	var listing struct {
		Sessions []chatstore.Session `json:"sessions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	if len(listing.Sessions) != 1 || listing.Sessions[0].ID != sessionID || listing.Sessions[0].Title != "How is usage?" {
		t.Fatalf("listing = %+v", listing)
	}
	// A listing must carry enough metadata for the browser to describe a saved
	// conversation without loading it.
	listed := listing.Sessions[0]
	if listed.Usage.TotalTokens != 480 || listed.TurnCount != 1 || listed.ToolCallCount != 1 || listed.SubagentCount != 1 {
		t.Fatalf("listed session metadata = %+v", listed)
	}

	recorder = httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/sessions/"+sessionID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d", recorder.Code)
	}
	var detail chatstore.SessionDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Messages) != 2 || len(detail.Messages[1].ToolCalls) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	// Restoring must return everything the live turn displayed.
	answer := detail.Messages[1]
	if answer.Usage == nil || answer.Usage.TotalTokens != 480 || answer.Rounds != 2 {
		t.Fatalf("restored turn accounting = %+v", answer)
	}
	if len(answer.Subagents) != 1 || answer.Subagents[0].Report != "Usage rose." {
		t.Fatalf("restored specialist runs = %+v", answer.Subagents)
	}

	recorder = httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/sessions/cs_00000000000000000000000000000000", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing get status = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/assistant/sessions/"+sessionID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/assistant/sessions/"+sessionID, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("repeat delete status = %d", recorder.Code)
	}
}

func TestAssistantSessionEndpointsWithoutStoreAreUnavailable(t *testing.T) {
	server := assistantTestServer(&fakeAssistantService{status: analyticsagent.Status{Available: true}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/assistant/sessions"},
		{http.MethodGet, "/api/v1/assistant/sessions/cs_00000000000000000000000000000000"},
		{http.MethodDelete, "/api/v1/assistant/sessions/cs_00000000000000000000000000000000"},
	} {
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s status = %d, want 503", endpoint.method, endpoint.path, recorder.Code)
		}
	}

	// Status reports persistence availability so the UI can hide history.
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/status", nil))
	if !strings.Contains(recorder.Body.String(), `"sessions_persisted":false`) {
		t.Fatalf("status body = %s", recorder.Body.String())
	}
}

func TestAssistantSessionsRejectNonlocalOrigin(t *testing.T) {
	service := &fakeAssistantService{}
	server, _ := assistantTestServerWithChatLog(t, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/assistant/sessions", nil)
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}
