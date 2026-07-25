package analyticsagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

const (
	// maxDelegationsPerTurn bounds how many specialists one question may start.
	maxDelegationsPerTurn = 3
	minDelegatedTaskBytes = 24
	maxDelegatedTaskBytes = 1200
	// maxSubagentReportBytes bounds a specialist finding before it is handed
	// back to the lead agent as tool evidence.
	maxSubagentReportBytes = 6 << 10
	// maxConcurrentToolCalls bounds parallel evidence gathering. Analytics tools
	// are read-only, so a round's calls are independent by construction.
	maxConcurrentToolCalls = 4
	// maxProviderAttempts includes the first attempt: one bounded retry.
	maxProviderAttempts      = 2
	providerRetryBaseDelay   = 400 * time.Millisecond
	providerRetryTimeReserve = 6 * time.Second
)

// budgetNotice is appended before the last available round so the model stops
// asking for evidence it can no longer receive and writes the report instead.
const budgetNotice = "Evidence budget reached: no further analytics tools or specialists are available for this answer. Write the best report you can from the evidence already gathered, and state explicitly what could not be verified."

// runBudget is the allowance shared by the lead agent and every specialist it
// starts. Bounding the turn as a whole is what keeps delegation from
// multiplying provider cost and source load.
type runBudget struct {
	mu          sync.Mutex
	toolCalls   int
	outputBytes int
	delegations int
	usage       Usage
}

func newRunBudget(toolCalls, outputBytes int) *runBudget {
	return &runBudget{toolCalls: toolCalls, outputBytes: outputBytes, delegations: maxDelegationsPerTurn}
}

// takeToolCall reserves one tool invocation, reporting whether the turn may
// still gather evidence.
func (b *runBudget) takeToolCall() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.toolCalls <= 0 {
		return false
	}
	b.toolCalls--
	return true
}

func (b *runBudget) takeDelegation() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.delegations <= 0 {
		return false
	}
	b.delegations--
	return true
}

// takeOutput reserves tool-result bytes. A refused reservation ends evidence
// gathering rather than truncating a result into something misleading.
func (b *runBudget) takeOutput(size int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size > b.outputBytes {
		return false
	}
	b.outputBytes -= size
	return true
}

func (b *runBudget) exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.toolCalls <= 0 || b.outputBytes <= 0
}

func (b *runBudget) addUsage(usage Usage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usage = b.usage.Add(usage)
}

func (b *runBudget) totalUsage() Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usage
}

// eventSink serializes stream events across the lead agent and any specialists
// running concurrently, and makes a transport failure sticky so one broken
// browser connection stops the whole turn instead of every goroutine retrying.
type eventSink struct {
	mu     sync.Mutex
	emit   func(StreamEvent) error
	failed error
}

func (s *eventSink) send(event StreamEvent) error {
	if s == nil || s.emit == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		return s.failed
	}
	if err := s.emit(event); err != nil {
		s.failed = err
		return err
	}
	return nil
}

// failure reports the transport error that stopped the stream, if any.
func (s *eventSink) failure() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

// turnRunner owns everything that belongs to one user question: the shared
// budget, the canonical records the browser and chat store receive, and the
// cost-scope tracking that decides whether the answer needs the cross-source
// notice.
type turnRunner struct {
	service *Service
	stream  bool
	sink    *eventSink
	budget  *runBudget

	mu              sync.Mutex
	callSeq         int
	toolCalls       []ToolCallRecord
	subagents       []SubagentRunRecord
	toolsUsed       []string
	costSources     map[string]struct{}
	crossSourceCost bool
}

func (t *turnRunner) nextCallID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callSeq++
	return fmt.Sprintf("tool-%d", t.callSeq)
}

func (t *turnRunner) recordTool(record ToolCallRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.toolCalls = append(t.toolCalls, record)
	t.toolsUsed = appendUnique(t.toolsUsed, record.Name)
}

