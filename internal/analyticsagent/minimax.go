package analyticsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"opencode-dashboard/internal/version"
)

const (
	// DefaultMiniMaxBaseURL is MiniMax's international OpenAI-compatible API.
	DefaultMiniMaxBaseURL = "https://api.minimax.io/v1"
	// MiniMaxM3Model is deliberately exact and case-sensitive. MiniMax does not
	// document a lowercase alias or an M3 high-speed alias.
	MiniMaxM3Model = "MiniMax-M3"

	maxResponseBodyBytes int64 = 8 << 20
	maxErrorBodyBytes    int64 = 64 << 10
	maxCompletionTokens        = 8192
	defaultClientTimeout       = 130 * time.Second
)

var (
	ErrModelUnavailable = errors.New("MiniMax M3 model unavailable")
	ErrAuthentication   = errors.New("MiniMax authentication failed")
	ErrRateLimited      = errors.New("MiniMax rate limit exceeded")
	ErrProvider         = errors.New("MiniMax provider failure")
)

// ModelUnavailableError means the authenticated MiniMax model catalog did not
// contain the exact MiniMax-M3 model ID. The client never silently falls back.
type ModelUnavailableError struct {
	Model     string
	Available []string
}

func (e *ModelUnavailableError) Error() string {
	model := e.Model
	if model == "" {
		model = MiniMaxM3Model
	}
	if len(e.Available) == 0 {
		return fmt.Sprintf("%s: %q was not returned by GET /models", ErrModelUnavailable, model)
	}
	return fmt.Sprintf("%s: %q was not returned by GET /models (available: %s)", ErrModelUnavailable, model, strings.Join(e.Available, ", "))
}

func (e *ModelUnavailableError) Unwrap() error { return ErrModelUnavailable }

// AuthenticationError covers a missing local credential and credentials
// rejected by MiniMax over HTTP or through its base_resp envelope.
type AuthenticationError struct {
	StatusCode   int
	ProviderCode int64
	Message      string
}

func (e *AuthenticationError) Error() string {
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = "the API key was rejected"
	}
	return formatClassifiedError(ErrAuthentication, e.StatusCode, e.ProviderCode, detail)
}

func (e *AuthenticationError) Unwrap() error { return ErrAuthentication }

// RateLimitError is returned for HTTP 429 and MiniMax provider code 1002.
type RateLimitError struct {
	StatusCode   int
	ProviderCode int64
	RetryAfter   string
	Message      string
}

func (e *RateLimitError) Error() string {
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = "retry later"
	}
	if retry := strings.TrimSpace(e.RetryAfter); retry != "" {
		detail += "; Retry-After: " + retry
	}
	return formatClassifiedError(ErrRateLimited, e.StatusCode, e.ProviderCode, detail)
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// ProviderError represents transport, protocol, decoding, and non-auth/rate
// MiniMax failures. Cause remains discoverable through errors.Is/errors.As.
type ProviderError struct {
	Operation    string
	StatusCode   int
	ProviderCode int64
	Message      string
	Cause        error
}

func (e *ProviderError) Error() string {
	prefix := ErrProvider.Error()
	if operation := strings.TrimSpace(e.Operation); operation != "" {
		prefix += " during " + operation
	}
	detail := strings.TrimSpace(e.Message)
	if detail == "" && e.Cause != nil {
		detail = e.Cause.Error()
	}
	return formatClassifiedError(errors.New(prefix), e.StatusCode, e.ProviderCode, detail)
}

func (e *ProviderError) Unwrap() error {
	if e.Cause == nil {
		return ErrProvider
	}
	return errors.Join(ErrProvider, e.Cause)
}

func formatClassifiedError(class error, status int, providerCode int64, detail string) string {
	message := class.Error()
	if status != 0 {
		message += fmt.Sprintf(" (HTTP %d)", status)
	}
	if providerCode != 0 {
		message += fmt.Sprintf(" (provider code %d)", providerCode)
	}
	if detail != "" {
		message += ": " + detail
	}
	return message
}

// MiniMaxClientConfig contains already-resolved provider configuration. Env
// and credential-store discovery intentionally live outside this transport.
type MiniMaxClientConfig struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// MiniMaxClient makes direct OpenAI-compatible HTTP calls to MiniMax M3.
type MiniMaxClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewMiniMaxClient constructs a provider client without making a network
// request. Call EnsureAvailable before starting an agent loop.
func NewMiniMaxClient(config MiniMaxClientConfig) (*MiniMaxClient, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, &AuthenticationError{Message: "an API key is required"}
	}

	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = DefaultMiniMaxBaseURL
	}
	normalizedBaseURL, err := normalizeMiniMaxBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultClientTimeout}
	}

	return &MiniMaxClient{
		apiKey:     apiKey,
		baseURL:    normalizedBaseURL,
		httpClient: httpClient,
	}, nil
}

func normalizeMiniMaxBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid MiniMax base URL: %w", err)
	}
	if parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("invalid MiniMax base URL %q: expected an absolute HTTP(S) URL", raw)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid MiniMax base URL %q: credentials, query strings, and fragments are not allowed", raw)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return "", fmt.Errorf("invalid MiniMax base URL %q: HTTPS is required for non-loopback hosts", raw)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// EnsureAvailable checks the authenticated model catalog and succeeds only
// when it contains the exact, case-sensitive MiniMax-M3 identifier.
func (c *MiniMaxClient) EnsureAvailable(ctx context.Context) error {
	body, err := c.request(ctx, http.MethodGet, "/models", nil, "list models")
	if err != nil {
		return err
	}

	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		BaseResp baseResponse `json:"base_resp"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return &ProviderError{Operation: "decode model catalog", Cause: err}
	}
	if err := classifyBaseResponse("list models", response.BaseResp); err != nil {
		return err
	}

	available := make([]string, 0, len(response.Data))
	for _, model := range response.Data {
		available = append(available, model.ID)
		if model.ID == MiniMaxM3Model {
			return nil
		}
	}
	return &ModelUnavailableError{Model: MiniMaxM3Model, Available: available}
}

// ToolDefinition is converted to the OpenAI function-tool wire shape. Its
// Parameters value must contain a JSON Schema object.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// FunctionCall is a model-requested function invocation. Arguments is a JSON
// string and must be decoded and validated by the agent loop before execution.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall is an OpenAI-compatible function call returned by MiniMax.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// ChatRequest supplies complete conversation history and available read-only
// analytics tools. Messages are raw JSON objects so a previous assistant
// message can be replayed without discarding MiniMax reasoning fields.
type ChatRequest struct {
	Messages []json.RawMessage
	Tools    []ToolDefinition
}

// ChatResponse contains parsed loop control fields plus the complete raw
// assistant message. Append AssistantMessage to the next ChatRequest exactly;
// do not reconstruct it from Content and ToolCalls.
type ChatResponse struct {
	FinishReason     string
	Content          string
	ToolCalls        []ToolCall
	AssistantMessage json.RawMessage
}

type baseResponse struct {
	StatusCode int64  `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireChatRequest struct {
	Model               string            `json:"model"`
	Messages            []json.RawMessage `json:"messages"`
	Tools               []wireTool        `json:"tools,omitempty"`
	MaxCompletionTokens int               `json:"max_completion_tokens"`
	Thinking            struct {
		Type string `json:"type"`
	} `json:"thinking"`
	ReasoningSplit bool `json:"reasoning_split"`
	Stream         bool `json:"stream"`
}

// Chat performs one non-streaming M3 turn. It deliberately does not perform
// model discovery itself; callers should run EnsureAvailable once before the
// bounded multi-turn loop rather than adding a discovery request to every turn.
func (c *MiniMaxClient) Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("MiniMax chat requires at least one message")
	}
	messages := make([]json.RawMessage, len(request.Messages))
	for i, message := range request.Messages {
		if !json.Valid(message) {
			return nil, fmt.Errorf("MiniMax chat message %d is not valid JSON", i)
		}
		messages[i] = bytes.Clone(message)
	}

	tools := make([]wireTool, len(request.Tools))
	for i, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("MiniMax tool %d has no name", i)
		}
		if len(tool.Parameters) == 0 || !json.Valid(tool.Parameters) {
			return nil, fmt.Errorf("MiniMax tool %q parameters are not valid JSON", tool.Name)
		}
		tools[i] = wireTool{
			Type: "function",
			Function: wireFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  bytes.Clone(tool.Parameters),
			},
		}
	}

	wireRequest := wireChatRequest{
		Model:               MiniMaxM3Model,
		Messages:            messages,
		Tools:               tools,
		MaxCompletionTokens: maxCompletionTokens,
		ReasoningSplit:      true,
		Stream:              false,
	}
	wireRequest.Thinking.Type = "adaptive"

	payload, err := json.Marshal(wireRequest)
	if err != nil {
		return nil, fmt.Errorf("encode MiniMax chat request: %w", err)
	}
	body, err := c.request(ctx, http.MethodPost, "/chat/completions", payload, "chat completion")
	if err != nil {
		return nil, err
	}

	var response struct {
		Choices []struct {
			FinishReason string          `json:"finish_reason"`
			Message      json.RawMessage `json:"message"`
		} `json:"choices"`
		BaseResp baseResponse `json:"base_resp"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, &ProviderError{Operation: "decode chat completion", Cause: err}
	}
	if err := classifyBaseResponse("chat completion", response.BaseResp); err != nil {
		return nil, err
	}
	if len(response.Choices) == 0 {
		return nil, &ProviderError{Operation: "decode chat completion", Message: "response contained no choices"}
	}
	choice := response.Choices[0]
	if len(choice.Message) == 0 || !json.Valid(choice.Message) {
		return nil, &ProviderError{Operation: "decode chat completion", Message: "response contained an invalid assistant message"}
	}

	var parsedMessage struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []ToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal(choice.Message, &parsedMessage); err != nil {
		return nil, &ProviderError{Operation: "decode assistant message", Cause: err}
	}
	if parsedMessage.Role != "assistant" {
		return nil, &ProviderError{Operation: "validate chat completion", Message: "response message role was not assistant"}
	}
	switch choice.FinishReason {
	case "stop", "length", "content_filter":
		if len(parsedMessage.ToolCalls) != 0 {
			return nil, &ProviderError{Operation: "validate chat completion", Message: fmt.Sprintf("finish_reason %q included tool calls", choice.FinishReason)}
		}
	case "tool_calls":
		if len(parsedMessage.ToolCalls) == 0 {
			return nil, &ProviderError{Operation: "validate chat completion", Message: "finish_reason tool_calls contained no tool calls"}
		}
	default:
		return nil, &ProviderError{Operation: "validate chat completion", Message: fmt.Sprintf("unsupported finish_reason %q", choice.FinishReason)}
	}
	for i, call := range parsedMessage.ToolCalls {
		if call.Type != "function" || strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
			return nil, &ProviderError{Operation: "validate chat completion", Message: fmt.Sprintf("tool call %d is not a valid function call", i)}
		}
	}

	return &ChatResponse{
		FinishReason:     choice.FinishReason,
		Content:          parsedMessage.Content,
		ToolCalls:        parsedMessage.ToolCalls,
		AssistantMessage: bytes.Clone(choice.Message),
	}, nil
}

func (c *MiniMaxClient) request(ctx context.Context, method, path string, payload []byte, operation string) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, &ProviderError{Operation: "build " + operation + " request", Cause: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", version.UserAgent())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &ProviderError{Operation: operation, Cause: err}
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := readErrorMessage(response.Body, response.StatusCode)
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, &AuthenticationError{StatusCode: response.StatusCode, Message: message}
		case http.StatusTooManyRequests:
			return nil, &RateLimitError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After"), Message: message}
		default:
			return nil, &ProviderError{Operation: operation, StatusCode: response.StatusCode, Message: message}
		}
	}

	responseBody, err := readBounded(response.Body, maxResponseBodyBytes)
	if err != nil {
		return nil, &ProviderError{Operation: "read " + operation + " response", Cause: err}
	}
	return responseBody, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeded %d bytes", limit)
	}
	return body, nil
}

func readErrorMessage(reader io.Reader, statusCode int) string {
	body, err := io.ReadAll(io.LimitReader(reader, maxErrorBodyBytes+1))
	if err != nil {
		return "unable to read provider error response: " + err.Error()
	}
	truncated := int64(len(body)) > maxErrorBodyBytes
	if truncated {
		body = body[:maxErrorBodyBytes]
	}
	message := extractProviderMessage(body)
	if message == "" {
		message = http.StatusText(statusCode)
		if message == "" {
			message = "provider returned an empty error response"
		}
	}
	if truncated {
		message += " (response truncated)"
	}
	return message
}

func extractProviderMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message  string       `json:"message"`
		BaseResp baseResponse `json:"base_resp"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		for _, message := range []string{envelope.Error.Message, envelope.Message, envelope.BaseResp.StatusMsg} {
			if message = strings.TrimSpace(message); message != "" {
				return message
			}
		}
	}
	return strings.TrimSpace(string(body))
}

func classifyBaseResponse(operation string, response baseResponse) error {
	if response.StatusCode == 0 {
		return nil
	}
	switch response.StatusCode {
	case 1002:
		return &RateLimitError{ProviderCode: response.StatusCode, Message: response.StatusMsg}
	case 1004:
		return &AuthenticationError{ProviderCode: response.StatusCode, Message: response.StatusMsg}
	default:
		return &ProviderError{Operation: operation, ProviderCode: response.StatusCode, Message: response.StatusMsg}
	}
}
