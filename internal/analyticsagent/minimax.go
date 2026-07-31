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
	"sort"
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
	Operation    string
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

// RateLimitError is returned for HTTP 429 and MiniMax provider codes that
// represent temporary request or plan-usage exhaustion.
type RateLimitError struct {
	Operation    string
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
	// Usage is the provider's token accounting for this single round. It stays
	// zero when the provider does not report counters.
	Usage Usage
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
	ReasoningSplit bool               `json:"reasoning_split"`
	Stream         bool               `json:"stream"`
	StreamOptions  *wireStreamOptions `json:"stream_options,omitempty"`
}

// wireStreamOptions asks for the terminal usage chunk. Providers that do not
// implement it simply omit usage, which the loop already tolerates.
type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

func makeWireChatPayload(request ChatRequest, stream bool) ([]byte, error) {
	if len(request.Messages) == 0 {
		return nil, errors.New("MiniMax chat requires at least one message")
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
		var schema map[string]any
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil || schema == nil || schema["type"] != "object" {
			return nil, fmt.Errorf("MiniMax tool %q parameters must be a JSON Schema object", tool.Name)
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
		Stream:              stream,
	}
	wireRequest.Thinking.Type = "adaptive"
	if stream {
		wireRequest.StreamOptions = &wireStreamOptions{IncludeUsage: true}
	}

	payload, err := json.Marshal(wireRequest)
	if err != nil {
		return nil, fmt.Errorf("encode MiniMax chat request: %w", err)
	}
	return payload, nil
}

// Chat performs one non-streaming M3 turn. It deliberately does not perform
// model discovery itself; callers should run EnsureAvailable once before the
// bounded multi-turn loop rather than adding a discovery request to every turn.
func (c *MiniMaxClient) Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	payload, err := makeWireChatPayload(request, false)
	if err != nil {
		return nil, err
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
		Usage    json.RawMessage `json:"usage"`
		BaseResp baseResponse    `json:"base_resp"`
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
		Usage:            requestUsage(parseUsage(response.Usage)),
	}, nil
}

// requestUsage guarantees that a completed round is always counted, even when
// the provider omitted its token counters entirely.
func requestUsage(usage Usage) Usage {
	if usage.Requests == 0 {
		usage.Requests = 1
	}
	return usage
}

type streamChatChunk struct {
	Choices  []streamChatChoice `json:"choices"`
	Usage    json.RawMessage    `json:"usage"`
	BaseResp baseResponse       `json:"base_resp"`
}

type streamChatChoice struct {
	Index        int             `json:"index"`
	Delta        json.RawMessage `json:"delta"`
	FinishReason json.RawMessage `json:"finish_reason"`
}

type streamToolCallAccumulator struct {
	index               int
	id                  string
	typeName            string
	name                streamFragmentAccumulator
	arguments           streamFragmentAccumulator
	extraFields         map[string]json.RawMessage
	extraFunctionFields map[string]json.RawMessage
}

type streamFragmentMode uint8

const (
	streamFragmentIncremental streamFragmentMode = iota + 1
	streamFragmentCumulative
)

// streamFragmentAccumulator keeps both interpretations until the complete
// tool call is available. MiniMax installations have emitted both ordinary
// OpenAI fragments and cumulative fields; choosing independently per chunk can
// silently drop valid fragments when a fragment happens to prefix the current
// value.
type streamFragmentAccumulator struct {
	incremental        string
	cumulative         string
	sawValue           bool
	cumulativePossible bool
}

type streamReasoningDetails struct {
	items map[int]map[string]json.RawMessage
}

type streamAssistantAccumulator struct {
	role                    string
	content                 string
	contentPresent          bool
	contentNull             bool
	contentString           bool
	reasoningContent        string
	hasReasoningContent     bool
	reasoningContentNull    bool
	reasoningContentString  bool
	reasoningDetails        streamReasoningDetails
	hasReasoningDetails     bool
	reasoningDetailsNull    bool
	toolCalls               map[int]*streamToolCallAccumulator
	knownToolNames          map[string]struct{}
	extraAssistantFields    map[string]json.RawMessage
	providerFinishReason    string
	providerFinishReasonSet bool
	usage                   Usage
}