// noteToolName records that a tool was invoked even when the invocation itself
// is reported as something other than a tool call, such as a delegation.
func (t *turnRunner) noteToolName(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.toolsUsed = appendUnique(t.toolsUsed, name)
}

func (t *turnRunner) recordSubagent(record SubagentRunRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.subagents = append(t.subagents, record)
}

// noteCostScope tracks whether the answer mixes cost from more than one source,
// which the service discloses even when the model's prose omits it.
func (t *turnRunner) noteCostScope(name, arguments string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if name == "get_cross_source_overview" {
		t.crossSourceCost = true
		return
	}
	if !isCostBearingTool(name) {
		return
	}
	sourceID := sourceFromToolArguments(arguments)
	if sourceID == "" {
		return
	}
	if t.costSources == nil {
		t.costSources = make(map[string]struct{})
	}
	t.costSources[sourceID] = struct{}{}
	if len(t.costSources) > 1 {
		t.crossSourceCost = true
	}
}

func (t *turnRunner) needsCrossSourceNotice() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.crossSourceCost
}

func (t *turnRunner) results() ([]string, []ToolCallRecord, []SubagentRunRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	toolsUsed := t.toolsUsed
	if toolsUsed == nil {
		toolsUsed = make([]string, 0)
	}
	toolCalls := t.toolCalls
	if toolCalls == nil {
		toolCalls = make([]ToolCallRecord, 0)
	}
	subagents := t.subagents
	if subagents == nil {
		subagents = make([]SubagentRunRecord, 0)
	}
	return toolsUsed, toolCalls, subagents
}

// agentRun is one agent's bounded provider loop. The lead agent's run is the
// user-visible one; a specialist's run is identical except that it publishes
// tool progress instead of prose and cannot delegate.
type agentRun struct {
	turn         *turnRunner
	agent        *agentDefinition
	messages     []json.RawMessage
	definitions  []ToolDefinition
	maxRounds    int
	maxToolCalls int
	// parentCallID is the delegation this run answers, empty for the lead.
	parentCallID string
	// visible marks the run whose prose is streamed to the browser.
	visible bool

	seen          map[string]string
	toolCallCount int
	rounds        int
	toolsUsed     []string
	usage         Usage
	noticeSent    bool
}

// agentOutcome is one agent loop's final result: the prose it produced and what
// it cost to produce.
type agentOutcome struct {
	content   string
	status    string
	rounds    int
	toolsUsed []string
	usage     Usage
}

func (r *agentRun) execute(ctx context.Context) (agentOutcome, error) {
	for round := 1; round <= r.maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return agentOutcome{}, err
		}
		r.rounds = round

		// The last permitted round, and any round after the shared budget is
		// spent, is offered no tools at all: the model can only answer.
		final := round == r.maxRounds || r.turn.budget.exhausted() || r.toolCallCount >= r.maxToolCalls
		if final {
			r.appendBudgetNotice()
		}
		if err := r.emit(StreamEvent{Type: StreamEventRoundStart, Agent: r.agent.ID, Round: round, ParentCallID: r.parentCallID}); err != nil {
			return agentOutcome{}, err
		}

		response, visible, err := r.callModel(ctx, final)
		if err != nil {
			return agentOutcome{}, err
		}
		// Replay the complete raw assistant object. Reconstructing only content
		// and tool calls can discard provider-specific reasoning/signature
		// fields that MiniMax requires on the next turn.
		r.messages = append(r.messages, cloneRaw(response.AssistantMessage))

		if len(response.ToolCalls) == 0 {
			content, err := r.finalContent(response, visible)
			if err != nil {
				return agentOutcome{}, err
			}
			status := SubagentStatusComplete
			if final && round < r.maxRounds {
				status = SubagentStatusExhausted
			}
			return agentOutcome{content: content, status: status, rounds: round, toolsUsed: r.toolsUsed, usage: r.usage}, nil
		}
		if response.FinishReason != "tool_calls" {
			return agentOutcome{}, fmt.Errorf("%w: provider returned tool calls without a tool_calls finish reason", ErrProviderFailure)
		}
		if final {
			// Tools were not offered this round, so a tool call is a provider
			// protocol violation rather than a recoverable model mistake.
			return agentOutcome{}, fmt.Errorf("%w: provider requested tools after the evidence budget was closed", ErrProviderFailure)
		}
		if err := visible.finishRound(response.Content); err != nil {
			return agentOutcome{}, err
		}

		toolMessages, err := r.runToolCalls(ctx, response.ToolCalls, round)
		if err != nil {
			return agentOutcome{}, err
		}
		r.messages = append(r.messages, toolMessages...)
	}
	// Unreachable: the last round is always tool-free and therefore terminal.
	return agentOutcome{}, fmt.Errorf("%w: more than %d model rounds", ErrLoopLimit, r.maxRounds)
}

