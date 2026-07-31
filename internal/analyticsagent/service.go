package analyticsagent

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	defaultMaxRounds           = 6
	defaultMaxToolCalls        = 16
	defaultMaxToolOutputBytes  = 384 << 10
	defaultMaxConcurrentChats  = 2
	maxConcurrentChatsLimit    = 8
	defaultRunTimeout          = 90 * time.Second
	maxRunTimeout              = 5 * time.Minute
	maxBrowserMessages         = 40
	maxBrowserMessageBytes     = 16 << 10
	maxBrowserHistoryBytes     = 56 << 10
	maxProviderAssistantBytes  = 256 << 10
	maxProviderToolCallIDBytes = 256
	maxProviderToolNameBytes   = 128
	maxProviderToolArgsBytes   = 32 << 10
	// A returned answer must itself be valid as a browser-history message on
	// the next stateless turn.
	maxFinalResponseBytes = maxBrowserMessageBytes
	statusAvailableTTL    = time.Minute
	statusUnavailableTTL  = 10 * time.Second
)

// PrivacyConsentVersion is returned by status and must be echoed by chat
// requests after the browser has shown and accepted that version's disclosure.
const PrivacyConsentVersion = "analytics-assistant-v5"

var (
	ErrUnavailable     = errors.New("analytics assistant unavailable")
	ErrBusy            = errors.New("analytics assistant busy")
	ErrProviderFailure = errors.New("analytics assistant provider failure")
	ErrLoopLimit       = errors.New("analytics assistant loop limit reached")
	ErrInvalidChat     = errors.New("invalid analytics assistant chat")
)

// ContextValidationError identifies an invalid browser navigation context.
// Detail is intentionally limited to server-authored, non-reflective text so
// the local HTTP layer can return an actionable message without echoing input.
type ContextValidationError struct {
	Detail string
}

func (e *ContextValidationError) Error() string {
	return ErrInvalidChat.Error() + ": context " + e.Detail
}

func (e *ContextValidationError) Unwrap() error { return ErrInvalidChat }

func invalidBrowserContext(detail string) error {
	return &ContextValidationError{Detail: detail}
}

// Client is the narrow provider boundary used by Service. MiniMaxClient
// implements it, while tests can script complete responses without network I/O.
type Client interface {
	EnsureAvailable(context.Context) error
	Chat(context.Context, ChatRequest) (*ChatResponse, error)
}

// StreamingClient is implemented by provider clients that can expose generated
// assistant content as it arrives. Service falls back to Client.Chat for test
// doubles and alternative providers, while MiniMax uses the streaming path in
// production.
type StreamingClient interface {
	ChatStream(context.Context, ChatRequest, func(string) error) (*ChatResponse, error)
}

type ServiceOptions struct {
	Client             Client
	Registry           *source.Registry
	CacheIntegrity     CacheIntegrityProvider
	RunTimeout         time.Duration
	MaxRounds          int
	MaxToolCalls       int
	MaxToolOutputBytes int
	// MaxConcurrentChats bounds simultaneous agent runs across all browser
	// tabs. It exists to protect the provider quota and the local sources, not
	// to serialize the UI.
	MaxConcurrentChats int
	// HistoryKey, when 32 bytes, is used to sign assistant history messages.
	// Supplying a persisted key keeps saved conversations replayable across
	// restarts; otherwise a process-local random key is generated.
	HistoryKey []byte
}

type Service struct {
	client             Client
	tools              *ToolRegistry
	runTimeout         time.Duration
	maxRounds          int
	maxToolCalls       int
	maxToolOutputBytes int
	sem                chan struct{}
	historyKey         []byte
	statusMu           sync.Mutex
	cachedStatus       Status
	statusExpires      time.Time
}

type Status struct {
	Available      bool     `json:"available"`
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	Reason         string   `json:"reason,omitempty"`
	PrivacyNotice  string   `json:"privacy_notice"`
	ConsentVersion string   `json:"consent_version"`
	Capabilities   []string `json:"capabilities"`
	// Specialists describes the delegable agents so the browser can label
	// delegated work without hardcoding the roster.
	Specialists []SpecialistInfo `json:"specialists"`
	// SessionsPersisted is set by the web layer when a durable chat store is
	// attached, so the browser can offer saved-conversation history.
	SessionsPersisted bool `json:"sessions_persisted"`
}

type BrowserMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Signature string `json:"signature,omitempty"`
}

