package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opencode-dashboard/internal/analyticsagent"
	"opencode-dashboard/internal/chatstore"
)

const maxAssistantRequestBytes = 64 << 10

const assistantStatusTimeout = 4 * time.Second

const maxAssistantSessionList = 100

type AssistantService interface {
	Status(context.Context) analyticsagent.Status
	Chat(context.Context, analyticsagent.ChatInput) (analyticsagent.ChatResult, error)
}

// AssistantStreamingService is optional so alternate/test assistant services
// that only support the legacy buffered endpoint remain source compatible.
type AssistantStreamingService interface {
	ChatStream(context.Context, analyticsagent.ChatInput, func(analyticsagent.StreamEvent) error) (analyticsagent.ChatResult, error)
}

// AssistantChatStore is the optional durable log for assistant conversations.
// When absent, chat still works statelessly and the session endpoints report
// persistence as unavailable.
type AssistantChatStore interface {
	AppendTurn(context.Context, chatstore.Turn) (chatstore.Receipt, error)
	ListSessions(context.Context, int) ([]chatstore.Session, error)
	GetSession(context.Context, string) (*chatstore.SessionDetail, error)
	DeleteSession(context.Context, string) (bool, error)
	SessionExists(context.Context, string) (bool, error)
}

func (h *Handlers) AssistantStatus(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	status := analyticsagent.BaseStatus()
	if h.assistant == nil {
		status.Reason = "MiniMax M3 is not configured"
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), assistantStatusTimeout)
		defer cancel()
		status = h.assistant.Status(ctx)
	}
	status.SessionsPersisted = h.chatlog != nil
	writeJSONNoStore(w, http.StatusOK, status)
}

// assistantChatResponseBody extends the canonical chat result with where the
// turn was persisted (absent without persistence).
type assistantChatResponseBody struct {
	analyticsagent.ChatResult
	SessionID    string           `json:"session_id,omitempty"`
	SessionTitle string           `json:"session_title,omitempty"`
	SessionUsage *chatstore.Usage `json:"session_usage,omitempty"`
}

func (h *Handlers) AssistantChat(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAssistantError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if h.assistant == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "MiniMax M3 assistant is unavailable")
		return
	}

	input, ok := decodeAssistantChatInput(w, r)
	if !ok {
		return
	}
	if !h.checkAssistantChatSession(w, r.Context(), input.SessionID) {
		return
	}

	result, err := h.assistant.Chat(r.Context(), input)
	if err != nil {
		h.logAssistantFailure("buffered", false, err)
		writeAssistantServiceError(w, err)
		return
	}
	body := assistantChatResponseBody{ChatResult: result}
	if receipt, persisted := h.persistAssistantTurn(r.Context(), input, result); persisted {
		body.SessionID = receipt.SessionID
		body.SessionTitle = receipt.Title
		body.SessionUsage = &receipt.Session
	}
	writeJSONNoStore(w, http.StatusOK, body)
}

// checkAssistantChatSession rejects turns addressed to a session that cannot
// be appended to, before any provider work happens.
func (h *Handlers) checkAssistantChatSession(w http.ResponseWriter, ctx context.Context, sessionID string) bool {
	if sessionID == "" {
		return true
	}
	if h.chatlog == nil {
		writeAssistantError(w, http.StatusBadRequest, "assistant chat persistence is unavailable")
		return false
	}
	exists, err := h.chatlog.SessionExists(ctx, sessionID)
	if err != nil {
		h.logger.Error("assistant chat session lookup failed", "error", err)
		writeAssistantError(w, http.StatusInternalServerError, "assistant chat session lookup failed")
		return false
	}
	if !exists {
		writeAssistantError(w, http.StatusNotFound, "assistant chat session not found")
		return false
	}
	return true
}