// appendBudgetNotice tells the model, exactly once, that it must answer from the
// evidence it already has.
func (r *agentRun) appendBudgetNotice() {
	if r.noticeSent {
		return
	}
	r.noticeSent = true
	if notice, err := makeTextMessage("system", budgetNotice); err == nil {
		r.messages = append(r.messages, notice)
	}
}

// callModel performs one provider round with a bounded retry for transient
// failures. The returned round stream carries whatever visible prose was
// published so the caller can reconcile it with the provider's final content.
func (r *agentRun) callModel(ctx context.Context, withoutTools bool) (*ChatResponse, *roundStream, error) {
	tools := r.definitions
	if withoutTools {
		tools = nil
	}
	deadline, hasDeadline := ctx.Deadline()

	var lastErr error
	for attempt := 1; attempt <= maxProviderAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		visible := &roundStream{run: r, content: newVisibleContentStream()}
		response, err := r.invokeProvider(ctx, tools, visible)
		if err == nil {
			// A completed round is always one provider request, even when the
			// provider reported no token counters for it.
			usage := response.Usage
			if usage.Requests == 0 {
				usage.Requests = 1
			}
			r.usage = r.usage.Add(usage)
			r.turn.budget.addUsage(usage)
			return response, visible, nil
		}
		lastErr = err

		var transport *streamTransportError
		if errors.As(err, &transport) {
			// The browser connection failed; nothing is worth retrying.
			return nil, nil, transport.cause
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		retryable := attempt < maxProviderAttempts && isRetryableProviderError(err)
		if retryable && hasDeadline && time.Until(deadline) < providerRetryTimeReserve {
			retryable = false
		}
		if !retryable {
			if resetErr := visible.reset(); resetErr != nil {
				return nil, nil, resetErr
			}
			return nil, nil, mapProviderError(err)
		}
		// Discard the partial answer before trying again so the browser never
		// shows two interleaved attempts.
		if resetErr := visible.reset(); resetErr != nil {
			return nil, nil, resetErr
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(providerRetryDelay(attempt)):
		}
	}
	return nil, nil, mapProviderError(lastErr)
}

// invokeProvider runs a single provider request and validates the shape the
// loop depends on.
func (r *agentRun) invokeProvider(ctx context.Context, tools []ToolDefinition, visible *roundStream) (*ChatResponse, error) {
	request := ChatRequest{Messages: r.messages, Tools: tools}

	var response *ChatResponse
	var err error
	if r.turn.stream {
		if streamingClient, ok := r.turn.service.client.(StreamingClient); ok {
			response, err = streamingClient.ChatStream(ctx, request, visible.push)
		} else {
			response, err = r.turn.service.client.Chat(ctx, request)
			if err == nil && response != nil {
				err = visible.push(response.Content)
			}
		}
	} else {
		response, err = r.turn.service.client.Chat(ctx, request)
	}
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.AssistantMessage) == 0 || !json.Valid(response.AssistantMessage) {
		return nil, fmt.Errorf("%w: provider returned no replayable assistant message", ErrProviderFailure)
	}
	if len(response.AssistantMessage) > maxProviderAssistantBytes {
		return nil, fmt.Errorf("%w: provider assistant message is too large", ErrProviderFailure)
	}
	return response, nil
}