type BrowserContext struct {
	Route    string `json:"route,omitempty"`
	Source   string `json:"source,omitempty"`
	Period   string `json:"period,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type ChatInput struct {
	Messages       []BrowserMessage `json:"messages"`
	Context        *BrowserContext  `json:"context,omitempty"`
	ConsentVersion string           `json:"consent_version"`
	// SessionID optionally names the persisted chat session this turn belongs
	// to. The service validates only its shape; storage semantics belong to
	// the web layer.
	SessionID string `json:"session_id,omitempty"`
}

// ChatResult is the canonical outcome of one question: the signed answer plus
// the complete, privacy-safe record of how it was produced.
type ChatResult struct {
	Message   BrowserMessage `json:"message"`
	Model     string         `json:"model"`
	Provider  string         `json:"provider"`
	Agent     AgentID        `json:"agent"`
	Rounds    int            `json:"rounds"`
	Usage     Usage          `json:"usage"`
	ToolsUsed []string       `json:"tools_used"`
	// ToolCalls holds every analytics tool invocation, including the ones a
	// specialist made; Subagents holds each delegated investigation.
	ToolCalls  []ToolCallRecord    `json:"tool_calls"`
	Subagents  []SubagentRunRecord `json:"subagents"`
	DurationMS int64               `json:"duration_ms"`
	// Notices are deterministic disclosures the backend appended to the answer.
	Notices []string `json:"notices,omitempty"`
}

const privacyNotice = "Questions, the assistant conversation included with them, and aggregate usage metrics used to answer them are sent to MiniMax. The current dashboard route, source, selected preset or custom date range, browser timezone, aggregate UTC day/hour bucket labels, aggregate source diagnostics, request-accounting evidence, sanitized cache health, model/provider/tool names, and project names without their directories may also be sent. Raw source diagnostics and errors, coding transcripts, coding prompts or reasoning, session titles, raw configuration, credentials, file paths, raw event or per-session activity timestamps, raw request/session identifiers, and coding-tool input/output are never exposed by analytics tools."

const crossSourceCostNotice = "Cost scope: source costs are not additive. OpenCode reports recorded spend, Claude Code may mix reported and computed values, and Codex/Kimi Code/Qwen Code values are API-equivalent estimates rather than subscription, membership, coding-plan, or token-plan spend."

const truncationNotice = "Evidence scope: at least one analytics result or specialist finding was truncated to its configured limit. Treat omitted rows, earlier buckets, or report text as unknown unless the answer explicitly states that fuller evidence was fetched."

var assistantCapabilities = []string{
	"cross-source usage reports",
	"usage trends",
	"model analytics",
	"tool analytics",
	"privacy-safe project analytics",
	"privacy-safe session analytics",
	"per-model, per-tool, and per-project trends",
	"delegated specialist investigations",
	"source integrity audits",
}

func NewService(opts ServiceOptions) *Service {
	timeout := opts.RunTimeout
	if timeout <= 0 {
		timeout = defaultRunTimeout
	} else if timeout > maxRunTimeout {
		timeout = maxRunTimeout
	}
	maxRounds := opts.MaxRounds
	if maxRounds <= 0 || maxRounds > defaultMaxRounds {
		maxRounds = defaultMaxRounds
	}
	maxToolCalls := opts.MaxToolCalls
	if maxToolCalls <= 0 || maxToolCalls > defaultMaxToolCalls {
		maxToolCalls = defaultMaxToolCalls
	}
	maxToolOutput := opts.MaxToolOutputBytes
	if maxToolOutput <= 0 || maxToolOutput > defaultMaxToolOutputBytes {
		maxToolOutput = defaultMaxToolOutputBytes
	}
	concurrency := opts.MaxConcurrentChats
	if concurrency <= 0 {
		concurrency = defaultMaxConcurrentChats
	} else if concurrency > maxConcurrentChatsLimit {
		concurrency = maxConcurrentChatsLimit
	}
	historyKey := append([]byte(nil), opts.HistoryKey...)
	if len(historyKey) != 32 {
		historyKey = make([]byte, 32)
		if _, err := rand.Read(historyKey); err != nil {
			historyKey = nil
		}
	}
	return &Service{
		client:             opts.Client,
		tools:              NewToolRegistryWithCache(opts.Registry, opts.CacheIntegrity),
		runTimeout:         timeout,
		maxRounds:          maxRounds,
		maxToolCalls:       maxToolCalls,
		maxToolOutputBytes: maxToolOutput,
		sem:                make(chan struct{}, concurrency),
		historyKey:         historyKey,
	}
}

func (s *Service) Status(ctx context.Context) Status {
	status := BaseStatus()
	if s == nil || s.client == nil || len(s.historyKey) == 0 {
		status.Reason = "MiniMax M3 is not configured"
		return status
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if time.Now().Before(s.statusExpires) {
		return cloneStatus(s.cachedStatus)
	}
	if err := s.client.EnsureAvailable(ctx); err != nil {
		status.Reason = publicAvailabilityReason(err)
		s.cachedStatus = cloneStatus(status)
		s.statusExpires = time.Now().Add(statusUnavailableTTL)
		return status
	}
	status.Available = true
	s.cachedStatus = cloneStatus(status)
	s.statusExpires = time.Now().Add(statusAvailableTTL)
	return status
}

// BaseStatus supplies the stable web contract without performing I/O.
func BaseStatus() Status {
	return Status{
		Provider:       ProviderMiniMax,
		Model:          MiniMaxM3Model,
		PrivacyNotice:  privacyNotice,
		ConsentVersion: PrivacyConsentVersion,
		Capabilities:   append([]string(nil), assistantCapabilities...),
		Specialists:    Specialists(),
	}
}

// ProviderMiniMax names the only provider this assistant talks to.
const ProviderMiniMax = "minimax"

func publicAvailabilityReason(err error) string {
	switch {
	case errors.Is(err, ErrModelUnavailable):
		return "MiniMax M3 is not available for this account"
	case errors.Is(err, ErrAuthentication):
		return "MiniMax authentication is unavailable or was rejected"
	case errors.Is(err, ErrRateLimited):
		return "MiniMax availability is temporarily rate limited"
	default:
		return "MiniMax M3 availability could not be verified"
	}
}

func (s *Service) Chat(ctx context.Context, input ChatInput) (ChatResult, error) {
	return s.runChat(ctx, input, nil, false)
}

// ChatStream runs the same bounded, signed agent loop as Chat while reporting
// visible assistant deltas, tool lifecycle, and specialist progress. The
// returned ChatResult remains the canonical, signed result callers persist.
func (s *Service) ChatStream(ctx context.Context, input ChatInput, emit func(StreamEvent) error) (ChatResult, error) {
	if emit == nil {
		return ChatResult{}, errors.New("analytics assistant stream emitter is required")
	}
	return s.runChat(ctx, input, emit, true)
}

func (s *Service) runChat(ctx context.Context, input ChatInput, emit func(StreamEvent) error, stream bool) (ChatResult, error) {
	if s == nil || s.client == nil || len(s.historyKey) == 0 {
		return ChatResult{}, ErrUnavailable
	}
	normalized, err := NormalizeChatInput(input)
	if err != nil {
		return ChatResult{}, err
	}
	input = normalized
	if err := s.verifyBrowserHistory(input.Messages); err != nil {
		return ChatResult{}, err
	}

	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		return ChatResult{}, ErrBusy
	}

	runCtx, cancel := context.WithTimeout(ctx, s.runTimeout)
	defer cancel()
	if err := s.client.EnsureAvailable(runCtx); err != nil {
		return ChatResult{}, mapProviderError(err)
	}

	turn := &turnRunner{
		service: s,
		stream:  stream,
		sink:    &eventSink{emit: emit},
		budget:  newRunBudget(s.maxToolCalls, s.maxToolOutputBytes),
	}
	lead, err := s.newLeadRun(turn, input)
	if err != nil {
		return ChatResult{}, fmt.Errorf("%w: %v", ErrInvalidChat, err)
	}
	if stream {
		if err := turn.sink.send(StreamEvent{Type: StreamEventStart, Agent: AgentLead, Model: MiniMaxM3Model}); err != nil {
			return ChatResult{}, err
		}
	}

	startedAt := time.Now()
	outcome, err := lead.execute(runCtx)
	if err != nil {
		if runCtx.Err() != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return ChatResult{}, runCtx.Err()
		}
		return ChatResult{}, err
	}

	content := outcome.content
	notices := turn.deterministicNotices()
	suffix := noticeSuffix(notices)
	content += suffix
	if stream && suffix != "" {
		if err := turn.sink.send(StreamEvent{Type: StreamEventContentDelta, Agent: AgentLead, Delta: suffix}); err != nil {
			return ChatResult{}, err
		}
	}

	toolsUsed, toolCalls, subagents := turn.results()
	return ChatResult{
		Message:    BrowserMessage{Role: "assistant", Content: content, Signature: s.signAssistantMessage(content)},
		Model:      MiniMaxM3Model,
		Provider:   ProviderMiniMax,
		Agent:      AgentLead,
		Rounds:     outcome.rounds,
		Usage:      turn.budget.totalUsage(),
		ToolsUsed:  toolsUsed,
		ToolCalls:  toolCalls,
		Subagents:  subagents,
		DurationMS: time.Since(startedAt).Milliseconds(),
		Notices:    notices,
	}, nil
}

// newLeadRun builds the user-visible agent loop from the browser conversation.
func (s *Service) newLeadRun(turn *turnRunner, input ChatInput) (*agentRun, error) {
	definition, found := agentByID(AgentLead)
	if !found {
		return nil, errors.New("the lead analytics agent is not defined")
	}
	messages, err := leadMessages(definition, input)
	if err != nil {
		return nil, err
	}
	definitions := append(s.tools.DefinitionsFor(definition.Tools), delegateToolDefinition())
	return &agentRun{
		turn:         turn,
		agent:        definition,
		messages:     messages,
		definitions:  definitions,
		maxRounds:    s.maxRounds,
		maxToolCalls: s.maxToolCalls,
		visible:      true,
		seen:         make(map[string]string),
		toolsUsed:    make([]string, 0),
	}, nil
}

// newSpecialistRun builds a delegated investigation. The specialist sees only
// its own task: never the conversation, the user's wording, or another
// specialist's work.
func (s *Service) newSpecialistRun(turn *turnRunner, definition *agentDefinition, task, parentCallID string) (*agentRun, error) {
	system, err := makeTextMessage("system", definition.systemPrompt()+currentDateNote())
	if err != nil {
		return nil, err
	}
	prompt, err := makeTextMessage("user", "<investigation_task>\n"+task+"\n</investigation_task>")
	if err != nil {
		return nil, err
	}
	maxRounds := definition.MaxRounds
	if maxRounds <= 0 || maxRounds > s.maxRounds {
		maxRounds = s.maxRounds
	}
	maxToolCalls := definition.MaxToolCalls
	if maxToolCalls <= 0 || maxToolCalls > s.maxToolCalls {
		maxToolCalls = s.maxToolCalls
	}
	return &agentRun{
		turn:         turn,
		agent:        definition,
		messages:     []json.RawMessage{system, prompt},
		definitions:  s.tools.DefinitionsFor(definition.Tools),
		maxRounds:    maxRounds,
		maxToolCalls: maxToolCalls,
		parentCallID: parentCallID,
		seen:         make(map[string]string),
		toolsUsed:    make([]string, 0),
	}, nil
}

// NormalizeChatInput validates the public request and returns a canonical copy.
// In particular, it converts the legacy "YYYY-MM-DD to YYYY-MM-DD|now"
// navigation hint into the same period/from/to shape accepted by analytics
// tools. Callers must use the returned value; the input is not mutated.
func NormalizeChatInput(input ChatInput) (ChatInput, error) {
	if input.ConsentVersion != PrivacyConsentVersion {
		return ChatInput{}, fmt.Errorf("%w: privacy consent version is missing or stale", ErrInvalidChat)
	}
	if input.SessionID != "" && !isSafeChatSessionID(input.SessionID) {
		return ChatInput{}, fmt.Errorf("%w: session id is invalid", ErrInvalidChat)
	}
	if input.Context != nil {
		normalized, err := normalizeBrowserContextAt(*input.Context, time.Now().UTC())
		if err != nil {
			return ChatInput{}, err
		}
		input.Context = &normalized
	}
	if err := validateBrowserMessages(input.Messages); err != nil {
		return ChatInput{}, err
	}
	return input, nil
}

// ValidateChatInput preserves the validation-only API for callers that do not
// need the canonical copy.
func ValidateChatInput(input ChatInput) error {
	_, err := NormalizeChatInput(input)
	return err
}

func validateBrowserMessages(messages []BrowserMessage) error {
	if len(messages) == 0 {
		return fmt.Errorf("%w: messages are required", ErrInvalidChat)
	}
	if len(messages) > maxBrowserMessages {
		return fmt.Errorf("%w: at most %d messages are allowed", ErrInvalidChat, maxBrowserMessages)
	}
	totalBytes := 0
	for i, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			return fmt.Errorf("%w: message %d has an unsupported role", ErrInvalidChat, i)
		}
		if i > 0 && message.Role == messages[i-1].Role {
			return fmt.Errorf("%w: messages must alternate user and assistant roles", ErrInvalidChat)
		}
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("%w: message %d has empty content", ErrInvalidChat, i)
		}
		if message.Role == "user" && message.Signature != "" {
			return fmt.Errorf("%w: user message %d must not be signed", ErrInvalidChat, i)
		}
		if message.Role == "assistant" && message.Signature == "" {
			return fmt.Errorf("%w: assistant message %d is not signed", ErrInvalidChat, i)
		}
		if len(message.Signature) > 128 {
			return fmt.Errorf("%w: message %d signature is too long", ErrInvalidChat, i)
		}
		if len(message.Content) > maxBrowserMessageBytes {
			return fmt.Errorf("%w: message %d is too long", ErrInvalidChat, i)
		}
		totalBytes += len(message.Content)
		if totalBytes > maxBrowserHistoryBytes {
			return fmt.Errorf("%w: message history is too long", ErrInvalidChat)
		}
	}
	if messages[0].Role != "user" {
		return fmt.Errorf("%w: the first message must be from the user", ErrInvalidChat)
	}
	if messages[len(messages)-1].Role != "user" {
		return fmt.Errorf("%w: the last message must be from the user", ErrInvalidChat)
	}
	return nil
}

// currentDateNote grounds relative questions. The provider model cannot know
// the current date and otherwise guesses absolute from/to ranges from its
// training cutoff, producing confidently empty reports.
func currentDateNote() string {
	return "\nToday's date (UTC) is " + time.Now().UTC().Format("2006-01-02") +
		". Analytics accepts only these exact period presets: " + strings.Join(stats.SupportedPeriodPresets(), ", ") +
		". Hour presets are rolling UTC windows; day presets are UTC calendar-aligned. " +
		"Before every tool call choose exactly one time mode: PRESET uses {\"period\":\"7d\"} with no from/to; CUSTOM uses {\"from\":\"2026-07-01\",\"to\":\"2026-07-31\"} (or from alone) with no period; DEFAULT omits all three and uses " + stats.DefaultPeriodPreset + ". " +
		"Final key check: first select one mode. For CUSTOM remove period and verify from exists. For PRESET remove from and to and verify period exists. Do not apply both corrections. " +
		"Browser timezone is only a navigation/display hint and never changes analytics query bounds.\n"
}

func leadMessages(definition *agentDefinition, input ChatInput) ([]json.RawMessage, error) {
	system, err := makeTextMessage("system", definition.systemPrompt()+currentDateNote())
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(input.Messages)+1)
	out = append(out, system)
	contextNote := ""
	if input.Context != nil {
		contextJSON, err := json.Marshal(input.Context)
		if err != nil {
			return nil, err
		}
		contextNote = "\n\n<untrusted_navigation_context>" + string(contextJSON) + "</untrusted_navigation_context>"
	}
	for index, message := range input.Messages {
		content := message.Content
		if index == len(input.Messages)-1 && contextNote != "" {
			// Navigation hints have user/data authority, never system authority.
			// Their fields are allowlist-validated before reaching this point. Put
			// them before the question so an explicit range in the question is the
			// final and most salient time intent.
			content = contextNote + "\n\n" + content
		}
		raw, err := makeTextMessage(message.Role, content)
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}

func normalizeBrowserContextAt(value BrowserContext, now time.Time) (BrowserContext, error) {
	for name, field := range map[string]string{
		"route": value.Route, "source": value.Source, "period": value.Period,
		"from": value.From, "to": value.To, "timezone": value.Timezone,
	} {
		if len(field) > 256 {
			return BrowserContext{}, invalidBrowserContext(name + " is too long")
		}
	}
	if value.Route != "" {
		switch value.Route {
		case "/overview", "/daily", "/models", "/tools", "/projects", "/sessions", "/config":
		default:
			return BrowserContext{}, invalidBrowserContext("route is invalid")
		}
	}
	if value.Source != "" && !isSafeSourceID(value.Source) {
		return BrowserContext{}, invalidBrowserContext("source is invalid")
	}
	if value.Timezone != "" && !isSafeTimezone(value.Timezone) {
		return BrowserContext{}, invalidBrowserContext("timezone is invalid")
	}

	value.Period = strings.TrimSpace(value.Period)
	value.From = strings.TrimSpace(value.From)
	value.To = strings.TrimSpace(value.To)
	// Compatibility bridge for browser clients from the release immediately
	// before structured ranges. Analytics tools themselves never accept this
	// display label; remove the bridge after one compatibility release.
	if legacyFrom, legacyTo, ok := parseLegacyBrowserPeriod(value.Period); ok {
		if value.From != "" || value.To != "" {
			return BrowserContext{}, invalidBrowserContext("must use either period or from/to, not both")
		}
		value.Period = ""
		value.From = legacyFrom
		value.To = legacyTo
	}

	pq, err := validatePeriodAt(periodArgs{Period: value.Period, From: value.From, To: value.To}, now.UTC())
	if err != nil {
		return BrowserContext{}, invalidBrowserContext("time selection is invalid: " + err.Error())
	}
	value.Period = pq.Period
	value.From = pq.From
	value.To = pq.To
	return value, nil
}

func parseLegacyBrowserPeriod(value string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(value), " to ")
	if len(parts) != 2 {
		return "", "", false
	}
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
		return "", "", false
	}
	if parts[1] == "now" {
		return parts[0], "", true
	}
	if _, err := time.Parse("2006-01-02", parts[1]); err != nil {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// isSafeChatSessionID bounds persisted-chat session ids to a URL- and
// JSON-safe shape. Existence checks belong to the storage layer.
func isSafeChatSessionID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func isSafeTimezone(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("/_+-", char) {
			continue
		}
		return false
	}
	return true
}

func (s *Service) signAssistantMessage(content string) string {
	mac := hmac.New(sha256.New, s.historyKey)
	_, _ = mac.Write([]byte("assistant-history-v1\x00"))
	_, _ = mac.Write([]byte(content))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) verifyBrowserHistory(messages []BrowserMessage) error {
	for i, message := range messages {
		if message.Role != "assistant" {
			continue
		}
		expected := s.signAssistantMessage(message.Content)
		if !hmac.Equal([]byte(expected), []byte(message.Signature)) {
			return fmt.Errorf("%w: assistant message %d signature is invalid", ErrInvalidChat, i)
		}
	}
	return nil
}

func cloneStatus(value Status) Status {
	value.Capabilities = append([]string(nil), value.Capabilities...)
	value.Specialists = append([]SpecialistInfo(nil), value.Specialists...)
	return value
}

func makeTextMessage(role, content string) (json.RawMessage, error) {
	encoded, err := json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: role, Content: content})
	return json.RawMessage(encoded), err
}

func makeToolMessage(callID, name string, result json.RawMessage) (json.RawMessage, error) {
	encoded, err := json.Marshal(struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Name       string `json:"name"`
		Content    string `json:"content"`
	}{Role: "tool", ToolCallID: callID, Name: name, Content: string(result)})
	return json.RawMessage(encoded), err
}

func mapProviderError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrProviderFailure) || errors.Is(err, ErrLoopLimit) || errors.Is(err, ErrInvalidChat) {
		return err
	}
	if errors.Is(err, ErrModelUnavailable) || errors.Is(err, ErrAuthentication) {
		return fmt.Errorf("%w: MiniMax M3 is unavailable: %w", ErrUnavailable, err)
	}
	return fmt.Errorf("%w: MiniMax request failed: %w", ErrProviderFailure, err)
}

func toolResultOK(result json.RawMessage) bool {
	var envelope struct {
		OK bool `json:"ok"`
	}
	return json.Unmarshal(result, &envelope) == nil && envelope.OK
}

func toolCallFingerprint(name, arguments string) string {
	var value any
	if err := json.Unmarshal([]byte(arguments), &value); err == nil {
		if canonical, err := json.Marshal(value); err == nil {
			arguments = string(canonical)
		}
	}
	return strings.TrimSpace(name) + "\n" + strings.TrimSpace(arguments)
}

func isCostBearingTool(name string) bool {
	switch name {
	case "get_overview", "get_daily_usage", "get_usage_trend_by_dimension",
		"get_session_usage", "get_model_usage", "get_project_usage":
		return true
	default:
		return false
	}
}

func sourceFromToolArguments(arguments string) string {
	var value struct {
		Source string `json:"source"`
	}
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return ""
	}
	value.Source = strings.TrimSpace(value.Source)
	if !isSafeSourceID(value.Source) {
		return ""
	}
	return value.Source
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