// ChatStream performs one streamed M3 turn. MiniMax deployments have emitted
// prose as both cumulative values and ordinary incremental chunks, so
// onContent receives a normalized append-only delta in either mode. Private
// reasoning fields are accumulated for provider replay and never sent to the
// callback.
func (c *MiniMaxClient) ChatStream(ctx context.Context, request ChatRequest, onContent func(string) error) (*ChatResponse, error) {
	payload, err := makeWireChatPayload(request, true)
	if err != nil {
		return nil, err
	}
	response, err := c.requestResponse(ctx, http.MethodPost, "/chat/completions", payload, "stream chat completion", "text/event-stream")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		body, readErr := readBounded(response.Body, maxResponseBodyBytes)
		if readErr != nil {
			return nil, &ProviderError{Operation: "read stream chat completion response", Cause: readErr}
		}
		var envelope struct {
			BaseResp baseResponse `json:"base_resp"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			if err := classifyBaseResponse("stream chat completion", envelope.BaseResp); err != nil {
				return nil, err
			}
		}
		return nil, &ProviderError{Operation: "validate stream chat completion", Message: fmt.Sprintf("unexpected Content-Type %q", response.Header.Get("Content-Type"))}
	}

	limited := &io.LimitedReader{R: response.Body, N: maxResponseBodyBytes + 1}
	reader := bufio.NewReader(limited)
	accumulator := &streamAssistantAccumulator{knownToolNames: make(map[string]struct{}, len(request.Tools))}
	for _, tool := range request.Tools {
		accumulator.knownToolNames[tool.Name] = struct{}{}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, readErr := readServerSentData(reader)
		if limited.N == 0 {
			return nil, &ProviderError{Operation: "read stream chat completion response", Message: fmt.Sprintf("response body exceeded %d bytes", maxResponseBodyBytes)}
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

		var chunk streamChatChunk
		if err := json.Unmarshal(trimmed, &chunk); err != nil {
			return nil, &ProviderError{Operation: "decode stream chat completion", Cause: err}
		}
		if err := classifyBaseResponse("stream chat completion", chunk.BaseResp); err != nil {
			return nil, err
		}
		// Usage arrives in a terminal chunk that usually carries no choices.
		// Providers may also repeat it; the last complete report wins.
		if usage := parseUsage(chunk.Usage); usage.HasTokens() {
			accumulator.usage = usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				return nil, &ProviderError{Operation: "validate stream chat completion", Message: fmt.Sprintf("unsupported choice index %d", choice.Index)}
			}
			if len(choice.Delta) != 0 && !bytes.Equal(bytes.TrimSpace(choice.Delta), []byte("null")) {
				if err := accumulator.applyDelta(ctx, choice.Delta, onContent); err != nil {
					return nil, err
				}
			}
			finishReason, present, err := decodeOptionalString(choice.FinishReason, "finish_reason")
			if err != nil {
				return nil, &ProviderError{Operation: "decode stream chat completion", Cause: err}
			}
			if present {
				if accumulator.providerFinishReasonSet && accumulator.providerFinishReason != finishReason {
					return nil, &ProviderError{Operation: "validate stream chat completion", Message: "stream contained conflicting finish reasons"}
				}
				accumulator.providerFinishReason = finishReason
				accumulator.providerFinishReasonSet = true
			}
		}
	}

	if !accumulator.providerFinishReasonSet || accumulator.providerFinishReason == "" {
		return nil, &ProviderError{Operation: "validate stream chat completion", Message: "stream contained no finish reason"}
	}
	return accumulator.response()
}

func (a *streamAssistantAccumulator) applyDelta(ctx context.Context, raw json.RawMessage, onContent func(string) error) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return &ProviderError{Operation: "decode stream assistant delta", Cause: err}
	}
	if a.extraAssistantFields == nil {
		a.extraAssistantFields = make(map[string]json.RawMessage)
	}

	if value, ok := fields["role"]; ok {
		role, present, err := decodeOptionalString(value, "delta.role")
		if err != nil {
			return &ProviderError{Operation: "decode stream assistant delta", Cause: err}
		}
		if present && role != "" {
			if a.role != "" && a.role != role {
				return &ProviderError{Operation: "validate stream chat completion", Message: "stream changed assistant role"}
			}
			a.role = role
		}
	}

	if value, ok := fields["content"]; ok {
		a.contentPresent = true
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if !a.contentString {
				a.contentNull = true
			}
		} else {
			content, present, err := decodeOptionalString(value, "delta.content")
			if err != nil {
				return &ProviderError{Operation: "decode stream assistant delta", Cause: err}
			}
			if present {
				a.contentNull = false
				a.contentString = true
				if content != "" {
					updated, delta := advanceAdaptiveStreamString(a.content, content)
					a.content = updated
					if delta != "" && onContent != nil {
						if err := onContent(delta); err != nil {
							return fmt.Errorf("MiniMax content callback: %w", err)
						}
						if err := ctx.Err(); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	if value, ok := fields["reasoning_content"]; ok {
		a.hasReasoningContent = true
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if !a.reasoningContentString {
				a.reasoningContentNull = true
			}
		} else {
			reasoning, present, err := decodeOptionalString(value, "delta.reasoning_content")
			if err != nil {
				return &ProviderError{Operation: "decode stream assistant delta", Cause: err}
			}
			if present {
				a.reasoningContentNull = false
				a.reasoningContentString = true
				if reasoning != "" {
					updated, _ := advanceAdaptiveStreamString(a.reasoningContent, reasoning)
					a.reasoningContent = updated
				}
			}
		}
	}

	if value, ok := fields["reasoning_details"]; ok {
		a.hasReasoningDetails = true
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if len(a.reasoningDetails.items) == 0 {
				a.reasoningDetailsNull = true
			}
		} else {
			if err := a.reasoningDetails.merge(value); err != nil {
				return err
			}
			a.reasoningDetailsNull = false
		}
	}

	if value, ok := fields["tool_calls"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		var calls []json.RawMessage
		if err := json.Unmarshal(value, &calls); err != nil {
			return &ProviderError{Operation: "decode stream tool calls", Cause: err}
		}
		for _, call := range calls {
			if err := a.mergeToolCall(call); err != nil {
				return err
			}
		}
	}

	for name, value := range fields {
		switch name {
		case "role", "content", "reasoning_content", "reasoning_details", "tool_calls":
			continue
		}
		a.extraAssistantFields[name] = bytes.Clone(value)
	}
	return nil
}

func (a *streamAssistantAccumulator) mergeToolCall(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return &ProviderError{Operation: "decode stream tool call", Cause: err}
	}
	if a.toolCalls == nil {
		a.toolCalls = make(map[int]*streamToolCallAccumulator)
	}
	index := 0
	if rawIndex, ok := fields["index"]; ok && !bytes.Equal(bytes.TrimSpace(rawIndex), []byte("null")) {
		if err := json.Unmarshal(rawIndex, &index); err != nil {
			return &ProviderError{Operation: "decode stream tool call", Cause: err}
		}
	} else if len(a.toolCalls) > 1 {
		return &ProviderError{Operation: "validate stream tool calls", Message: "tool call delta omitted an ambiguous index"}
	}
	if index < 0 || index > 1024 {
		return &ProviderError{Operation: "validate stream tool calls", Message: fmt.Sprintf("invalid tool call index %d", index)}
	}
	call := a.toolCalls[index]
	if call == nil {
		call = &streamToolCallAccumulator{
			index:               index,
			extraFields:         make(map[string]json.RawMessage),
			extraFunctionFields: make(map[string]json.RawMessage),
		}
		a.toolCalls[index] = call
	}
	if value, ok := fields["id"]; ok {
		id, present, err := decodeOptionalString(value, "tool_call.id")
		if err != nil {
			return &ProviderError{Operation: "decode stream tool call", Cause: err}
		}
		if present && id != "" {
			if call.id != "" && call.id != id {
				return &ProviderError{Operation: "validate stream tool calls", Message: fmt.Sprintf("tool call %d changed id", index)}
			}
			call.id = id
		}
	}
	if value, ok := fields["type"]; ok {
		typeName, present, err := decodeOptionalString(value, "tool_call.type")
		if err != nil {
			return &ProviderError{Operation: "decode stream tool call", Cause: err}
		}
		if present && typeName != "" {
			if call.typeName != "" && call.typeName != typeName {
				return &ProviderError{Operation: "validate stream tool calls", Message: fmt.Sprintf("tool call %d changed type", index)}
			}
			call.typeName = typeName
		}
	}
	if value, ok := fields["function"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		var functionFields map[string]json.RawMessage
		if err := json.Unmarshal(value, &functionFields); err != nil {
			return &ProviderError{Operation: "decode stream tool function", Cause: err}
		}
		if nameRaw, ok := functionFields["name"]; ok {
			name, present, err := decodeOptionalString(nameRaw, "tool_call.function.name")
			if err != nil {
				return &ProviderError{Operation: "decode stream tool function", Cause: err}
			}
			if present {
				call.name.add(name)
			}
		}
		if argumentsRaw, ok := functionFields["arguments"]; ok {
			arguments, present, err := decodeOptionalString(argumentsRaw, "tool_call.function.arguments")
			if err != nil {
				return &ProviderError{Operation: "decode stream tool function", Cause: err}
			}
			if present {
				call.arguments.add(arguments)
			}
		}
		for name, nested := range functionFields {
			if name != "name" && name != "arguments" {
				call.extraFunctionFields[name] = bytes.Clone(nested)
			}
		}
	}
	for name, value := range fields {
		if name != "id" && name != "type" && name != "function" {
			call.extraFields[name] = bytes.Clone(value)
		}
	}
	return nil
}

func (a *streamAssistantAccumulator) response() (*ChatResponse, error) {
	if a.role != "assistant" {
		return nil, &ProviderError{Operation: "validate stream chat completion", Message: "response message role was not assistant"}
	}
	indices := make([]int, 0, len(a.toolCalls))
	for index := range a.toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	toolCalls := make([]ToolCall, 0, len(indices))
	rawToolCalls := make([]json.RawMessage, 0, len(indices))
	for _, index := range indices {
		value := a.toolCalls[index]
		call, mode, err := value.resolve(a.knownToolNames)
		if err != nil {
			return nil, err
		}
		if call.Type != "function" || strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
			return nil, &ProviderError{Operation: "validate stream chat completion", Message: fmt.Sprintf("tool call %d is not a valid function call", index)}
		}
		toolCalls = append(toolCalls, call)
		rawCall, err := value.raw(call, mode)
		if err != nil {
			return nil, err
		}
		rawToolCalls = append(rawToolCalls, rawCall)
	}

	switch a.providerFinishReason {
	case "stop", "length", "content_filter":
		if len(toolCalls) != 0 {
			return nil, &ProviderError{Operation: "validate stream chat completion", Message: fmt.Sprintf("finish_reason %q included tool calls", a.providerFinishReason)}
		}
	case "tool_calls":
		if len(toolCalls) == 0 {
			return nil, &ProviderError{Operation: "validate stream chat completion", Message: "finish_reason tool_calls contained no tool calls"}
		}
	default:
		return nil, &ProviderError{Operation: "validate stream chat completion", Message: fmt.Sprintf("unsupported finish_reason %q", a.providerFinishReason)}
	}

	message := make(map[string]json.RawMessage, len(a.extraAssistantFields)+5)
	for name, value := range a.extraAssistantFields {
		message[name] = bytes.Clone(value)
	}
	message["role"], _ = json.Marshal(a.role)
	if a.contentPresent {
		if a.contentNull && !a.contentString {
			message["content"] = json.RawMessage(`null`)
		} else {
			message["content"], _ = json.Marshal(a.content)
		}
	}
	if a.hasReasoningContent {
		if a.reasoningContentNull && !a.reasoningContentString {
			message["reasoning_content"] = json.RawMessage(`null`)
		} else {
			message["reasoning_content"], _ = json.Marshal(a.reasoningContent)
		}
	}
	if a.hasReasoningDetails {
		if a.reasoningDetailsNull && len(a.reasoningDetails.items) == 0 {
			message["reasoning_details"] = json.RawMessage(`null`)
		} else {
			reasoning, err := a.reasoningDetails.raw()
			if err != nil {
				return nil, &ProviderError{Operation: "encode stream reasoning details", Cause: err}
			}
			message["reasoning_details"] = reasoning
		}
	}
	if len(rawToolCalls) != 0 {
		encoded, err := json.Marshal(rawToolCalls)
		if err != nil {
			return nil, &ProviderError{Operation: "encode stream tool calls", Cause: err}
		}
		message["tool_calls"] = encoded
	}
	assistantMessage, err := json.Marshal(message)
	if err != nil {
		return nil, &ProviderError{Operation: "encode stream assistant message", Cause: err}
	}
	return &ChatResponse{
		FinishReason:     a.providerFinishReason,
		Content:          a.content,
		ToolCalls:        toolCalls,
		AssistantMessage: assistantMessage,
		Usage:            requestUsage(a.usage),
	}, nil
}

func (d *streamReasoningDetails) merge(raw json.RawMessage) error {
	var incoming []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return &ProviderError{Operation: "decode stream reasoning details", Cause: err}
	}
	if d.items == nil {
		d.items = make(map[int]map[string]json.RawMessage)
	}
	for position, item := range incoming {
		index := position
		if rawIndex, ok := item["index"]; ok {
			if err := json.Unmarshal(rawIndex, &index); err != nil || index < 0 || index > 1024 {
				return &ProviderError{Operation: "validate stream reasoning details", Message: "reasoning detail contained an invalid index"}
			}
		}
		current := d.items[index]
		if current == nil {
			current = make(map[string]json.RawMessage)
			d.items[index] = current
		}
		for name, value := range item {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				continue
			}
			if name == "text" {
				textValue, present, err := decodeOptionalString(value, "reasoning_details.text")
				if err != nil {
					return &ProviderError{Operation: "decode stream reasoning details", Cause: err}
				}
				if present {
					var currentText string
					if previous, exists := current[name]; exists {
						if err := json.Unmarshal(previous, &currentText); err != nil {
							return &ProviderError{Operation: "decode stream reasoning details", Cause: err}
						}
					}
					updated, _ := advanceAdaptiveStreamString(currentText, textValue)
					current[name], _ = json.Marshal(updated)
				}
				continue
			}
			current[name] = bytes.Clone(value)
		}
	}
	return nil
}

func (d *streamReasoningDetails) raw() (json.RawMessage, error) {
	indices := make([]int, 0, len(d.items))
	for index := range d.items {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	items := make([]map[string]json.RawMessage, 0, len(indices))
	for _, index := range indices {
		items = append(items, d.items[index])
	}
	encoded, err := json.Marshal(items)
	return json.RawMessage(encoded), err
}

func decodeOptionalString(raw json.RawMessage, field string) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", false, fmt.Errorf("%s must be a string or null: %w", field, err)
	}
	return value, true, nil
}

func (f *streamFragmentAccumulator) add(next string) {
	if next == "" {
		return
	}
	if !f.sawValue {
		f.sawValue = true
		f.cumulativePossible = true
		f.incremental = next
		f.cumulative = next
		return
	}
	f.incremental += next
	if f.cumulativePossible {
		if strings.HasPrefix(next, f.cumulative) {
			f.cumulative = next
		} else {
			f.cumulativePossible = false
		}
	}
}

func (f streamFragmentAccumulator) value(mode streamFragmentMode) string {
	if mode == streamFragmentCumulative {
		return f.cumulative
	}
	return f.incremental
}

func (c *streamToolCallAccumulator) resolve(knownToolNames map[string]struct{}) (ToolCall, streamFragmentMode, error) {
	incremental := ToolCall{
		ID:       c.id,
		Type:     c.typeName,
		Function: FunctionCall{Name: c.name.value(streamFragmentIncremental), Arguments: c.arguments.value(streamFragmentIncremental)},
	}
	cumulativePossible := (!c.name.sawValue || c.name.cumulativePossible) && (!c.arguments.sawValue || c.arguments.cumulativePossible)
	if !cumulativePossible {
		return incremental, streamFragmentIncremental, nil
	}
	cumulative := ToolCall{
		ID:       c.id,
		Type:     c.typeName,
		Function: FunctionCall{Name: c.name.value(streamFragmentCumulative), Arguments: c.arguments.value(streamFragmentCumulative)},
	}
	if incremental == cumulative {
		return incremental, streamFragmentIncremental, nil
	}

	incrementalJSON := json.Valid([]byte(incremental.Function.Arguments))
	cumulativeJSON := json.Valid([]byte(cumulative.Function.Arguments))
	if incrementalJSON != cumulativeJSON {
		if incrementalJSON {
			return incremental, streamFragmentIncremental, nil
		}
		return cumulative, streamFragmentCumulative, nil
	}
	_, incrementalKnown := knownToolNames[incremental.Function.Name]
	_, cumulativeKnown := knownToolNames[cumulative.Function.Name]
	if incrementalKnown != cumulativeKnown {
		if incrementalKnown {
			return incremental, streamFragmentIncremental, nil
		}
		return cumulative, streamFragmentCumulative, nil
	}
	return ToolCall{}, 0, &ProviderError{
		Operation: "validate stream tool calls",
		Message:   fmt.Sprintf("tool call %d fragment mode was ambiguous", c.index),
	}
}

func (c *streamToolCallAccumulator) raw(call ToolCall, mode streamFragmentMode) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage, len(c.extraFields)+3)
	for name, value := range c.extraFields {
		fields[name] = bytes.Clone(value)
	}
	fields["id"], _ = json.Marshal(call.ID)
	fields["type"], _ = json.Marshal(call.Type)
	function := make(map[string]json.RawMessage, len(c.extraFunctionFields)+2)
	for name, value := range c.extraFunctionFields {
		function[name] = bytes.Clone(value)
	}
	function["name"], _ = json.Marshal(c.name.value(mode))
	function["arguments"], _ = json.Marshal(c.arguments.value(mode))
	encodedFunction, err := json.Marshal(function)
	if err != nil {
		return nil, &ProviderError{Operation: "encode stream tool function", Cause: err}
	}
	fields["function"] = encodedFunction
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, &ProviderError{Operation: "encode stream tool call", Cause: err}
	}
	return json.RawMessage(encoded), nil
}

// advanceAdaptiveStreamString normalizes either provider convention into an
// append-only value and delta. A value extending the accumulated text is
// cumulative; every other value is an incremental chunk.
func advanceAdaptiveStreamString(current, next string) (string, string) {
	if next == "" {
		return current, ""
	}
	if strings.HasPrefix(next, current) {
		return next, next[len(current):]
	}
	return current + next, next
}

// readServerSentData implements the SSE data-field rules needed by the provider
// transport: comments and unknown fields are ignored, consecutive data fields
// are joined with newlines, CRLF is accepted, and a blank line dispatches the
// event. The caller wraps reader with a strict total-byte limit.
func readServerSentData(reader *bufio.Reader) ([]byte, error) {
	var data bytes.Buffer
	hasData := false
	for {
		line, err := reader.ReadString('\n')
		if len(line) != 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if line == "" {
				if hasData {
					return data.Bytes(), nil
				}
			} else if !strings.HasPrefix(line, ":") {
				field, value, found := strings.Cut(line, ":")
				if !found {
					field = line
					value = ""
				}
				if field == "data" {
					if strings.HasPrefix(value, " ") {
						value = value[1:]
					}
					if hasData {
						data.WriteByte('\n')
					}
					data.WriteString(value)
					hasData = true
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && hasData {
				return data.Bytes(), nil
			}
			return nil, err
		}
	}
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
			return nil, &AuthenticationError{Operation: operation, StatusCode: response.StatusCode, Message: message}
		case http.StatusTooManyRequests:
			return nil, &RateLimitError{Operation: operation, StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After"), Message: message}
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

// requestResponse leaves a successful response body open for incremental
// consumption. Error responses are still classified and closed here so stream
// callers receive the same authentication/rate-limit/provider error types as
// the buffered transport.
func (c *MiniMaxClient) requestResponse(ctx context.Context, method, path string, payload []byte, operation, accept string) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, &ProviderError{Operation: "build " + operation + " request", Cause: err}
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", version.UserAgent())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &ProviderError{Operation: operation, Cause: err}
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return response, nil
	}
	defer response.Body.Close()
	message := readErrorMessage(response.Body, response.StatusCode)
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, &AuthenticationError{Operation: operation, StatusCode: response.StatusCode, Message: message}
	case http.StatusTooManyRequests:
		return nil, &RateLimitError{Operation: operation, StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After"), Message: message}
	default:
		return nil, &ProviderError{Operation: operation, StatusCode: response.StatusCode, Message: message}
	}
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
	case 1002, 2056:
		return &RateLimitError{Operation: operation, ProviderCode: response.StatusCode, Message: response.StatusMsg}
	case 1004, 2049:
		return &AuthenticationError{Operation: operation, ProviderCode: response.StatusCode, Message: response.StatusMsg}
	default:
		return &ProviderError{Operation: operation, ProviderCode: response.StatusCode, Message: response.StatusMsg}
	}
}