func providerRetryDelay(attempt int) time.Duration {
	delay := providerRetryBaseDelay << (attempt - 1)
	// Full jitter keeps concurrent specialists from retrying in lockstep.
	return delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
}

// isRetryableProviderError limits retries to failures that a second attempt can
// plausibly survive: rate limiting, provider-side errors, and transport faults.
func isRetryableProviderError(err error) bool {
	var rateLimit *RateLimitError
	if errors.As(err, &rateLimit) {
		return true
	}
	var provider *ProviderError
	if errors.As(err, &provider) {
		return provider.StatusCode >= 500 || (provider.StatusCode == 0 && provider.Cause != nil)
	}
	return false
}

// finalContent turns a terminal provider response into the agent's answer,
// reconciling it with whatever was already streamed.
func (r *agentRun) finalContent(response *ChatResponse, visible *roundStream) (string, error) {
	if response.FinishReason != "stop" {
		if err := visible.reset(); err != nil {
			return "", err
		}
		if response.FinishReason == "length" {
			return "", fmt.Errorf("%w: the report exceeded the provider's output limit", ErrProviderFailure)
		}
		return "", fmt.Errorf("%w: provider did not return a complete report", ErrProviderFailure)
	}
	content, err := stripLeadingThinkBlocks(response.Content)
	if err != nil {
		if resetErr := visible.reset(); resetErr != nil {
			return "", resetErr
		}
		return "", fmt.Errorf("%w: unsafe reasoning envelope", ErrProviderFailure)
	}
	if content == "" {
		if resetErr := visible.reset(); resetErr != nil {
			return "", resetErr
		}
		return "", fmt.Errorf("%w: provider returned an empty final response", ErrProviderFailure)
	}
	if r.visible && len(content) > maxFinalResponseBytes {
		if resetErr := visible.reset(); resetErr != nil {
			return "", resetErr
		}
		return "", fmt.Errorf("%w: provider final response is too large", ErrProviderFailure)
	}
	if err := visible.flush(response.Content); err != nil {
		return "", err
	}
	return content, nil
}

func (r *agentRun) emit(event StreamEvent) error {
	if !r.turn.stream {
		return nil
	}
	return r.turn.sink.send(event)
}

// plannedCall is one provider tool call after validation and budgeting, either
// executable or already resolved to a safe rejection envelope.
type plannedCall struct {
	providerID   string
	streamCallID string
	name         string
	displayName  string
	arguments    json.RawMessage
	rawArguments string
	rejection    json.RawMessage
	// specialist and task are set only for an accepted delegation, so the
	// announcement and the execution can never disagree about what will run.
	specialist *agentDefinition
	task       string
	result     json.RawMessage
	ok         bool
	durationMS int64
	// subagent holds the delegated run's record until the round records it in
	// the order the model asked for, rather than the order work finished.
	subagent *SubagentRunRecord
}

func (c *plannedCall) delegation() bool { return c.specialist != nil }

