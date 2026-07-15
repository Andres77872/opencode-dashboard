package analyticsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewMiniMaxClientDefaultsAndValidatesConfiguration(t *testing.T) {
	t.Parallel()

	client, err := NewMiniMaxClient(MiniMaxClientConfig{APIKey: " key "})
	if err != nil {
		t.Fatalf("NewMiniMaxClient() error = %v", err)
	}
	if client.baseURL != DefaultMiniMaxBaseURL {
		t.Fatalf("baseURL = %q, want %q", client.baseURL, DefaultMiniMaxBaseURL)
	}
	if client.apiKey != "key" {
		t.Fatalf("apiKey = %q, want trimmed key", client.apiKey)
	}
	if client.httpClient == nil || client.httpClient.Timeout != defaultClientTimeout {
		t.Fatalf("default HTTP client = %#v, want %s timeout", client.httpClient, defaultClientTimeout)
	}

	_, err = NewMiniMaxClient(MiniMaxClientConfig{})
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("missing key error = %v, want ErrAuthentication", err)
	}
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("missing key error type = %T, want *AuthenticationError", err)
	}

	_, err = NewMiniMaxClient(MiniMaxClientConfig{APIKey: "key", BaseURL: "http://example.com/v1"})
	if err == nil || !strings.Contains(err.Error(), "HTTPS is required") {
		t.Fatalf("insecure non-loopback base error = %v, want HTTPS validation error", err)
	}
}

func TestMiniMaxClientEnsureAvailableRequiresExactModelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		models  []string
		wantErr bool
	}{
		{name: "exact ID", models: []string{"MiniMax-M2.7", MiniMaxM3Model}},
		{name: "wrong case is unavailable", models: []string{"MiniMax-M3 " /* trailing space */, "minimax-m3", "MiniMax-M2.7"}, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
					t.Errorf("request = %s %s, want GET /v1/models", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer secret" {
					t.Errorf("Authorization = %q, want Bearer secret", got)
				}
				if got := r.Header.Get("Accept"); got != "application/json" {
					t.Errorf("Accept = %q, want application/json", got)
				}
				models := make([]map[string]string, 0, len(test.models))
				for _, id := range test.models {
					models = append(models, map[string]string{"id": id, "object": "model"})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
			}))
			t.Cleanup(server.Close)

			client := newTestMiniMaxClient(t, server)
			err := client.EnsureAvailable(context.Background())
			if !test.wantErr {
				if err != nil {
					t.Fatalf("EnsureAvailable() error = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrModelUnavailable) {
				t.Fatalf("EnsureAvailable() error = %v, want ErrModelUnavailable", err)
			}
			var unavailable *ModelUnavailableError
			if !errors.As(err, &unavailable) {
				t.Fatalf("EnsureAvailable() error type = %T, want *ModelUnavailableError", err)
			}
			if unavailable.Model != MiniMaxM3Model {
				t.Fatalf("unavailable model = %q, want %q", unavailable.Model, MiniMaxM3Model)
			}
		})
	}
}

