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
	"os"
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

func TestRegistryDefinitionsSurviveMiniMaxWireEncoding(t *testing.T) {
	definitions := NewToolRegistry(nil).Definitions()
	payload, err := makeWireChatPayload(ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"report"}`)},
		Tools:    definitions,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var wire wireChatRequest
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Tools) != len(definitions) {
		t.Fatalf("wire tools = %d, want %d", len(wire.Tools), len(definitions))
	}
	for index, definition := range definitions {
		got := wire.Tools[index].Function
		if got.Name != definition.Name || got.Description != definition.Description {
			t.Errorf("wire tool %d metadata = %#v, want %#v", index, got, definition)
		}
		if !bytes.Equal(got.Parameters, definition.Parameters) {
			t.Errorf("wire tool %q parameters changed\ngot:  %s\nwant: %s", definition.Name, got.Parameters, definition.Parameters)
		}
		var schema map[string]any
		if json.Unmarshal(got.Parameters, &schema) != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("wire tool %q lost closed object schema: %s", definition.Name, got.Parameters)
		}
		if definition.Name != "list_sources" {
			modes, ok := schema["oneOf"].([]any)
			if !ok || len(modes) != 3 {
				t.Errorf("wire tool %q lost PRESET/CUSTOM/DEFAULT exclusivity: %s", definition.Name, got.Parameters)
			}
			properties, _ := schema["properties"].(map[string]any)
			period, _ := properties["period"].(map[string]any)
			if value, exists := period["default"]; exists {
				t.Errorf("wire tool %q advertises misleading period default %#v", definition.Name, value)
			}
		}
	}
}

func TestMiniMaxWireRejectsNonObjectToolSchema(t *testing.T) {
	_, err := makeWireChatPayload(ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"report"}`)},
		Tools: []ToolDefinition{{
			Name: "bad_schema", Parameters: json.RawMessage(`[]`),
		}},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "JSON Schema object") {
		t.Fatalf("error = %v, want object-schema rejection", err)
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

func TestMiniMaxClientChatStreamNormalizesCumulativeContentAndKeepsReasoningPrivate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}
		var body struct {
			Stream         bool `json:"stream"`
			ReasoningSplit bool `json:"reasoning_split"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !body.Stream || !body.ReasoningSplit {
			t.Errorf("stream/reasoning_split = %v/%v, want true/true", body.Stream, body.ReasoningSplit)
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, ": provider heartbeat\r\n\r\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\",\"reasoning_content\":\"private\",\"reasoning_details\":[{\"index\":0,\"type\":\"reasoning.text\",\"text\":\"pri\",\"signature\":\"keep-me\"}],\"future_provider_field\":{\"preserved\":true}},\"finish_reason\":null}],\"base_resp\":{\"status_code\":0}}\r\n\r\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\",\"reasoning_content\":\"private reasoning\",\"reasoning_details\":[{\"index\":0,\"type\":\"reasoning.text\",\"text\":\"private reasoning\"}]},\"finish_reason\":null}]}\r\n\r\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\r\n\r\n")
		_, _ = io.WriteString(w, "data: [DONE]\r\n\r\n")
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	var deltas []string
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
	}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if got := strings.Join(deltas, ""); got != "Hello" {
		t.Fatalf("streamed deltas = %#v (joined %q), want append-only Hello", deltas, got)
	}
	if len(deltas) != 2 || deltas[0] != "Hel" || deltas[1] != "lo" {
		t.Errorf("streamed deltas = %#v, want [Hel lo]", deltas)
	}
	for _, delta := range deltas {
		if strings.Contains(delta, "private") || strings.Contains(delta, "reasoning") {
			t.Fatalf("reasoning leaked through callback: %q", delta)
		}
	}
	if response.FinishReason != "stop" || response.Content != "Hello" || len(response.ToolCalls) != 0 {
		t.Fatalf("ChatStream() response = %#v", response)
	}
	for _, preserved := range []string{`"reasoning_content":"private reasoning"`, `"reasoning_details"`, `"signature":"keep-me"`, `"future_provider_field"`} {
		if !bytes.Contains(response.AssistantMessage, []byte(preserved)) {
			t.Errorf("replayable assistant message lost %s: %s", preserved, response.AssistantMessage)
		}
	}
}

func TestMiniMaxClientChatStreamNormalizesIncrementalContentAtTerminalEOF(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\",\"reasoning_content\":\"private \",\"reasoning_details\":[{\"index\":0,\"type\":\"reasoning.text\",\"text\":\"pri\"}]},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\",\"reasoning_content\":\"reasoning\",\"reasoning_details\":[{\"index\":0,\"text\":\"vate reasoning\",\"signature\":\"keep-in-replay\"}]},\"finish_reason\":\"stop\"}]}\n\n")
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	var deltas []string
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
	}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "Hel" || deltas[1] != "lo" || strings.Join(deltas, "") != "Hello" {
		t.Fatalf("streamed deltas = %#v, want append-only [Hel lo]", deltas)
	}
	if response.Content != "Hello" || response.FinishReason != "stop" {
		t.Fatalf("ChatStream() response = %#v", response)
	}
	for _, preserved := range []string{`"reasoning_content":"private reasoning"`, `"text":"private reasoning"`, `"signature":"keep-in-replay"`} {
		if !bytes.Contains(response.AssistantMessage, []byte(preserved)) {
			t.Errorf("replayable assistant message lost %s: %s", preserved, response.AssistantMessage)
		}
	}
	for _, delta := range deltas {
		if strings.Contains(delta, "private") || strings.Contains(delta, "reasoning") {
			t.Fatalf("reasoning leaked through callback: %q", delta)
		}
	}
}

func TestMiniMaxClientChatStreamReconstructsCapturedM3ToolTurnAtTerminalEOF(t *testing.T) {
	t.Parallel()

	fixture, err := os.ReadFile("testdata/minimax_m3_incremental_tool_stream.sse")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	var callbacks []string
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"list sources"}`)},
		Tools: []ToolDefinition{{
			Name: "list_sources", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		}},
	}, func(delta string) error {
		callbacks = append(callbacks, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(callbacks) != 0 {
		t.Fatalf("private/tool-only stream invoked visible callbacks: %#v", callbacks)
	}
	if response.FinishReason != "tool_calls" || response.Content != "" || len(response.ToolCalls) != 1 {
		t.Fatalf("ChatStream() response = %#v", response)
	}
	call := response.ToolCalls[0]
	if call.ID != "provider-call-sanitized" || call.Type != "function" || call.Function.Name != "list_sources" || call.Function.Arguments != `{}` {
		t.Fatalf("reconstructed tool call = %#v", call)
	}
	for _, preserved := range []string{
		`"reasoning_content":"I should inspect the available sources before answering."`,
		`"text":"I should inspect the available sources before answering."`,
		`"signature":"sanitized-signature"`,
		`"provider_extension":{"trace":"preserved"}`,
		`"provider_tool_field":{"opaque":"preserved"}`,
	} {
		if !bytes.Contains(response.AssistantMessage, []byte(preserved)) {
			t.Errorf("replayable assistant message lost %s: %s", preserved, response.AssistantMessage)
		}
	}
}

func TestStreamReasoningDetailsAccumulatesEachIndexIndependently(t *testing.T) {
	t.Parallel()

	var details streamReasoningDetails
	if err := details.merge(json.RawMessage(`[
		{"index":0,"type":"reasoning.text","text":"first "},
		{"index":1,"type":"reasoning.text","text":"second"}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := details.merge(json.RawMessage(`[
		{"index":0,"text":"chunk"},
		{"index":1,"text":"second cumulative"}
	]`)); err != nil {
		t.Fatal(err)
	}
	raw, err := details.raw()
	if err != nil {
		t.Fatal(err)
	}
	var replay []struct {
		Index int    `json:"index"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal(raw, &replay); err != nil {
		t.Fatal(err)
	}
	if len(replay) != 2 || replay[0].Index != 0 || replay[0].Text != "first chunk" ||
		replay[1].Index != 1 || replay[1].Text != "second cumulative" {
		t.Fatalf("reasoning details = %s, want independent incremental/cumulative accumulation", raw)
	}
}

func TestMiniMaxClientChatStreamDeliversContentBeforeProviderCompletes(t *testing.T) {
	release := make(chan struct{}, 1)
	providerFinished := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(providerFinished)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"First\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"First chunk\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		select {
		case release <- struct{}{}:
		default:
		}
	})

	client := newTestMiniMaxClient(t, server)
	firstDelta := make(chan string, 1)
	type streamResult struct {
		response *ChatResponse
		err      error
	}
	completed := make(chan streamResult, 1)
	go func() {
		response, err := client.ChatStream(context.Background(), ChatRequest{
			Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
		}, func(delta string) error {
			select {
			case firstDelta <- delta:
			default:
			}
			return nil
		})
		completed <- streamResult{response: response, err: err}
	}()

	select {
	case delta := <-firstDelta:
		if delta != "First" {
			t.Fatalf("first streamed delta = %q, want First", delta)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("content callback did not run before the provider completed")
	}
	select {
	case <-providerFinished:
		t.Fatal("provider completed before the first content callback was observed")
	default:
	}
	release <- struct{}{}

	select {
	case result := <-completed:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.response.Content != "First chunk" || result.response.FinishReason != "stop" {
			t.Fatalf("ChatStream() response = %#v", result.response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not complete after the provider was released")
	}
}

func TestMiniMaxClientChatStreamAccumulatesToolCallFragments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"get_\",\"arguments\":\"{\"}}]},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"get_overview\",\"arguments\":\"{\\\"source\\\":\"}}]},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"source\\\":\\\"codex\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	var callbackCalls int
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"compare"}`)},
	}, func(string) error {
		callbackCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if callbackCalls != 0 {
		t.Fatalf("content callback calls = %d, want none for tool-only turn", callbackCalls)
	}
	if response.FinishReason != "tool_calls" || len(response.ToolCalls) != 1 {
		t.Fatalf("ChatStream() response = %#v", response)
	}
	call := response.ToolCalls[0]
	if call.ID != "call-1" || call.Type != "function" || call.Function.Name != "get_overview" || call.Function.Arguments != `{"source":"codex"}` {
		t.Fatalf("accumulated tool call = %#v", call)
	}
	if !json.Valid(response.AssistantMessage) || !bytes.Contains(response.AssistantMessage, []byte(`"tool_calls"`)) {
		t.Fatalf("assistant replay message is invalid: %s", response.AssistantMessage)
	}
}

func TestMiniMaxClientChatStreamPreservesArbitraryIncrementalToolFragments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null,\"tool_calls\":[{\"index\":0,\"id\":\"call-incremental\",\"type\":\"function\",\"function\":{\"name\":\"get_\",\"arguments\":\"{\\\"q\"}}]},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"overview\",\"arguments\":\"uery\\\":\\\"\"}}]},\"finish_reason\":null}]}\n\n")
		// This fragment is a prefix of the accumulated value. A per-fragment
		// cumulative heuristic used to silently discard it.
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\"}}]},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"value\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	response, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"compare"}`)},
		Tools: []ToolDefinition{{
			Name: "get_overview", Parameters: json.RawMessage(`{"type":"object"}`),
		}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", response.ToolCalls)
	}
	call := response.ToolCalls[0]
	if call.Function.Name != "get_overview" || call.Function.Arguments != `{"query":"{value"}` {
		t.Fatalf("incremental fragments were altered: %#v", call.Function)
	}
}