// runToolCalls validates, budgets, executes, and records every tool call in one
// round, returning the provider tool messages in the order the model asked for
// them. Recoverable mistakes become tool results the model can react to; only
// protocol violations and transport failures end the run.
func (r *agentRun) runToolCalls(ctx context.Context, calls []ToolCall, round int) ([]json.RawMessage, error) {
	planned := make([]*plannedCall, 0, len(calls))
	for _, call := range calls {
		item, err := r.planCall(call)
		if err != nil {
			return nil, err
		}
		planned = append(planned, item)
	}

	executable := make([]*plannedCall, 0, len(planned))
	for _, item := range planned {
		if item.rejection != nil {
			item.result = item.rejection
			item.ok = false
			continue
		}
		executable = append(executable, item)
	}

	// Announce every call before any of them runs so progress reads as one
	// round of work rather than a race between finished tools.
	for _, item := range planned {
		if err := r.announceCall(item); err != nil {
			return nil, err
		}
	}
	if err := r.executeCalls(ctx, executable, round); err != nil {
		return nil, err
	}

	// Records are written in the order the model asked for the calls, so a
	// persisted or restored conversation never depends on which query finished
	// first.
	messages := make([]json.RawMessage, 0, len(planned))
	for _, item := range planned {
		if item.rejection != nil {
			if err := r.publishFinish(item, round); err != nil {
				return nil, err
			}
		}
		if item.subagent != nil {
			r.turn.recordSubagent(*item.subagent)
		} else {
			r.recordCall(item, round)
		}
		message, err := makeToolMessage(item.providerID, item.name, item.result)
		if err != nil {
			return nil, fmt.Errorf("%w: encode tool result", ErrProviderFailure)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// planCall validates one provider tool call. Structural violations are fatal;
// everything the model could plausibly correct becomes a rejection envelope.
func (r *agentRun) planCall(call ToolCall) (*plannedCall, error) {
	if call.Type != "function" || strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
		return nil, fmt.Errorf("%w: provider returned an invalid tool call", ErrProviderFailure)
	}
	if len(call.ID) > maxProviderToolCallIDBytes || len(call.Function.Name) > maxProviderToolNameBytes || len(call.Function.Arguments) > maxProviderToolArgsBytes {
		return nil, fmt.Errorf("%w: provider tool call is too large", ErrProviderFailure)
	}

	name := call.Function.Name
	item := &plannedCall{
		providerID:   call.ID,
		streamCallID: r.turn.nextCallID(),
		name:         name,
		displayName:  name,
		arguments:    safeToolArguments(call.Function.Arguments),
		rawArguments: call.Function.Arguments,
	}

	if !r.allows(name) {
		// The name is provider-supplied and outside the allowlist, so it is
		// never echoed to the browser or the records.
		item.displayName = "unavailable_tool"
		item.arguments = json.RawMessage(`{}`)
		item.rejection = toolErrorEnvelope("unknown_tool", "That tool is not available to this agent. Use only the tools in your tool list.")
		return item, nil
	}

	fingerprint := toolCallFingerprint(name, call.Function.Arguments)
	if previous, exists := r.seen[fingerprint]; exists {
		item.rejection = toolErrorEnvelope("duplicate_call",
			fmt.Sprintf("This exact call already ran in this investigation as %s. Reuse that result or change the arguments.", previous))
		return item, nil
	}
	r.seen[fingerprint] = item.streamCallID

	if r.toolCallCount >= r.maxToolCalls {
		item.rejection = toolErrorEnvelope("agent_budget_exhausted", "This agent has no tool calls left. Answer from the evidence already gathered.")
		return item, nil
	}
	if !r.turn.budget.takeToolCall() {
		item.rejection = toolErrorEnvelope("budget_exhausted", "The evidence budget for this question is spent. Answer from the evidence already gathered and state what is unverified.")
		return item, nil
	}
	r.toolCallCount++
	r.toolsUsed = appendUnique(r.toolsUsed, name)
	r.turn.noteToolName(name)

	if name == delegateToolName {
		r.planDelegation(item)
		return item, nil
	}
	r.turn.noteCostScope(name, call.Function.Arguments)
	return item, nil
}

// planDelegation resolves a delegation completely before anything is announced,
// so a rejected delegation is reported as an ordinary failed tool call and an
// accepted one is always announced as the specialist that will actually run.
func (r *agentRun) planDelegation(item *plannedCall) {
	agentID, task, err := parseDelegation(item.rawArguments)
	if err != nil {
		item.rejection = toolErrorEnvelope("invalid_arguments", err.Error())
		return
	}
	definition, found := agentByID(agentID)
	if !found || !isSpecialistAgent(agentID) {
		item.rejection = toolErrorEnvelope("unknown_specialist", "That specialist does not exist. Choose one from the tool schema.")
		return
	}
	if !r.turn.budget.takeDelegation() {
		item.rejection = toolErrorEnvelope("delegation_budget_exhausted",
			fmt.Sprintf("At most %d specialists may run for one question. Finish the analysis yourself.", maxDelegationsPerTurn))
		return
	}
	item.specialist = definition
	item.task = task
}

func (r *agentRun) allows(name string) bool {
	if name == delegateToolName {
		return r.agent.ID == AgentLead
	}
	for _, allowed := range r.agent.Tools {
		if allowed == name {
			return isAnalyticsToolName(name)
		}
	}
	return false
}

// announceCall publishes the start of one call. Delegations are announced as
// specialist runs so the browser can nest their tool activity.
func (r *agentRun) announceCall(item *plannedCall) error {
	if item.delegation() {
		return r.emit(StreamEvent{
			Type: StreamEventSubagentStart, Agent: r.agent.ID, CallID: item.streamCallID,
			Subagent: &SubagentEvent{Agent: item.specialist.ID, Title: item.specialist.Title, Task: item.task},
		})
	}
	return r.emit(StreamEvent{
		Type: StreamEventToolStart, Agent: r.agent.ID, ParentCallID: r.parentCallID, Round: r.rounds,
		CallID: item.streamCallID, Name: item.displayName, Arguments: cloneRaw(item.arguments),
	})
}

// recordCall stores the canonical record of one completed call.
func (r *agentRun) recordCall(item *plannedCall, round int) {
	r.turn.recordTool(ToolCallRecord{
		CallID: item.streamCallID, Name: item.displayName, Agent: r.agent.ID,
		ParentCallID: r.parentCallID, Round: round,
		Arguments: cloneRaw(item.arguments), Result: cloneRaw(item.result),
		OK: item.ok, DurationMS: item.durationMS,
	})
}

// publishFinish reports one call's outcome to the browser as soon as it is
// known, which is deliberately not the order records are written in.
func (r *agentRun) publishFinish(item *plannedCall, round int) error {
	ok := item.ok
	return r.emit(StreamEvent{
		Type: StreamEventToolFinish, Agent: r.agent.ID, ParentCallID: r.parentCallID, Round: round,
		CallID: item.streamCallID, Name: item.displayName, OK: &ok,
		Result: cloneRaw(item.result), DurationMS: item.durationMS,
	})
}

// executeCalls runs the round's executable calls concurrently. Analytics tools
// are read-only aggregate queries, so parallel evidence gathering is safe and
// keeps a multi-tool round close to the latency of its slowest query.
func (r *agentRun) executeCalls(ctx context.Context, calls []*plannedCall, round int) error {
	if len(calls) == 0 {
		return nil
	}
	if len(calls) == 1 {
		return r.executeCall(ctx, calls[0], round)
	}

	limit := min(len(calls), maxConcurrentToolCalls)
	var wait sync.WaitGroup
	slots := make(chan struct{}, limit)
	errs := make([]error, len(calls))
	for index, item := range calls {
		wait.Add(1)
		go func() {
			defer wait.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			errs[index] = r.executeCall(ctx, item, round)
		}()
	}
	wait.Wait()
	return errors.Join(errs...)
}

func (r *agentRun) executeCall(ctx context.Context, item *plannedCall, round int) error {
	startedAt := time.Now()
	if item.delegation() {
		result, record, err := r.delegate(ctx, item)
		if err != nil {
			return err
		}
		item.durationMS = time.Since(startedAt).Milliseconds()
		item.result = result
		item.ok = toolResultOK(result)
		record.DurationMS = item.durationMS
		if record.Agent != "" {
			item.subagent = &record
		}
		usage := record.Usage
		ok := item.ok
		return r.emit(StreamEvent{
			Type: StreamEventSubagentFinish, Agent: r.agent.ID, CallID: item.streamCallID, OK: &ok,
			DurationMS: item.durationMS,
			Subagent: &SubagentEvent{
				Agent: record.Agent, Title: record.Title, Task: record.Task, Status: record.Status,
				Report: record.Report, Rounds: record.Rounds, ToolsUsed: record.ToolsUsed,
				Usage: &usage, Error: record.Error,
			},
		})
	}

	result := r.turn.service.tools.Execute(ctx, item.name, json.RawMessage(item.rawArguments))
	item.durationMS = time.Since(startedAt).Milliseconds()
	if len(result) == 0 {
		result = toolErrorEnvelope("tool_failed", "The analytics tool failed safely.")
	}
	if !r.turn.budget.takeOutput(len(result)) {
		result = toolErrorEnvelope("result_too_large", "The result did not fit in the remaining evidence budget. Request a narrower period or a smaller limit.")
	}
	item.result = result
	item.ok = toolResultOK(result)
	return r.publishFinish(item, round)
}

// delegate runs one specialist investigation and converts it into tool evidence
// for the lead agent. Only cancellation and browser-transport failures end the
// turn; a specialist that fails on its own returns a failure the lead agent can
// report or work around.
func (r *agentRun) delegate(ctx context.Context, item *plannedCall) (json.RawMessage, SubagentRunRecord, error) {
	record := SubagentRunRecord{
		CallID: item.streamCallID, Agent: item.specialist.ID, Title: item.specialist.Title,
		Task: item.task, Status: SubagentStatusFailed, ToolsUsed: make([]string, 0),
	}
	child, err := r.turn.service.newSpecialistRun(r.turn, item.specialist, item.task, item.streamCallID)
	if err != nil {
		record.Error = "The specialist could not be started."
		return toolErrorEnvelope("tool_failed", record.Error), record, nil
	}

	outcome, err := child.execute(ctx)
	record.Rounds = child.rounds
	record.Usage = child.usage
	if len(child.toolsUsed) > 0 {
		record.ToolsUsed = child.toolsUsed
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, record, err
		}
		if failure := r.turn.sink.failure(); failure != nil {
			return nil, record, failure
		}
		record.Error = specialistFailureReason(err)
		return toolErrorEnvelope("specialist_failed", record.Error), record, nil
	}

	report, truncated := boundReport(outcome.content)
	record.Report = report
	record.Status = outcome.status
	record.Rounds = outcome.rounds
	record.Usage = outcome.usage
	record.Error = ""
	if len(outcome.toolsUsed) > 0 {
		record.ToolsUsed = outcome.toolsUsed
	}

	envelope := marshalEnvelope(true, struct {
		Agent     AgentID  `json:"agent"`
		Status    string   `json:"status"`
		Report    string   `json:"report"`
		Rounds    int      `json:"rounds"`
		ToolsUsed []string `json:"tools_used"`
		Truncated bool     `json:"report_truncated,omitempty"`
	}{
		Agent: record.Agent, Status: record.Status, Report: report,
		Rounds: record.Rounds, ToolsUsed: record.ToolsUsed, Truncated: truncated,
	}, nil)
	return envelope, record, nil
}

// specialistFailureReason keeps provider detail out of the lead agent's context
// while still telling it what kind of failure to work around.
func specialistFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrLoopLimit):
		return "The specialist reached its investigation limit before concluding."
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrRateLimited):
		return "The specialist could not reach the model provider."
	default:
		return "The specialist failed before producing a finding."
	}
}