// persistAssistantTurn appends the completed exchange — prompt, answer, every
// tool call, every specialist run, and the turn's accounting — to the durable
// chat log. Persistence failures are logged and never fail the chat itself.
func (h *Handlers) persistAssistantTurn(ctx context.Context, input analyticsagent.ChatInput, result analyticsagent.ChatResult) (chatstore.Receipt, bool) {
	if h.chatlog == nil || len(input.Messages) == 0 {
		return chatstore.Receipt{}, false
	}
	prompt := input.Messages[len(input.Messages)-1]
	if prompt.Role != "user" {
		return chatstore.Receipt{}, false
	}

	// The chat may have been canceled-adjacent; persist with a background-safe
	// context so a browser disconnect after completion cannot lose the turn.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	receipt, err := h.chatlog.AppendTurn(persistCtx, chatstore.Turn{
		SessionID:          input.SessionID,
		Provider:           storedProvider(result),
		Model:              result.Model,
		Agent:              string(result.Agent),
		ConsentVersion:     input.ConsentVersion,
		Context:            storedTurnContext(input.Context),
		UserContent:        prompt.Content,
		AssistantContent:   result.Message.Content,
		AssistantSignature: result.Message.Signature,
		Rounds:             result.Rounds,
		DurationMS:         result.DurationMS,
		Usage:              storedUsage(result.Usage),
		Notices:            result.Notices,
		ToolCalls:          storedToolCalls(result.ToolCalls),
		Subagents:          storedSubagentRuns(result.Subagents),
	})
	if err != nil {
		h.logger.Error("assistant chat turn was not persisted", "error", err)
		return chatstore.Receipt{}, false
	}
	return receipt, true
}

func storedProvider(result analyticsagent.ChatResult) string {
	if provider := strings.TrimSpace(result.Provider); provider != "" {
		return provider
	}
	return analyticsagent.ProviderMiniMax
}

func storedTurnContext(value *analyticsagent.BrowserContext) chatstore.TurnContext {
	if value == nil {
		return chatstore.TurnContext{}
	}
	return chatstore.TurnContext{
		Route: value.Route, Source: value.Source, Period: value.Period, Timezone: value.Timezone,
	}
}

func storedUsage(value analyticsagent.Usage) chatstore.Usage {
	return chatstore.Usage{
		Requests:          value.Requests,
		InputTokens:       value.InputTokens,
		OutputTokens:      value.OutputTokens,
		CachedInputTokens: value.CachedInputTokens,
		ReasoningTokens:   value.ReasoningTokens,
		TotalTokens:       value.TotalTokens,
	}
}

func storedToolCalls(calls []analyticsagent.ToolCallRecord) []chatstore.ToolCall {
	stored := make([]chatstore.ToolCall, 0, len(calls))
	for index, call := range calls {
		stored = append(stored, chatstore.ToolCall{
			Index: index, Name: call.Name, CallRef: call.CallID,
			ParentCallRef: call.ParentCallID, Agent: string(call.Agent), Round: call.Round,
			Arguments: call.Arguments, Result: call.Result,
			OK: call.OK, DurationMS: call.DurationMS,
		})
	}
	return stored
}

func storedSubagentRuns(runs []analyticsagent.SubagentRunRecord) []chatstore.SubagentRun {
	stored := make([]chatstore.SubagentRun, 0, len(runs))
	for index, run := range runs {
		stored = append(stored, chatstore.SubagentRun{
			Index: index, CallRef: run.CallID, Agent: string(run.Agent), Title: run.Title,
			Task: run.Task, Status: run.Status, Report: run.Report, Error: run.Error,
			Rounds: run.Rounds, ToolsUsed: run.ToolsUsed, DurationMS: run.DurationMS,
			Usage: storedUsage(run.Usage),
		})
	}
	return stored
}

func (h *Handlers) AssistantSessions(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.chatlog == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant chat persistence is unavailable")
		return
	}
	sessions, err := h.chatlog.ListSessions(r.Context(), maxAssistantSessionList)
	if err != nil {
		h.logger.Error("assistant chat sessions listing failed", "error", err)
		writeAssistantError(w, http.StatusInternalServerError, "assistant chat sessions are unavailable")
		return
	}
	writeJSONNoStore(w, http.StatusOK, struct {
		Sessions []chatstore.Session `json:"sessions"`
	}{Sessions: sessions})
}

func (h *Handlers) AssistantSessionByID(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.chatlog == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant chat persistence is unavailable")
		return
	}
	detail, err := h.chatlog.GetSession(r.Context(), r.PathValue("id"))
	if errors.Is(err, chatstore.ErrSessionNotFound) {
		writeAssistantError(w, http.StatusNotFound, "assistant chat session not found")
		return
	}
	if err != nil {
		h.logger.Error("assistant chat session load failed", "error", err)
		writeAssistantError(w, http.StatusInternalServerError, "assistant chat session is unavailable")
		return
	}
	writeJSONNoStore(w, http.StatusOK, detail)
}

