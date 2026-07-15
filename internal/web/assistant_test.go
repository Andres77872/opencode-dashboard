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
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/analyticsagent"
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