func boundReport(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if len(content) <= maxSubagentReportBytes {
		return content, false
	}
	trimmed := content[:maxSubagentReportBytes]
	// Never split a UTF-8 sequence when cutting a report short.
	for len(trimmed) > 0 && !isUTF8Boundary(content, len(trimmed)) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return strings.TrimSpace(trimmed) + "\n\n[finding truncated]", true
}

func isUTF8Boundary(value string, index int) bool {
	return index >= len(value) || value[index]&0xC0 != 0x80
}

type delegationArgs struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

func parseDelegation(arguments string) (AgentID, string, error) {
	var args delegationArgs
	if err := decodeStrict(json.RawMessage(arguments), &args); err != nil {
		return "", "", errors.New("delegation arguments must be a JSON object with agent and task")
	}
	task := strings.TrimSpace(args.Task)
	if len(task) < minDelegatedTaskBytes {
		return "", "", fmt.Errorf("task must describe a complete investigation in at least %d characters", minDelegatedTaskBytes)
	}
	if len(task) > maxDelegatedTaskBytes {
		return "", "", fmt.Errorf("task must be at most %d characters", maxDelegatedTaskBytes)
	}
	return AgentID(strings.TrimSpace(args.Agent)), task, nil
}

func toolErrorEnvelope(code, message string) json.RawMessage {
	return marshalEnvelope(false, nil, &safeToolError{Code: code, Message: message})
}