func (h *Handlers) AssistantSessionDelete(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.chatlog == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant chat persistence is unavailable")
		return
	}
	deleted, err := h.chatlog.DeleteSession(r.Context(), r.PathValue("id"))
	if err != nil {
		h.logger.Error("assistant chat session delete failed", "error", err)
		writeAssistantError(w, http.StatusInternalServerError, "assistant chat session delete failed")
		return
	}
	if !deleted {
		writeAssistantError(w, http.StatusNotFound, "assistant chat session not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// AssistantChatStream runs the same bounded report loop as AssistantChat while
// exposing privacy-safe progress as newline-delimited JSON. The response is
// committed lazily so validation and service admission errors retain their
// normal HTTP status and JSON error contract.
func (h *Handlers) AssistantChatStream(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAssistantError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if h.assistant == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "MiniMax M3 assistant is unavailable")
		return
	}
	streaming, ok := h.assistant.(AssistantStreamingService)
	if !ok {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant streaming is unavailable")
		return
	}
	input, ok := decodeAssistantChatInput(w, r)
	if !ok {
		return
	}
	if !h.checkAssistantChatSession(w, r.Context(), input.SessionID) {
		return
	}

	stream := newAssistantNDJSONStream(w)
	result, err := streaming.ChatStream(r.Context(), input, stream.forward)
	if err != nil {
		h.logAssistantFailure("stream", stream.committed, err)
		if !stream.committed {
			writeAssistantServiceError(w, err)
			return
		}
		// A canceled request normally means the peer has gone away, so there is no
		// useful or reliable terminal frame left to write.
		if !errors.Is(r.Context().Err(), context.Canceled) {
			_, message := assistantServiceError(err)
			_ = stream.write(assistantStreamErrorFrame{Type: "error", Message: message})
		}
		return
	}
	receipt, persisted := h.persistAssistantTurn(r.Context(), input, result)

	if err := stream.start(result.Model); err != nil {
		return
	}
	model := strings.TrimSpace(result.Model)
	if model == "" {
		model = analyticsagent.MiniMaxM3Model
	}
	frame := assistantStreamCompleteFrame{
		Type:       "complete",
		Message:    result.Message,
		Model:      model,
		Provider:   storedProvider(result),
		Agent:      result.Agent,
		Rounds:     result.Rounds,
		DurationMS: result.DurationMS,
		Usage:      result.Usage,
		ToolsUsed:  nonNilStrings(result.ToolsUsed),
		ToolCalls:  result.ToolCalls,
		Subagents:  result.Subagents,
		Notices:    result.Notices,
	}
	if frame.ToolCalls == nil {
		frame.ToolCalls = make([]analyticsagent.ToolCallRecord, 0)
	}
	if frame.Subagents == nil {
		frame.Subagents = make([]analyticsagent.SubagentRunRecord, 0)
	}
	if persisted {
		frame.SessionID = receipt.SessionID
		frame.SessionTitle = receipt.Title
		frame.SessionUsage = &receipt.Session
	}
	_ = stream.write(frame)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return make([]string, 0)
	}
	return values
}

func decodeAssistantChatInput(w http.ResponseWriter, r *http.Request) (analyticsagent.ChatInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAssistantRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input analyticsagent.ChatInput
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAssistantError(w, http.StatusRequestEntityTooLarge, "assistant request exceeds 64 KiB")
			return analyticsagent.ChatInput{}, false
		}
		writeAssistantError(w, http.StatusBadRequest, "assistant request must be valid JSON")
		return analyticsagent.ChatInput{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAssistantError(w, http.StatusBadRequest, "assistant request must contain one JSON object")
		return analyticsagent.ChatInput{}, false
	}
	if err := analyticsagent.ValidateChatInput(input); err != nil {
		writeAssistantError(w, http.StatusBadRequest, "assistant messages are invalid")
		return analyticsagent.ChatInput{}, false
	}
	return input, true
}

type assistantNDJSONStream struct {
	w          http.ResponseWriter
	controller *http.ResponseController
	committed  bool
	started    bool
}

