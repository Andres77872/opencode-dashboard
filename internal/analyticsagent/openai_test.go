package analyticsagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenericOpenAIProfileUsesMinimalCompatiblePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"thinking", "reasoning_split", "stream_options", "max_completion_tokens"} {
			if _, ok := payload[forbidden]; ok {
				t.Errorf("generic payload contains %q: %#v", forbidden, payload)
			}
		}
		if payload["model"] != "custom-model" || payload["max_tokens"] != float64(2048) || payload["stream"] != false {
			t.Errorf("payload=%#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer server.Close()
	client, err := NewOpenAIClient(OpenAIClientConfig{BaseURL: server.URL + "/v1", Model: "custom-model", MaxCompletionTokens: 2048, InsecureTransportAck: true})
	if err != nil {
		t.Fatal(err)
	}
	message, _ := makeTextMessage("user", "hello")
	response, err := client.Chat(context.Background(), ChatRequest{Messages: []json.RawMessage{message}})
	if err != nil || response.Content != "ok" || response.Usage.TotalTokens != 3 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestProviderBaseURLRequiresAcknowledgementForPrivateHTTP(t *testing.T) {
	if _, _, err := NormalizeProviderBaseURL("http://127.0.0.1:8080/v1", false); err == nil {
		t.Fatal("loopback HTTP accepted without acknowledgement")
	}
	if got, destination, err := NormalizeProviderBaseURL("http://192.168.1.9:8080/v1/", true); err != nil || got != "http://192.168.1.9:8080/v1" || destination != "http://192.168.1.9:8080" {
		t.Fatalf("got=%q destination=%q err=%v", got, destination, err)
	}
	for _, invalid := range []string{"http://example.com/v1", "https://user:pass@example.com/v1", "https://example.com/v1?q=secret", "https://example.com/v1#fragment"} {
		if _, _, err := NormalizeProviderBaseURL(invalid, true); err == nil {
			t.Errorf("accepted %q", invalid)
		}
	}
}