// roundStream withholds reasoning envelopes and partial tag prefixes until the
// text is known to be user-visible answer prose, and remembers what it has
// published so a failed round can be visibly discarded.
type roundStream struct {
	run       *agentRun
	content   *visibleContentStream
	published bool
	discarded bool
}

// streamTransportError marks a browser-transport failure so the retry path can
// tell it apart from a provider failure worth retrying.
type streamTransportError struct{ cause error }

func (e *streamTransportError) Error() string {
	return "assistant stream transport failed: " + e.cause.Error()
}

func (e *streamTransportError) Unwrap() error { return e.cause }

func transportFailure(err error) error { return &streamTransportError{cause: err} }

func (s *roundStream) push(delta string) error {
	if s == nil {
		return nil
	}
	chunk, err := s.content.Push(delta)
	if err != nil {
		return fmt.Errorf("%w: unsafe reasoning envelope", ErrProviderFailure)
	}
	return s.publish(chunk)
}

func (s *roundStream) publish(chunk string) error {
	if chunk == "" || !s.run.visible || !s.run.turn.stream {
		return nil
	}
	if err := s.run.emit(StreamEvent{Type: StreamEventContentDelta, Agent: s.run.agent.ID, Delta: chunk}); err != nil {
		return transportFailure(err)
	}
	s.published = true
	return nil
}