type assistantStreamProgressFrame struct {
	Type string `json:"type"`
	// Agent, ParentCallID, and Round let the browser attribute progress to the
	// lead analyst or to a specialist working under a delegation.
	Agent        analyticsagent.AgentID `json:"agent,omitempty"`
	ParentCallID string                 `json:"parent_call_id,omitempty"`
	Round        int                    `json:"round,omitempty"`
	Delta        string                 `json:"delta,omitempty"`
	CallID       string                 `json:"call_id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	OK           *bool                  `json:"ok,omitempty"`
	Model        string                 `json:"model,omitempty"`
	Arguments    json.RawMessage        `json:"arguments,omitempty"`
	Result       json.RawMessage        `json:"result,omitempty"`
	DurationMS   int64                  `json:"duration_ms,omitempty"`
	Subagent     *assistantSubagentInfo `json:"subagent,omitempty"`
}

// assistantSubagentInfo describes a delegated investigation as it starts and as
// it finishes.
type assistantSubagentInfo struct {
	Agent     analyticsagent.AgentID `json:"agent"`
	Title     string                 `json:"title"`
	Task      string                 `json:"task"`
	Status    string                 `json:"status,omitempty"`
	Report    string                 `json:"report,omitempty"`
	Rounds    int                    `json:"rounds,omitempty"`
	ToolsUsed []string               `json:"tools_used,omitempty"`
	Usage     *analyticsagent.Usage  `json:"usage,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

type assistantStreamCompleteFrame struct {
	Type         string                             `json:"type"`
	Message      analyticsagent.BrowserMessage      `json:"message"`
	Model        string                             `json:"model"`
	Provider     string                             `json:"provider,omitempty"`
	Agent        analyticsagent.AgentID             `json:"agent,omitempty"`
	Rounds       int                                `json:"rounds,omitempty"`
	DurationMS   int64                              `json:"duration_ms,omitempty"`
	Usage        analyticsagent.Usage               `json:"usage"`
	ToolsUsed    []string                           `json:"tools_used"`
	ToolCalls    []analyticsagent.ToolCallRecord    `json:"tool_calls"`
	Subagents    []analyticsagent.SubagentRunRecord `json:"subagents"`
	Notices      []string                           `json:"notices,omitempty"`
	SessionID    string                             `json:"session_id,omitempty"`
	SessionTitle string                             `json:"session_title,omitempty"`
	SessionUsage *chatstore.Usage                   `json:"session_usage,omitempty"`
}

type assistantStreamErrorFrame struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func newAssistantNDJSONStream(w http.ResponseWriter) *assistantNDJSONStream {
	return &assistantNDJSONStream{w: w, controller: http.NewResponseController(w)}
}

func (s *assistantNDJSONStream) write(frame any) error {
	if !s.committed {
		s.w.Header().Set("Content-Type", "application/x-ndjson")
		s.w.Header().Set("Cache-Control", "no-store")
		s.w.Header().Set("X-Content-Type-Options", "nosniff")
		s.w.WriteHeader(http.StatusOK)
		s.committed = true
	}
	if err := json.NewEncoder(s.w).Encode(frame); err != nil {
		return err
	}
	return s.controller.Flush()
}

func (s *assistantNDJSONStream) start(model string) error {
	if s.started {
		return nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = analyticsagent.MiniMaxM3Model
	}
	s.started = true
	return s.write(assistantStreamProgressFrame{Type: "start", Model: model})
}

func (s *assistantNDJSONStream) forward(event analyticsagent.StreamEvent) error {
	switch event.Type {
	case analyticsagent.StreamEventStart:
		return s.start(event.Model)
	case analyticsagent.StreamEventRoundStart:
		if event.Round <= 0 {
			return errors.New("invalid assistant round-start stream event")
		}
		if err := s.start(event.Model); err != nil {
			return err
		}
		return s.write(assistantStreamProgressFrame{
			Type: event.Type, Agent: event.Agent, Round: event.Round, ParentCallID: event.ParentCallID,
		})
	case analyticsagent.StreamEventContentDelta:
		if event.Delta == "" {
			return nil
		}
		if err := s.start(event.Model); err != nil {
			return err
		}
		return s.write(assistantStreamProgressFrame{Type: event.Type, Delta: event.Delta})
	case analyticsagent.StreamEventContentReset:
		if err := s.start(event.Model); err != nil {
			return err
		}
		return s.write(assistantStreamProgressFrame{Type: event.Type})
	case analyticsagent.StreamEventToolStart:
		if strings.TrimSpace(event.CallID) == "" || strings.TrimSpace(event.Name) == "" {
			return errors.New("invalid assistant tool-start stream event")
		}
		if err := s.start(event.Model); err != nil {
			return err
		}
		return s.write(assistantStreamProgressFrame{
			Type: event.Type, Agent: event.Agent, ParentCallID: event.ParentCallID, Round: event.Round,
			CallID: event.CallID, Name: event.Name, Arguments: safeFrameJSON(event.Arguments),
		})
	case analyticsagent.StreamEventToolFinish:
		if strings.TrimSpace(event.CallID) == "" || strings.TrimSpace(event.Name) == "" || event.OK == nil {
			return errors.New("invalid assistant tool-finish stream event")
		}
		if err := s.start(event.Model); err != nil {
			return err
		}
		return s.write(assistantStreamProgressFrame{
			Type: event.Type, Agent: event.Agent, ParentCallID: event.ParentCallID, Round: event.Round,
			CallID: event.CallID, Name: event.Name, OK: event.OK,
			Result: safeFrameJSON(event.Result), DurationMS: event.DurationMS,
		})
	case analyticsagent.StreamEventSubagentStart, analyticsagent.StreamEventSubagentFinish:
		if strings.TrimSpace(event.CallID) == "" || event.Subagent == nil || strings.TrimSpace(string(event.Subagent.Agent)) == "" {
			return errors.New("invalid assistant subagent stream event")
		}
		if event.Type == analyticsagent.StreamEventSubagentFinish && event.OK == nil {
			return errors.New("invalid assistant subagent stream event")
		}
		if err := s.start(event.Model); err != nil {
			return err
		}
		return s.write(assistantStreamProgressFrame{
			Type: event.Type, Agent: event.Agent, CallID: event.CallID, OK: event.OK,
			DurationMS: event.DurationMS, Subagent: subagentFrame(event.Subagent),
		})
	default:
		return errors.New("invalid assistant stream event")
	}
}

func subagentFrame(value *analyticsagent.SubagentEvent) *assistantSubagentInfo {
	if value == nil {
		return nil
	}
	frame := &assistantSubagentInfo{
		Agent: value.Agent, Title: value.Title, Task: value.Task, Status: value.Status,
		Report: value.Report, Rounds: value.Rounds, ToolsUsed: value.ToolsUsed, Error: value.Error,
	}
	if value.Usage != nil {
		usage := *value.Usage
		frame.Usage = &usage
	}
	return frame
}

// safeFrameJSON keeps NDJSON frames well-formed even if an upstream event
// carries malformed raw JSON: invalid payloads are dropped, never emitted.
func safeFrameJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}
	return value
}