func TestMiniMaxClientChatSendsFixedM3ContractAndPreservesAssistantMessage(t *testing.T) {
	t.Parallel()

	const assistantMessage = `{
          "role": "assistant",
          "content": "I need the usage data.",
          "reasoning_details": [{"type":"reasoning.text","id":"r1","text":"private reasoning","signature":"keep-me"}],
          "tool_calls": [{
            "id": "call_usage_1",
            "type": "function",
            "function": {"name":"query_usage","arguments":"{\"source\":\"codex\"}"},
            "index": 0
          }],
          "future_provider_field": {"must":"survive"}
        }`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request = %s %s, want POST /v1/chat/completions", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}

		var body struct {
			Model               string            `json:"model"`
			Messages            []json.RawMessage `json:"messages"`
			MaxCompletionTokens int               `json:"max_completion_tokens"`
			Thinking            struct {
				Type string `json:"type"`
			} `json:"thinking"`
			ReasoningSplit bool  `json:"reasoning_split"`
			Stream         *bool `json:"stream"`
			Tools          []struct {
				Type     string `json:"type"`
				Function struct {
					Name        string          `json:"name"`
					Description string          `json:"description"`
					Parameters  json.RawMessage `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != MiniMaxM3Model {
			t.Errorf("model = %q, want %q", body.Model, MiniMaxM3Model)
		}
		if body.MaxCompletionTokens != maxCompletionTokens {
			t.Errorf("max_completion_tokens = %d, want %d", body.MaxCompletionTokens, maxCompletionTokens)
		}
		if body.Thinking.Type != "adaptive" {
			t.Errorf("thinking.type = %q, want adaptive", body.Thinking.Type)
		}
		if !body.ReasoningSplit {
			t.Error("reasoning_split = false, want true")
		}
		if body.Stream == nil || *body.Stream {
			t.Errorf("stream = %v, want explicit false", body.Stream)
		}
		if len(body.Messages) != 1 || !bytes.Contains(body.Messages[0], []byte(`"role":"user"`)) {
			t.Errorf("messages = %s, want one user message", body.Messages)
		}
		if len(body.Tools) != 1 {
			t.Fatalf("tools length = %d, want 1", len(body.Tools))
		}
		tool := body.Tools[0]
		if tool.Type != "function" || tool.Function.Name != "query_usage" || tool.Function.Description != "Query usage" {
			t.Errorf("tool = %#v, want OpenAI query_usage function", tool)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.Function.Parameters, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("tool parameters = %s, want object JSON Schema (err %v)", tool.Function.Parameters, err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":%s}],"base_resp":{"status_code":0,"status_msg":""}}`, assistantMessage)
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	response, err := client.Chat(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"Compare usage"}`)},
		Tools: []ToolDefinition{{
			Name:        "query_usage",
			Description: "Query usage",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"}},"required":["source"]}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if response.FinishReason != "tool_calls" || response.Content != "I need the usage data." {
		t.Fatalf("Chat() response = %#v", response)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls length = %d, want 1", len(response.ToolCalls))
	}
	call := response.ToolCalls[0]
	if call.ID != "call_usage_1" || call.Type != "function" || call.Function.Name != "query_usage" || call.Function.Arguments != `{"source":"codex"}` {
		t.Fatalf("tool call = %#v, want parsed query_usage call", call)
	}
	if !bytes.Equal(response.AssistantMessage, []byte(assistantMessage)) {
		t.Fatalf("raw assistant message was changed\ngot:  %s\nwant: %s", response.AssistantMessage, assistantMessage)
	}
	for _, preserved := range []string{`"reasoning_details"`, `"signature":"keep-me"`, `"future_provider_field"`} {
		if !bytes.Contains(response.AssistantMessage, []byte(preserved)) {
			t.Errorf("raw assistant message lost %s: %s", preserved, response.AssistantMessage)
		}
	}
}

func TestMiniMaxClientChatReplaysRawAssistantFields(t *testing.T) {
	t.Parallel()

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 2 {
			if len(body.Messages) != 3 {
				t.Fatalf("second request messages = %d, want 3", len(body.Messages))
			}
			assistant := body.Messages[1]
			if !bytes.Contains(assistant, []byte(`"reasoning_details"`)) || !bytes.Contains(assistant, []byte(`"provider_extension":"preserved"`)) {
				t.Fatalf("replayed assistant message lost provider fields: %s", assistant)
			}
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Done"}}],"base_resp":{"status_code":0}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","reasoning_details":[{"text":"reason"}],"tool_calls":[{"id":"c1","type":"function","function":{"name":"query_usage","arguments":"{}"}}],"provider_extension":"preserved"}}],"base_resp":{"status_code":0}}`)
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	user := json.RawMessage(`{"role":"user","content":"Report"}`)
	first, err := client.Chat(context.Background(), ChatRequest{Messages: []json.RawMessage{user}})
	if err != nil {
		t.Fatalf("first Chat() error = %v", err)
	}
	tool := json.RawMessage(`{"role":"tool","tool_call_id":"c1","content":"{\"ok\":true}"}`)
	second, err := client.Chat(context.Background(), ChatRequest{Messages: []json.RawMessage{user, first.AssistantMessage, tool}})
	if err != nil {
		t.Fatalf("second Chat() error = %v", err)
	}
	if second.Content != "Done" || second.FinishReason != "stop" {
		t.Fatalf("second Chat() = %#v, want final Done response", second)
	}
}

func TestMiniMaxClientClassifiesHTTPAndProviderFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		want       error
		checkType  func(error) bool
	}{
		{
			name:   "unauthorized",
			status: http.StatusUnauthorized,
			body:   `{"error":{"message":"bad API key"}}`,
			want:   ErrAuthentication,
			checkType: func(err error) bool {
				var target *AuthenticationError
				return errors.As(err, &target) && target.StatusCode == http.StatusUnauthorized
			},
		},
		{
			name:       "rate limited",
			status:     http.StatusTooManyRequests,
			body:       `{"message":"slow down"}`,
			retryAfter: "3",
			want:       ErrRateLimited,
			checkType: func(err error) bool {
				var target *RateLimitError
				return errors.As(err, &target) && target.StatusCode == http.StatusTooManyRequests && target.RetryAfter == "3"
			},
		},
		{
			name:   "provider failure",
			status: http.StatusBadGateway,
			body:   `{"base_resp":{"status_msg":"upstream unavailable"}}`,
			want:   ErrProvider,
			checkType: func(err error) bool {
				var target *ProviderError
				return errors.As(err, &target) && target.StatusCode == http.StatusBadGateway
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)

			err := newTestMiniMaxClient(t, server).EnsureAvailable(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("EnsureAvailable() error = %v, want errors.Is(%v)", err, test.want)
			}
			if !test.checkType(err) {
				t.Fatalf("EnsureAvailable() error = %#v, typed details not preserved", err)
			}
			if !strings.Contains(err.Error(), strings.Trim(test.body, `{}`)) && !strings.Contains(err.Error(), "bad API key") && !strings.Contains(err.Error(), "slow down") && !strings.Contains(err.Error(), "upstream unavailable") {
				t.Fatalf("error is not helpful: %v", err)
			}
		})
	}
}

