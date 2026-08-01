package analyticsagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"opencode-dashboard/internal/version"
)

const (
	DefaultKimiBaseURL       = "https://api.kimi.com/coding/v1"
	defaultGenericTimeout    = 130 * time.Second
	maxModelCatalogBodyBytes = 2 << 20
	maxModelCatalogEntries   = 2000
)

// DiscoveredModel is the bounded, provider-neutral subset of GET /models.
// ContextLimit is zero when the server does not publish one, in which case the
// user must supply it before selecting the model.
type DiscoveredModel struct {
	ID           string `json:"id"`
	ContextLimit int    `json:"context_limit,omitempty"`
}

type OpenAIClientConfig struct {
	APIKey               string
	BaseURL              string
	Model                string
	MaxCompletionTokens  int
	InsecureTransportAck bool
	HTTPClient           *http.Client
}

// OpenAIClient is the deliberately small Chat Completions profile used for
// Kimi and custom providers. It sends no MiniMax thinking/reasoning fields.
type OpenAIClient struct {
	apiKey              string
	baseURL             string
	model               string
	maxCompletionTokens int
	httpClient          *http.Client
}

func NewOpenAIClient(config OpenAIClientConfig) (*OpenAIClient, error) {
	baseURL, _, err := NormalizeProviderBaseURL(config.BaseURL, config.InsecureTransportAck)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(config.Model)
	if model == "" || len(model) > 512 || strings.ContainsAny(model, "\r\n\x00") {
		return nil, errors.New("invalid assistant model id")
	}
	maxOutput := config.MaxCompletionTokens
	if maxOutput <= 0 {
		maxOutput = maxCompletionTokens
	}
	client := credentialSafeHTTPClient(config.HTTPClient)
	return &OpenAIClient{
		apiKey: strings.TrimSpace(config.APIKey), baseURL: baseURL, model: model,
		maxCompletionTokens: maxOutput, httpClient: client,
	}, nil
}

// EnsureAvailable is intentionally side-effect free. Discovery/readiness is
// owned by the revision-keyed provider registry and never repeated per round.
func (c *OpenAIClient) EnsureAvailable(context.Context) error { return nil }