func assistantServiceError(err error) (int, string) {
	switch {
	case errors.Is(err, analyticsagent.ErrInvalidChat):
		return http.StatusBadRequest, "assistant messages are invalid"
	case errors.Is(err, analyticsagent.ErrRateLimited):
		return http.StatusTooManyRequests, "MiniMax usage is temporarily limited; try again later"
	case errors.Is(err, analyticsagent.ErrAuthentication), errors.Is(err, analyticsagent.ErrModelUnavailable), errors.Is(err, analyticsagent.ErrUnavailable):
		return http.StatusServiceUnavailable, "MiniMax M3 assistant is unavailable"
	case errors.Is(err, analyticsagent.ErrBusy):
		return http.StatusTooManyRequests, "assistant is busy; try again shortly"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "assistant request timed out"
	case errors.Is(err, analyticsagent.ErrProviderFailure), errors.Is(err, analyticsagent.ErrProvider), errors.Is(err, analyticsagent.ErrLoopLimit):
		return http.StatusBadGateway, "MiniMax could not complete the analytics report"
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, "assistant request was canceled"
	default:
		return http.StatusInternalServerError, "assistant request failed"
	}
}

func (h *Handlers) logAssistantFailure(endpointMode string, committed bool, err error) {
	if h == nil || h.logger == nil {
		return
	}
	errorClass, operation, statusCode, providerCode := assistantFailureMetadata(err)
	h.logger.Error("analytics assistant failure",
		"endpoint_mode", endpointMode,
		"committed", committed,
		"error_class", errorClass,
		"operation", operation,
		"provider_http_status", statusCode,
		"provider_code", providerCode,
	)
}