// finishRound reconciles a round that ended in tool calls: any prose the model
// emitted before deciding to call tools is withdrawn from the browser.
func (s *roundStream) finishRound(authoritative string) error {
	if _, err := s.content.Finish(authoritative); err != nil {
		if resetErr := s.reset(); resetErr != nil {
			return resetErr
		}
		return fmt.Errorf("%w: unsafe reasoning envelope", ErrProviderFailure)
	}
	return s.reset()
}

// flush publishes the remainder of the provider's authoritative final content.
func (s *roundStream) flush(authoritative string) error {
	if !s.run.turn.stream || !s.run.visible {
		return nil
	}
	remaining, err := s.content.Finish(authoritative)
	if err != nil {
		if resetErr := s.reset(); resetErr != nil {
			return resetErr
		}
		return fmt.Errorf("%w: unsafe reasoning envelope", ErrProviderFailure)
	}
	return s.publish(remaining)
}

// reset withdraws everything published for this round from the browser.
func (s *roundStream) reset() error {
	if s == nil || !s.run.turn.stream || !s.run.visible || s.discarded || !s.published {
		return nil
	}
	s.discarded = true
	if err := s.run.emit(StreamEvent{Type: StreamEventContentReset, Agent: s.run.agent.ID}); err != nil {
		return transportFailure(err)
	}
	return nil
}