func (c *OpenAIClient) Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	payload, err := c.payload(request, false)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, &ProviderError{Operation: "build chat completion request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "opencode-dashboard/"+version.Version)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &ProviderError{Operation: "chat completion", Cause: errors.New("provider request failed")}
	}
	defer response.Body.Close()
	body, readErr := readBounded(response.Body, maxResponseBodyBytes)
	if readErr != nil {
		return nil, &ProviderError{Operation: "read chat completion response", Cause: readErr}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, classifyOpenAIHTTPError("chat completion", response.StatusCode, response.Header.Get("Retry-After"), body)
	}
	var envelope struct {
		Choices []struct {
			FinishReason string          `json:"finish_reason"`
			Message      json.RawMessage `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, &ProviderError{Operation: "decode chat completion", Cause: err}
	}
	if len(envelope.Choices) != 1 {
		return nil, &ProviderError{Operation: "decode chat completion", Message: "response must contain exactly one choice"}
	}
	choice := envelope.Choices[0]
	var message struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []ToolCall `json:"tool_calls"`
	}
	if len(choice.Message) == 0 || !json.Valid(choice.Message) || json.Unmarshal(choice.Message, &message) != nil || message.Role != "assistant" {
		return nil, &ProviderError{Operation: "decode chat completion", Message: "response contained an invalid assistant message"}
	}
	switch choice.FinishReason {
	case "stop", "length", "content_filter":
		if len(message.ToolCalls) != 0 {
			return nil, &ProviderError{Operation: "validate chat completion", Message: "terminal choice included tool calls"}
		}
	case "tool_calls":
		if len(message.ToolCalls) == 0 {
			return nil, &ProviderError{Operation: "validate chat completion", Message: "tool_calls choice contained no tool calls"}
		}
	default:
		return nil, &ProviderError{Operation: "validate chat completion", Message: "unsupported finish reason"}
	}
	for _, call := range message.ToolCalls {
		if call.Type != "function" || strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
			return nil, &ProviderError{Operation: "validate chat completion", Message: "invalid function tool call"}
		}
	}
	return &ChatResponse{FinishReason: choice.FinishReason, Content: message.Content, ToolCalls: message.ToolCalls,
		AssistantMessage: bytes.Clone(choice.Message), Usage: requestUsage(parseUsage(envelope.Usage))}, nil
}

func (c *OpenAIClient) payload(request ChatRequest, stream bool) ([]byte, error) {
	if len(request.Messages) == 0 {
		return nil, errors.New("assistant chat requires at least one message")
	}
	for i, message := range request.Messages {
		if !json.Valid(message) {
			return nil, fmt.Errorf("assistant chat message %d is invalid", i)
		}
	}
	type genericFunction struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	type genericTool struct {
		Type     string          `json:"type"`
		Function genericFunction `json:"function"`
	}
	tools := make([]genericTool, 0, len(request.Tools))
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || !json.Valid(tool.Parameters) {
			return nil, errors.New("assistant tool definition is invalid")
		}
		var schema map[string]any
		if json.Unmarshal(tool.Parameters, &schema) != nil || schema["type"] != "object" {
			return nil, errors.New("assistant tool parameters must be an object schema")
		}
		tools = append(tools, genericTool{Type: "function", Function: genericFunction{Name: tool.Name, Description: tool.Description, Parameters: bytes.Clone(tool.Parameters)}})
	}
	payload := struct {
		Model               string            `json:"model"`
		Messages            []json.RawMessage `json:"messages"`
		Tools               []genericTool     `json:"tools,omitempty"`
		MaxCompletionTokens int               `json:"max_tokens"`
		Stream              bool              `json:"stream"`
	}{Model: c.model, Messages: request.Messages, Tools: tools, MaxCompletionTokens: c.maxCompletionTokens, Stream: stream}
	return json.Marshal(payload)
}

// ChatStream uses the ordinary OpenAI SSE delta shape while retaining the same
// strict response/body/tool validation as the MiniMax adapter. The request
// remains generic: only model, messages, tools, max_tokens, and stream are sent.
func (c *OpenAIClient) ChatStream(ctx context.Context, request ChatRequest, onContent func(string) error) (*ChatResponse, error) {
	payload, err := c.payload(request, true)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, &ProviderError{Operation: "build stream chat completion request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "opencode-dashboard/"+version.Version)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &ProviderError{Operation: "stream chat completion", Cause: errors.New("provider request failed")}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := readBounded(response.Body, maxErrorBodyBytes)
		return nil, classifyOpenAIHTTPError("stream chat completion", response.StatusCode, response.Header.Get("Retry-After"), body)
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		return nil, &ProviderError{Operation: "validate stream chat completion", Message: "response is not an event stream"}
	}
	limited := &io.LimitedReader{R: response.Body, N: maxResponseBodyBytes + 1}
	reader := bufio.NewReader(limited)
	accumulator := &streamAssistantAccumulator{knownToolNames: make(map[string]struct{}, len(request.Tools))}
	for _, tool := range request.Tools {
		accumulator.knownToolNames[tool.Name] = struct{}{}
	}
	for {
		data, readErr := readServerSentData(reader)
		if limited.N == 0 {
			return nil, &ProviderError{Operation: "read stream chat completion response", Message: "response body is too large"}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, &ProviderError{Operation: "read stream chat completion response", Cause: readErr}
		}
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.Equal(trimmed, []byte("[DONE]")) {
			break
		}
		var chunk struct {
			Choices []streamChatChoice `json:"choices"`
			Usage   json.RawMessage    `json:"usage"`
		}
		if err := json.Unmarshal(trimmed, &chunk); err != nil {
			return nil, &ProviderError{Operation: "decode stream chat completion", Cause: err}
		}
		if usage := parseUsage(chunk.Usage); usage.HasTokens() {
			accumulator.usage = usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				return nil, &ProviderError{Operation: "validate stream chat completion", Message: "unsupported choice index"}
			}
			if len(choice.Delta) != 0 && !bytes.Equal(bytes.TrimSpace(choice.Delta), []byte("null")) {
				if err := accumulator.applyDelta(ctx, choice.Delta, onContent); err != nil {
					return nil, err
				}
			}
			finish, present, err := decodeOptionalString(choice.FinishReason, "finish_reason")
			if err != nil {
				return nil, &ProviderError{Operation: "decode stream chat completion", Cause: err}
			}
			if present {
				if accumulator.providerFinishReasonSet && accumulator.providerFinishReason != finish {
					return nil, &ProviderError{Operation: "validate stream chat completion", Message: "conflicting finish reasons"}
				}
				accumulator.providerFinishReason, accumulator.providerFinishReasonSet = finish, true
			}
		}
	}
	if !accumulator.providerFinishReasonSet {
		return nil, &ProviderError{Operation: "validate stream chat completion", Message: "stream contained no finish reason"}
	}
	return accumulator.response()
}

// DiscoverOpenAIModels performs a credential-safe, bounded GET /models.
func DiscoverOpenAIModels(ctx context.Context, baseURL, apiKey string, insecureAck bool, client *http.Client) ([]DiscoveredModel, error) {
	normalized, _, err := NormalizeProviderBaseURL(baseURL, insecureAck)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized+"/models", nil)
	if err != nil {
		return nil, &ProviderError{Operation: "build model discovery request", Cause: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "opencode-dashboard/"+version.Version)
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := credentialSafeHTTPClient(client).Do(req)
	if err != nil {
		return nil, &ProviderError{Operation: "model discovery", Cause: errors.New("provider request failed")}
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, maxModelCatalogBodyBytes)
	if err != nil {
		return nil, &ProviderError{Operation: "read model catalog", Cause: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, classifyOpenAIHTTPError("model discovery", response.StatusCode, response.Header.Get("Retry-After"), body)
	}
	var envelope struct {
		Data []struct {
			ID            string `json:"id"`
			ContextWindow int    `json:"context_window"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, &ProviderError{Operation: "decode model catalog", Cause: err}
	}
	if len(envelope.Data) > maxModelCatalogEntries {
		return nil, &ProviderError{Operation: "decode model catalog", Message: "catalog contains too many models"}
	}
	models := make([]DiscoveredModel, 0, len(envelope.Data))
	seen := make(map[string]struct{}, len(envelope.Data))
	for _, raw := range envelope.Data {
		id := strings.TrimSpace(raw.ID)
		if id == "" || len(id) > 512 || strings.ContainsAny(id, "\r\n\x00") {
			return nil, &ProviderError{Operation: "decode model catalog", Message: "catalog contains an invalid model id"}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		limit := raw.ContextWindow
		if limit == 0 {
			limit = raw.ContextLength
		}
		if limit < 0 || limit > 16_000_000 {
			limit = 0
		}
		models = append(models, DiscoveredModel{ID: id, ContextLimit: limit})
	}
	return models, nil
}

// NormalizeProviderBaseURL validates a destination without resolving or
// following it. HTTP is limited to loopback/private IP literals and requires
// an explicit persisted acknowledgement.
func NormalizeProviderBaseURL(raw string, insecureAck bool) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", "", errors.New("base_url must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errors.New("base_url cannot contain credentials, a query, or a fragment")
	}
	if parsed.Path == "" {
		parsed.Path = "/v1"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	host := parsed.Hostname()
	if parsed.Scheme == "http" {
		ip := net.ParseIP(host)
		allowed := strings.EqualFold(host, "localhost") || (ip != nil && (ip.IsLoopback() || ip.IsPrivate()))
		if !allowed {
			return "", "", errors.New("HTTP is allowed only for loopback or private-LAN IP endpoints")
		}
		if !insecureAck {
			return "", "", errors.New("HTTP endpoint requires insecure_transport_ack")
		}
	}
	return strings.TrimRight(parsed.String(), "/"), parsed.Scheme + "://" + parsed.Host, nil
}

func credentialSafeHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: defaultGenericTimeout}
	}
	clone := *client
	if clone.Timeout == 0 {
		clone.Timeout = defaultGenericTimeout
	}
	clone.Jar = nil
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects are disabled") }
	return &clone
}

func classifyOpenAIHTTPError(operation string, status int, retryAfter string, body []byte) error {
	message := extractProviderMessage(body)
	if message == "" {
		message = http.StatusText(status)
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &AuthenticationError{Operation: operation, StatusCode: status, Message: message}
	case http.StatusTooManyRequests:
		return &RateLimitError{Operation: operation, StatusCode: status, RetryAfter: retryAfter, Message: message}
	default:
		return &ProviderError{Operation: operation, StatusCode: status, Message: message}
	}
}