func TestMiniMaxClientClassifiesBaseResponseFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
		want error
	}{
		{name: "authentication", code: 1004, want: ErrAuthentication},
		{name: "rate limit", code: 1002, want: ErrRateLimited},
		{name: "other provider code", code: 1008, want: ErrProvider},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"data":[],"base_resp":{"status_code":%d,"status_msg":"provider detail"}}`, test.code)
			}))
			t.Cleanup(server.Close)
			err := newTestMiniMaxClient(t, server).EnsureAvailable(context.Background())
			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), fmt.Sprintf("provider code %d", test.code)) {
				t.Fatalf("EnsureAvailable() error = %v, want %v with provider code", err, test.want)
			}
		})
	}
}

func TestMiniMaxClientBoundsResponseAndErrorBodies(t *testing.T) {
	t.Parallel()

	t.Run("successful response", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", int(maxResponseBodyBytes)+1)), maxResponseBodyBytes+1)
		}))
		t.Cleanup(server.Close)
		err := newTestMiniMaxClient(t, server).EnsureAvailable(context.Background())
		if !errors.Is(err, ErrProvider) || !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("EnsureAvailable() error = %v, want bounded ErrProvider", err)
		}
	})

	t.Run("error response", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, strings.Repeat("e", int(maxErrorBodyBytes)+1024))
		}))
		t.Cleanup(server.Close)
		err := newTestMiniMaxClient(t, server).EnsureAvailable(context.Background())
		if !errors.Is(err, ErrProvider) || !strings.Contains(err.Error(), "response truncated") {
			t.Fatalf("EnsureAvailable() error = %v, want truncated ErrProvider", err)
		}
		if len(err.Error()) > int(maxErrorBodyBytes)+512 {
			t.Fatalf("bounded error length = %d, unexpectedly large", len(err.Error()))
		}
	})
}

func TestMiniMaxClientRejectsMalformedChatProtocol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "wrong response role", body: `{"choices":[{"finish_reason":"stop","message":{"role":"user","content":"x"}}]}`},
		{name: "unknown finish reason", body: `{"choices":[{"finish_reason":"mystery","message":{"role":"assistant","content":"x"}}]}`},
		{name: "tool reason without calls", body: `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":""}}]}`},
		{name: "wrong tool type", body: `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"custom","function":{"name":"query","arguments":"{}"}}]}}]}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			client := newTestMiniMaxClient(t, server)
			_, err := client.Chat(context.Background(), ChatRequest{Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)}})
			if !errors.Is(err, ErrProvider) {
				t.Fatalf("Chat() error = %v, want ErrProvider", err)
			}
		})
	}
}

func newTestMiniMaxClient(t *testing.T, server *httptest.Server) *MiniMaxClient {
	t.Helper()
	client, err := NewMiniMaxClient(MiniMaxClientConfig{
		APIKey:     "secret",
		BaseURL:    server.URL + "/v1/",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewMiniMaxClient() error = %v", err)
	}
	return client
}