func assistantFailureMetadata(err error) (errorClass, operation string, statusCode int, providerCode int64) {
	switch {
	case errors.Is(err, analyticsagent.ErrRateLimited):
		errorClass = "rate_limited"
	case errors.Is(err, analyticsagent.ErrAuthentication):
		errorClass = "authentication"
	case errors.Is(err, analyticsagent.ErrModelUnavailable):
		errorClass = "model_unavailable"
	case errors.Is(err, analyticsagent.ErrProvider):
		errorClass = "provider"
	case errors.Is(err, analyticsagent.ErrInvalidChat):
		errorClass = "invalid_chat"
	case errors.Is(err, analyticsagent.ErrUnavailable):
		errorClass = "unavailable"
	case errors.Is(err, analyticsagent.ErrBusy):
		errorClass = "busy"
	case errors.Is(err, context.DeadlineExceeded):
		errorClass = "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		errorClass = "canceled"
	case errors.Is(err, analyticsagent.ErrLoopLimit):
		errorClass = "loop_limit"
	case errors.Is(err, analyticsagent.ErrProviderFailure):
		errorClass = "provider_failure"
	default:
		errorClass = "internal"
	}

	var rateLimit *analyticsagent.RateLimitError
	var authentication *analyticsagent.AuthenticationError
	var provider *analyticsagent.ProviderError
	switch {
	case errors.As(err, &rateLimit):
		operation = rateLimit.Operation
		statusCode = rateLimit.StatusCode
		providerCode = rateLimit.ProviderCode
	case errors.As(err, &authentication):
		operation = authentication.Operation
		statusCode = authentication.StatusCode
		providerCode = authentication.ProviderCode
	case errors.As(err, &provider):
		operation = provider.Operation
		statusCode = provider.StatusCode
		providerCode = provider.ProviderCode
	}
	operation = safeAssistantProviderOperation(operation)
	return errorClass, operation, statusCode, providerCode
}

func safeAssistantProviderOperation(operation string) string {
	switch operation {
	case "list models", "build list models request", "read list models response", "decode model catalog",
		"chat completion", "build chat completion request", "read chat completion response", "decode chat completion", "decode assistant message", "validate chat completion",
		"stream chat completion", "build stream chat completion request", "read stream chat completion response", "decode stream chat completion", "validate stream chat completion",
		"decode stream assistant delta", "decode stream tool calls", "decode stream tool call", "decode stream tool function", "validate stream tool calls",
		"decode stream reasoning details", "validate stream reasoning details", "encode stream reasoning details",
		"encode stream tool calls", "encode stream tool call", "encode stream tool function", "encode stream assistant message":
		return operation
	default:
		return "unknown"
	}
}

func writeAssistantServiceError(w http.ResponseWriter, err error) {
	status, message := assistantServiceError(err)
	writeAssistantError(w, status, message)
}

func rejectNonlocalAssistantOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Modern browsers supply Sec-Fetch-Site even on requests (such as GET)
		// where Origin may be omitted. Command-line/local native clients omit both.
		if site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); site != "" && site != "same-origin" {
			writeAssistantError(w, http.StatusForbidden, "assistant requests are allowed only from the local dashboard")
			return true
		}
		return false
	}
	if !isAllowedAssistantOrigin(origin, r) {
		writeAssistantError(w, http.StatusForbidden, "assistant requests are allowed only from the local dashboard")
		return true
	}
	return false
}

func isAllowedAssistantOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil || !isLocalOrigin(origin) || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if parsed.Scheme != scheme {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}

	// The checked-in Vite development server is the one explicit cross-port
	// exception. Production assets are same-origin with the Go server.
	requestURL := &url.URL{Scheme: scheme, Host: r.Host}
	return parsed.Port() == "7451" && requestURL.Port() == strconv.Itoa(DefaultPort) &&
		isLoopbackHostname(parsed.Hostname()) && isLoopbackHostname(requestURL.Hostname())
}

func isLoopbackHostname(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func writeAssistantError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	APIError{Error: http.StatusText(status), Message: message, Code: status}.Write(w)
}