func TestStreamToolFragmentResolutionFailsClosedWhenAmbiguous(t *testing.T) {
	t.Parallel()

	call := &streamToolCallAccumulator{id: "call-1", typeName: "function"}
	call.name.add("get_overview")
	call.arguments.add("1")
	call.arguments.add("12")
	_, _, err := call.resolve(map[string]struct{}{"get_overview": {}})
	if !errors.Is(err, ErrProvider) || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("resolve() error = %v, want fail-closed ambiguity", err)
	}
}

func TestMiniMaxClientChatStreamReplaysNullContentAndNestedProviderFields(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Messages []json.RawMessage `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request %d: %v", requests, err)
			return
		}
		if !body.Stream {
			t.Errorf("request %d stream = false", requests)
		}
		w.Header().Set("Content-Type", "text/event-stream")

		if requests == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null,\"assistant_extension\":{\"round\":1},\"tool_calls\":[{\"index\":0,\"id\":\"call-replay\",\"type\":\"function\",\"provider_tool_field\":{\"keep\":true},\"function\":{\"name\":\"get_\",\"arguments\":\"{\",\"provider_function_field\":{\"signature\":\"nested-keep\"}}}]},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"get_overview\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}

		if len(body.Messages) != 3 {
			t.Errorf("second request messages = %d, want user/assistant/tool", len(body.Messages))
		} else {
			var replay struct {
				Content        json.RawMessage `json:"content"`
				AssistantExtra json.RawMessage `json:"assistant_extension"`
				ToolCalls      []struct {
					Index    *int            `json:"index"`
					Provider json.RawMessage `json:"provider_tool_field"`
					Function struct {
						Name      string          `json:"name"`
						Arguments string          `json:"arguments"`
						Provider  json.RawMessage `json:"provider_function_field"`
					} `json:"function"`
				} `json:"tool_calls"`
			}
			if err := json.Unmarshal(body.Messages[1], &replay); err != nil {
				t.Errorf("decode replayed assistant message: %v", err)
			} else {
				if string(replay.Content) != "null" {
					t.Errorf("replayed content = %s, want null", replay.Content)
				}
				if len(replay.AssistantExtra) == 0 || len(replay.ToolCalls) != 1 {
					t.Errorf("replayed extensions/tool calls missing: %s", body.Messages[1])
				} else {
					tool := replay.ToolCalls[0]
					if tool.Index == nil || *tool.Index != 0 || len(tool.Provider) == 0 || len(tool.Function.Provider) == 0 || tool.Function.Name != "get_overview" || tool.Function.Arguments != `{}` {
						t.Errorf("nested provider fields were not replayed: %s", body.Messages[1])
					}
				}
			}
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Done\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := newTestMiniMaxClient(t, server)
	tools := []ToolDefinition{{Name: "get_overview", Parameters: json.RawMessage(`{"type":"object"}`)}}
	user := json.RawMessage(`{"role":"user","content":"compare"}`)
	first, err := client.ChatStream(context.Background(), ChatRequest{Messages: []json.RawMessage{user}, Tools: tools}, nil)
	if err != nil {
		t.Fatalf("first ChatStream() error = %v", err)
	}
	if !bytes.Contains(first.AssistantMessage, []byte(`"content":null`)) || len(first.ToolCalls) != 1 {
		t.Fatalf("first replay message = %s, calls=%#v", first.AssistantMessage, first.ToolCalls)
	}
	toolResult := json.RawMessage(`{"role":"tool","tool_call_id":"call-replay","name":"get_overview","content":"{}"}`)
	second, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{user, first.AssistantMessage, toolResult},
		Tools:    tools,
	}, nil)
	if err != nil {
		t.Fatalf("second ChatStream() error = %v", err)
	}
	if requests != 2 || second.Content != "Done" || second.FinishReason != "stop" {
		t.Fatalf("requests=%d second=%#v", requests, second)
	}
}

func TestMiniMaxClientChatStreamPropagatesCallbackError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	sentinel := errors.New("browser stream closed")
	client := newTestMiniMaxClient(t, server)
	_, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
	}, func(string) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("ChatStream() error = %v, want callback sentinel", err)
	}
}

func TestMiniMaxClientChatStreamRejectsOversizedOrIncompleteSSE(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "EOF before finish reason", body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"},
		{name: "malformed JSON", body: "data: {\"choices\":[\n\n"},
		{name: "conflicting finish reasons", body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n"},
		{name: "tool finish without calls", body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null},\"finish_reason\":\"tool_calls\"}]}\n\n"},
		{name: "invalid tool call", body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null,\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"custom\",\"function\":{\"name\":\"list_sources\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"},
		{name: "oversized", body: strings.Repeat("x", int(maxResponseBodyBytes)+1)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			client := newTestMiniMaxClient(t, server)
			_, err := client.ChatStream(context.Background(), ChatRequest{
				Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
			}, nil)
			if !errors.Is(err, ErrProvider) {
				t.Fatalf("ChatStream() error = %v, want ErrProvider", err)
			}
		})
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
				return errors.As(err, &target) && target.Operation == "list models" && target.StatusCode == http.StatusUnauthorized
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
				return errors.As(err, &target) && target.Operation == "list models" && target.StatusCode == http.StatusTooManyRequests && target.RetryAfter == "3"
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
		code int64
		want error
	}{
		{name: "legacy authentication", code: 1004, want: ErrAuthentication},
		{name: "invalid API key", code: 2049, want: ErrAuthentication},
		{name: "rate limit", code: 1002, want: ErrRateLimited},
		{name: "temporary usage exhaustion", code: 2056, want: ErrRateLimited},
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
			switch test.want {
			case ErrAuthentication:
				var typed *AuthenticationError
				if !errors.As(err, &typed) || typed.Operation != "list models" || typed.ProviderCode != test.code {
					t.Fatalf("authentication details = %#v, want operation/list-models code %d", typed, test.code)
				}
			case ErrRateLimited:
				var typed *RateLimitError
				if !errors.As(err, &typed) || typed.Operation != "list models" || typed.ProviderCode != test.code {
					t.Fatalf("rate-limit details = %#v, want operation/list-models code %d", typed, test.code)
				}
			case ErrProvider:
				var typed *ProviderError
				if !errors.As(err, &typed) || typed.Operation != "list models" || typed.ProviderCode != test.code {
					t.Fatalf("provider details = %#v, want operation/list-models code %d", typed, test.code)
				}
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
