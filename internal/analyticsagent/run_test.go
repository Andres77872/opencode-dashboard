package analyticsagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
)

// routedAgentClient scripts one response queue per agent role. Delegated runs
// interleave with the lead's rounds, so responses cannot be indexed by call
// order the way a single-agent script can.
type routedAgentClient struct {
	mu       sync.Mutex
	scripts  map[AgentID][]*ChatResponse
	requests map[AgentID][]ChatRequest
	failures map[AgentID][]error
	delay    time.Duration
	maxLive  int
	live     int
}

func newRoutedClient() *routedAgentClient {
	return &routedAgentClient{
		scripts:  map[AgentID][]*ChatResponse{},
		requests: map[AgentID][]ChatRequest{},
		failures: map[AgentID][]error{},
	}
}

func (c *routedAgentClient) script(agent AgentID, responses ...*ChatResponse) *routedAgentClient {
	c.scripts[agent] = append(c.scripts[agent], responses...)
	return c
}

func (c *routedAgentClient) fail(agent AgentID, errs ...error) *routedAgentClient {
	c.failures[agent] = append(c.failures[agent], errs...)
	return c
}

func (c *routedAgentClient) EnsureAvailable(context.Context) error { return nil }

// agentOf identifies the caller from the system prompt the run was built with.
func agentOf(request ChatRequest) AgentID {
	if len(request.Messages) == 0 {
		return AgentLead
	}
	system := string(request.Messages[0])
	for id, definition := range agentRoster {
		if id == AgentLead {
			continue
		}
		if strings.Contains(system, strings.SplitN(definition.Focus, "\n", 2)[0]) {
			return id
		}
	}
	return AgentLead
}

func (c *routedAgentClient) Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	agent := agentOf(request)
	c.mu.Lock()
	copied := ChatRequest{Tools: append([]ToolDefinition(nil), request.Tools...)}
	for _, message := range request.Messages {
		copied.Messages = append(copied.Messages, cloneRaw(message))
	}
	c.requests[agent] = append(c.requests[agent], copied)
	index := len(c.requests[agent]) - 1
	c.live++
	if c.live > c.maxLive {
		c.maxLive = c.live
	}
	var response *ChatResponse
	if index < len(c.scripts[agent]) {
		response = c.scripts[agent][index]
	}
	var failure error
	if index < len(c.failures[agent]) {
		failure = c.failures[agent][index]
	}
	delay := c.delay
	c.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			c.finish()
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	c.finish()
	if failure != nil {
		return nil, failure
	}
	if response == nil {
		return nil, errors.New("no scripted response for " + string(agent))
	}
	return response, nil
}

func (c *routedAgentClient) finish() {
	c.mu.Lock()
	c.live--
	c.mu.Unlock()
}

func (c *routedAgentClient) requestsFor(agent AgentID) []ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[agent]
}

func usageResponse(t *testing.T, finishReason, content string, calls []ToolCall, usage Usage) *ChatResponse {
	t.Helper()
	response := assistantResponse(t, finishReason, content, calls, nil)
	response.Usage = usage
	return response
}

func delegationCall(id string, agent AgentID, task string) ToolCall {
	arguments, _ := json.Marshal(map[string]string{"agent": string(agent), "task": task})
	return functionToolCall(id, delegateToolName, string(arguments))
}

func TestParseUsageNormalizesProviderCounters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want Usage
	}{
		{name: "absent", raw: "", want: Usage{}},
		{name: "null", raw: "null", want: Usage{}},
		{name: "malformed", raw: `"nonsense"`, want: Usage{}},
		{
			name: "openai shape",
			raw:  `{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":100},"completion_tokens_details":{"reasoning_tokens":12}}`,
			want: Usage{Requests: 1, InputTokens: 120, OutputTokens: 30, CachedInputTokens: 100, ReasoningTokens: 12, TotalTokens: 150},
		},
		{
			name: "derives a missing total",
			raw:  `{"prompt_tokens":10,"completion_tokens":5}`,
			want: Usage{Requests: 1, InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		},
		{
			name: "input/output naming",
			raw:  `{"input_tokens":7,"output_tokens":3,"total_tokens":10}`,
			want: Usage{Requests: 1, InputTokens: 7, OutputTokens: 3, TotalTokens: 10},
		},
		{
			name: "impossible subsets are bounded and negatives dropped",
			raw:  `{"prompt_tokens":10,"completion_tokens":-4,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":99}}`,
			want: Usage{Requests: 1, InputTokens: 10, OutputTokens: 0, CachedInputTokens: 10, TotalTokens: 10},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseUsage(json.RawMessage(test.raw)); got != test.want {
				t.Fatalf("parseUsage(%s) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}

func TestServiceAggregatesUsageAcrossRounds(t *testing.T) {
	client := &scriptedAgentClient{responses: []*ChatResponse{
		usageResponse(t, "tool_calls", "", []ToolCall{functionToolCall("call-1", "list_sources", `{}`)},
			Usage{Requests: 1, InputTokens: 900, OutputTokens: 40, CachedInputTokens: 800, TotalTokens: 940}),
		usageResponse(t, "stop", "Report.", nil,
			Usage{Requests: 1, InputTokens: 1200, OutputTokens: 260, ReasoningTokens: 60, TotalTokens: 1460}),
	}}
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Report."))
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{Requests: 2, InputTokens: 2100, OutputTokens: 300, CachedInputTokens: 800, ReasoningTokens: 60, TotalTokens: 2400}
	if result.Usage != want {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
	if result.DurationMS < 0 || result.Rounds != 2 {
		t.Fatalf("result timing = %#v", result)
	}
}

func TestServiceCountsRoundsEvenWithoutProviderTokenCounters(t *testing.T) {
	client := &scriptedAgentClient{responses: []*ChatResponse{
		usageResponse(t, "stop", "Report.", nil, Usage{Requests: 1}),
	}}
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})
	result, err := service.Chat(context.Background(), oneUserMessage("Report."))
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.Requests != 1 || result.Usage.HasTokens() {
		t.Fatalf("usage = %#v, want one request and no invented tokens", result.Usage)
	}
}

func TestServiceRecoversFromInvalidAnalyticsArguments(t *testing.T) {
	client := &scriptedAgentClient{responses: []*ChatResponse{
		assistantResponse(t, "tool_calls", "", []ToolCall{
			functionToolCall("call-invalid", "get_overview", `{"source":"opencode","period":"90d"}`),
		}, nil),
		assistantResponse(t, "tool_calls", "", []ToolCall{
			functionToolCall("call-corrected", "get_overview", `{"source":"opencode","period":"30d"}`),
		}, nil),
		assistantResponse(t, "stop", "Corrected report.", nil, nil),
	}}
	registry := source.NewRegistry(source.SourceOpenCode)
	if err := registry.Register(newAnalyticsTestSource(source.SourceOpenCode, 2)); err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceOptions{Client: client, Registry: registry})
	result, err := service.Chat(context.Background(), oneUserMessage("Report the last 30 days."))
	if err != nil {
		t.Fatal(err)
	}
	if result.Message.Content != "Corrected report." || len(result.ToolCalls) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.ToolCalls[0].OK || !strings.Contains(string(result.ToolCalls[0].Result), "for custom dates omit period and use from/to") {
		t.Fatalf("invalid call result = %s", result.ToolCalls[0].Result)
	}
	if !result.ToolCalls[1].OK || !strings.Contains(string(result.ToolCalls[1].Arguments), `"period":"30d"`) {
		t.Fatalf("corrected call = %#v", result.ToolCalls[1])
	}
	if len(client.requests) != 3 || !strings.Contains(string(client.requests[1].Messages[len(client.requests[1].Messages)-1]), "invalid_arguments") {
		t.Fatalf("model did not receive actionable rejection: %#v", client.requests)
	}
}

func TestServiceRecoversFromConflictingTimeModes(t *testing.T) {
	tests := []struct {
		name          string
		question      string
		rejected      string
		corrected     string
		wantError     string
		wantCorrected []string
		wantOmitted   []string
	}{
		{
			name:      "keep custom range and remove period",
			question:  "Report July 1 through July 15, 2020.",
			rejected:  `{"source":"opencode","period":"7d","from":"2020-07-01","to":"2020-07-15"}`,
			corrected: `{"source":"opencode","from":"2020-07-01","to":"2020-07-15"}`,
			wantError: "CUSTOM mode keeps required from plus optional to and removes period",
			wantCorrected: []string{
				`"from":"2020-07-01"`, `"to":"2020-07-15"`,
			},
			wantOmitted: []string{`"period"`},
		},
		{
			name:          "keep preset and remove custom keys",
			question:      "Report the 7d preset.",
			rejected:      `{"source":"opencode","period":"7d","from":"2020-07-01"}`,
			corrected:     `{"source":"opencode","period":"7d"}`,
			wantError:     "PRESET mode keeps period and removes from/to",
			wantCorrected: []string{`"period":"7d"`},
			wantOmitted:   []string{`"from"`, `"to"`},
		},
		{
			name:      "custom correction adds from when only to was proposed",
			question:  "Report July 1 through July 15, 2020.",
			rejected:  `{"source":"opencode","period":"7d","to":"2020-07-15"}`,
			corrected: `{"source":"opencode","from":"2020-07-01","to":"2020-07-15"}`,
			wantError: "CUSTOM mode removes period and must add the required from date",
			wantCorrected: []string{
				`"from":"2020-07-01"`, `"to":"2020-07-15"`,
			},
			wantOmitted: []string{`"period"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &scriptedAgentClient{responses: []*ChatResponse{
				assistantResponse(t, "tool_calls", "", []ToolCall{
					functionToolCall("call-invalid", "get_overview", test.rejected),
				}, nil),
				assistantResponse(t, "tool_calls", "", []ToolCall{
					functionToolCall("call-corrected", "get_overview", test.corrected),
				}, nil),
				assistantResponse(t, "stop", "Corrected report.", nil, nil),
			}}
			registry := source.NewRegistry(source.SourceOpenCode)
			if err := registry.Register(newAnalyticsTestSource(source.SourceOpenCode, 2)); err != nil {
				t.Fatal(err)
			}
			service := NewService(ServiceOptions{Client: client, Registry: registry})
			result, err := service.Chat(context.Background(), oneUserMessage(test.question))
			if err != nil {
				t.Fatal(err)
			}
			if result.Message.Content != "Corrected report." || len(result.ToolCalls) != 2 {
				t.Fatalf("result = %#v", result)
			}
			if result.ToolCalls[0].OK || string(result.ToolCalls[0].Arguments) != `{}` {
				t.Fatalf("rejected call was not safely recorded: %#v", result.ToolCalls[0])
			}
			if !strings.Contains(string(result.ToolCalls[0].Result), test.wantError) {
				t.Fatalf("rejection is not actionable: %s", result.ToolCalls[0].Result)
			}
			if !result.ToolCalls[1].OK {
				t.Fatalf("corrected call failed: %#v", result.ToolCalls[1])
			}
			arguments := string(result.ToolCalls[1].Arguments)
			for _, want := range test.wantCorrected {
				if !strings.Contains(arguments, want) {
					t.Errorf("corrected arguments %s lack %s", arguments, want)
				}
			}
			for _, forbidden := range test.wantOmitted {
				if strings.Contains(arguments, forbidden) {
					t.Errorf("corrected arguments %s contain %s", arguments, forbidden)
				}
			}
			if len(client.requests) != 3 || !strings.Contains(string(client.requests[1].Messages[len(client.requests[1].Messages)-1]), test.wantError) {
				t.Fatalf("model did not receive the mode-preserving correction: %#v", client.requests)
			}
		})
	}
}

func TestRejectedAllowedToolArgumentsAreFullyRedacted(t *testing.T) {
	const secret = "/private/project/credential.txt"
	client := &scriptedAgentClient{responses: []*ChatResponse{
		assistantResponse(t, "tool_calls", "", []ToolCall{
			functionToolCall("bad", "get_overview", `{"source":"opencode","period":"90d","path":"`+secret+`"}`),
		}, nil),
		assistantResponse(t, "stop", "The invalid proposal was rejected.", nil, nil),
	}}
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})
	result, err := service.Chat(context.Background(), oneUserMessage("Report."))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || string(result.ToolCalls[0].Arguments) != `{}` {
		t.Fatalf("rejected arguments were not redacted: %#v", result.ToolCalls)
	}
	if strings.Contains(string(result.ToolCalls[0].Result), secret) {
		t.Fatalf("rejection leaked provider-controlled content: %s", result.ToolCalls[0].Result)
	}
}

func TestServiceDelegatesToASpecialistAndRecordsItsWork(t *testing.T) {
	const task = "Determine which model drove the opencode token increase over the last 7 days."
	client := newRoutedClient()
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", []ToolCall{delegationCall("call-1", AgentTrend, task)}, nil),
		usageResponse(t, "stop", "Model X drove the increase.", nil, Usage{Requests: 1, InputTokens: 500, OutputTokens: 50, TotalTokens: 550}),
	)
	client.script(AgentTrend,
		assistantResponse(t, "tool_calls", "", []ToolCall{functionToolCall("sub-1", "get_daily_usage", `{"source":"opencode","period":"7d"}`)}, nil),
		usageResponse(t, "stop", "Tokens rose 40% on model X.", nil, Usage{Requests: 1, InputTokens: 300, OutputTokens: 30, TotalTokens: 330}),
	)
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	var events []StreamEvent
	result, err := service.ChatStream(context.Background(), oneUserMessage("Why did tokens grow?"), func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Subagents) != 1 {
		t.Fatalf("subagent records = %#v", result.Subagents)
	}
	record := result.Subagents[0]
	if record.Agent != AgentTrend || record.Status != SubagentStatusComplete || record.Task != task {
		t.Fatalf("subagent record = %#v", record)
	}
	if record.Report != "Tokens rose 40% on model X." || record.Rounds != 2 {
		t.Fatalf("subagent finding = %#v", record)
	}
	if record.Usage.Requests != 2 || record.Usage.TotalTokens != 330 {
		t.Fatalf("subagent usage = %#v", record.Usage)
	}
	if len(record.ToolsUsed) != 1 || record.ToolsUsed[0] != "get_daily_usage" {
		t.Fatalf("subagent tools = %#v", record.ToolsUsed)
	}

	// The whole turn's usage covers the lead and its specialist.
	if result.Usage.Requests != 4 || result.Usage.TotalTokens != 880 {
		t.Fatalf("turn usage = %#v", result.Usage)
	}

	// The specialist's tool call is attributed to it and linked to the delegation.
	var specialistCall *ToolCallRecord
	for i := range result.ToolCalls {
		if result.ToolCalls[i].Agent == AgentTrend {
			specialistCall = &result.ToolCalls[i]
		}
	}
	if specialistCall == nil || specialistCall.ParentCallID != record.CallID {
		t.Fatalf("specialist tool calls = %#v", result.ToolCalls)
	}

	var start, finish *StreamEvent
	for i := range events {
		switch events[i].Type {
		case StreamEventSubagentStart:
			start = &events[i]
		case StreamEventSubagentFinish:
			finish = &events[i]
		}
	}
	if start == nil || start.Subagent == nil || start.Subagent.Agent != AgentTrend || start.Subagent.Task != task {
		t.Fatalf("subagent start event = %#v", start)
	}
	if finish == nil || finish.Subagent == nil || finish.Subagent.Report == "" || finish.Subagent.Usage == nil {
		t.Fatalf("subagent finish event = %#v", finish)
	}

	// A specialist must never receive the conversation or the delegation tool.
	specialistRequests := client.requestsFor(AgentTrend)
	if len(specialistRequests) != 2 {
		t.Fatalf("specialist rounds = %d", len(specialistRequests))
	}
	for _, message := range specialistRequests[0].Messages {
		if strings.Contains(string(message), "Why did tokens grow?") {
			t.Fatalf("the specialist saw the user's conversation: %s", message)
		}
	}
	for _, definition := range specialistRequests[0].Tools {
		if definition.Name == delegateToolName {
			t.Fatal("a specialist was offered the delegation tool")
		}
	}
	trend, _ := agentByID(AgentTrend)
	if len(specialistRequests[0].Tools) != len(trend.Tools) {
		t.Fatalf("specialist tools = %d, want its %d allowlisted tools", len(specialistRequests[0].Tools), len(trend.Tools))
	}
}

func TestTruncatedSpecialistFindingTriggersDeterministicNotice(t *testing.T) {
	const task = "Analyze the complete opencode trend and return a detailed evidence-backed finding."
	client := newRoutedClient()
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", []ToolCall{delegationCall("delegate", AgentTrend, task)}, nil),
		assistantResponse(t, "stop", "Lead summary.", nil, nil),
	)
	client.script(AgentTrend,
		assistantResponse(t, "stop", strings.Repeat("x", maxSubagentReportBytes+100), nil, nil),
	)
	result, err := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)}).Chat(
		context.Background(), oneUserMessage("Investigate."),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != 1 || !strings.HasSuffix(result.Subagents[0].Report, "[finding truncated]") {
		t.Fatalf("specialist report was not visibly bounded: %#v", result.Subagents)
	}
	if len(result.Notices) != 1 || result.Notices[0] != truncationNotice || !strings.Contains(result.Message.Content, truncationNotice) {
		t.Fatalf("specialist truncation was not disclosed deterministically: %#v", result)
	}
}

func TestServiceReportsSpecialistFailureAsEvidenceNotRunFailure(t *testing.T) {
	client := newRoutedClient()
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", []ToolCall{
			delegationCall("call-1", AgentCost, "Audit opencode cost provenance for the last 30 days."),
		}, nil),
		assistantResponse(t, "stop", "The cost audit could not complete; here is the raw total.", nil, nil),
	)
	client.fail(AgentCost, &ProviderError{Operation: "chat completion", StatusCode: 400, Message: "bad request"})
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Audit my costs."))
	if err != nil {
		t.Fatalf("a failed specialist ended the run: %v", err)
	}
	if len(result.Subagents) != 1 || result.Subagents[0].Status != SubagentStatusFailed || result.Subagents[0].Error == "" {
		t.Fatalf("subagent records = %#v", result.Subagents)
	}
	if result.Subagents[0].Report != "" {
		t.Fatalf("a failed specialist reported a finding: %#v", result.Subagents[0])
	}
	leadRequests := client.requestsFor(AgentLead)
	last := string(leadRequests[1].Messages[len(leadRequests[1].Messages)-1])
	if !strings.Contains(last, "specialist_failed") {
		t.Fatalf("the lead agent was not told the specialist failed: %s", last)
	}
	if strings.Contains(last, "bad request") {
		t.Fatalf("provider detail leaked into the lead agent's context: %s", last)
	}
}

func TestServiceRejectsInvalidAndOverusedDelegations(t *testing.T) {
	client := newRoutedClient()
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", []ToolCall{
			delegationCall("call-1", "root_agent", "Investigate everything about this machine, thoroughly."),
			delegationCall("call-2", AgentTrend, "too short"),
		}, nil),
		assistantResponse(t, "stop", "I will analyze this directly.", nil, nil),
	)
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Investigate."))
	if err != nil {
		t.Fatalf("invalid delegations ended the run: %v", err)
	}
	if len(result.Subagents) != 0 {
		t.Fatalf("a rejected delegation produced a specialist record: %#v", result.Subagents)
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	if !strings.Contains(string(result.ToolCalls[0].Result), "unknown_specialist") {
		t.Fatalf("unknown specialist result = %s", result.ToolCalls[0].Result)
	}
	if !strings.Contains(string(result.ToolCalls[1].Result), "invalid_arguments") {
		t.Fatalf("short task result = %s", result.ToolCalls[1].Result)
	}
}

func TestEquivalentDelegationsShareCanonicalDuplicateFingerprint(t *testing.T) {
	const task = "Analyze the opencode request trend and identify the strongest aggregate driver."
	firstArgs := `{"task":"  ` + task + `  ","agent":"trend_analyst"}`
	secondArgs := `{"agent":"trend_analyst","task":"` + task + `"}`
	client := newRoutedClient()
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", []ToolCall{functionToolCall("first", delegateToolName, firstArgs)}, nil),
		assistantResponse(t, "tool_calls", "", []ToolCall{functionToolCall("second", delegateToolName, secondArgs)}, nil),
		assistantResponse(t, "stop", "Used the original specialist finding.", nil, nil),
	)
	client.script(AgentTrend, assistantResponse(t, "stop", "Trend finding.", nil, nil))

	result, err := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)}).Chat(
		context.Background(), oneUserMessage("Investigate."),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != 1 || len(client.requestsFor(AgentTrend)) != 1 {
		t.Fatalf("equivalent delegation ran more than once: subagents=%#v requests=%d", result.Subagents, len(client.requestsFor(AgentTrend)))
	}
	if len(result.ToolCalls) != 1 || !strings.Contains(string(result.ToolCalls[0].Result), "duplicate_call") {
		t.Fatalf("second delegation was not rejected as duplicate: %#v", result.ToolCalls)
	}
}

func TestServiceBoundsDelegationsPerTurn(t *testing.T) {
	const task = "Analyze opencode usage for the last 30 days and report the dominant driver."
	calls := make([]ToolCall, maxDelegationsPerTurn+1)
	for i := range calls {
		// Distinct tasks so the duplicate guard is not what stops the last one.
		calls[i] = delegationCall("call-"+string(rune('a'+i)), AgentTrend, task+" Variant "+string(rune('A'+i))+".")
	}
	client := newRoutedClient()
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", calls, nil),
		assistantResponse(t, "stop", "Combined finding.", nil, nil),
	)
	for i := 0; i < maxDelegationsPerTurn; i++ {
		client.script(AgentTrend, assistantResponse(t, "stop", "Finding.", nil, nil))
	}
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Investigate several things."))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != maxDelegationsPerTurn {
		t.Fatalf("specialist runs = %d, want %d", len(result.Subagents), maxDelegationsPerTurn)
	}
	refused := result.ToolCalls[len(result.ToolCalls)-1]
	if refused.OK || !strings.Contains(string(refused.Result), "delegation_budget_exhausted") {
		t.Fatalf("over-budget delegation = %#v", refused)
	}
}

func TestServiceGathersIndependentEvidenceConcurrently(t *testing.T) {
	calls := []ToolCall{
		functionToolCall("a", "list_sources", `{}`),
		functionToolCall("b", "get_overview", `{"source":"opencode","period":"7d"}`),
		functionToolCall("c", "get_model_usage", `{"source":"opencode","period":"7d"}`),
	}
	client := &scriptedAgentClient{responses: []*ChatResponse{
		assistantResponse(t, "tool_calls", "", calls, nil),
		assistantResponse(t, "stop", "Report.", nil, nil),
	}}
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Report."))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 3 {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	// Results must be replayed in the order the model asked for them.
	wantOrder := []string{"list_sources", "get_overview", "get_model_usage"}
	toolMessages := client.requests[1].Messages[len(client.requests[1].Messages)-3:]
	for index, message := range toolMessages {
		if !strings.Contains(string(message), `"name":"`+wantOrder[index]+`"`) {
			t.Fatalf("tool message %d = %s, want %s", index, message, wantOrder[index])
		}
	}
	for index, record := range result.ToolCalls {
		if record.CallID != "tool-"+string(rune('1'+index)) {
			t.Fatalf("call ids are not stable: %#v", result.ToolCalls)
		}
	}
}

func TestProviderCannotProposeUnboundedCallsInOneRound(t *testing.T) {
	calls := make([]ToolCall, maxToolCallsPerRound+1)
	for index := range calls {
		calls[index] = functionToolCall(fmt.Sprintf("call-%d", index), "list_sources", `{}`)
	}
	client := &scriptedAgentClient{responses: []*ChatResponse{
		assistantResponse(t, "tool_calls", "", calls, nil),
	}}
	result, err := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)}).Chat(
		context.Background(), oneUserMessage("Report."),
	)
	if !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("result=%#v err=%v, want bounded provider failure", result, err)
	}
}

func TestServiceRunsIndependentSpecialistsInParallel(t *testing.T) {
	client := newRoutedClient()
	client.delay = 60 * time.Millisecond
	client.script(AgentLead,
		assistantResponse(t, "tool_calls", "", []ToolCall{
			delegationCall("call-1", AgentTrend, "Explain the opencode token trend over the last 7 days."),
			delegationCall("call-2", AgentTooling, "Explain the opencode tool failure rate over the last 7 days."),
		}, nil),
		assistantResponse(t, "stop", "Both findings agree.", nil, nil),
	)
	client.script(AgentTrend, assistantResponse(t, "stop", "Trend finding.", nil, nil))
	client.script(AgentTooling, assistantResponse(t, "stop", "Tooling finding.", nil, nil))
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Investigate both."))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != 2 {
		t.Fatalf("specialist runs = %#v", result.Subagents)
	}
	client.mu.Lock()
	concurrent := client.maxLive
	client.mu.Unlock()
	if concurrent < 2 {
		t.Fatalf("peak concurrent provider calls = %d, want independent specialists to overlap", concurrent)
	}
}

func TestServiceRetriesTransientProviderFailuresOnce(t *testing.T) {
	client := newRoutedClient()
	client.fail(AgentLead, &ProviderError{Operation: "chat completion", StatusCode: 503, Message: "upstream busy"})
	client.script(AgentLead, nil, assistantResponse(t, "stop", "Recovered report.", nil, nil))
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	result, err := service.Chat(context.Background(), oneUserMessage("Report."))
	if err != nil {
		t.Fatalf("a transient provider failure was not retried: %v", err)
	}
	if result.Message.Content != "Recovered report." {
		t.Fatalf("content = %q", result.Message.Content)
	}
	if attempts := len(client.requestsFor(AgentLead)); attempts != 2 {
		t.Fatalf("provider attempts = %d, want one retry", attempts)
	}
}

func TestServiceDoesNotRetryPermanentProviderFailures(t *testing.T) {
	client := newRoutedClient()
	client.fail(AgentLead,
		&AuthenticationError{Operation: "chat completion", StatusCode: 401},
		&AuthenticationError{Operation: "chat completion", StatusCode: 401},
	)
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	if _, err := service.Chat(context.Background(), oneUserMessage("Report.")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if attempts := len(client.requestsFor(AgentLead)); attempts != 1 {
		t.Fatalf("provider attempts = %d, want no retry", attempts)
	}
}

func TestServiceWithdrawsStreamedContentBeforeRetrying(t *testing.T) {
	base := &scriptedAgentClient{responses: []*ChatResponse{
		nil,
		assistantResponse(t, "stop", "Recovered report.", nil, nil),
	}}
	base.attemptErrors = []error{&ProviderError{Operation: "stream chat completion", StatusCode: 500}}
	client := &streamingAgentClient{
		scriptedAgentClient: base,
		chunks:              [][]string{{"Partial "}, {"Recovered report."}},
	}
	service := NewService(ServiceOptions{Client: client, Registry: source.NewRegistry(source.SourceOpenCode)})

	var types []string
	result, err := service.ChatStream(context.Background(), oneUserMessage("Report."), func(event StreamEvent) error {
		types = append(types, event.Type)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Message.Content != "Recovered report." {
		t.Fatalf("content = %q", result.Message.Content)
	}
	if strings.Count(strings.Join(types, ","), StreamEventContentReset) != 1 {
		t.Fatalf("event types = %#v, want exactly one reset before the retry", types)
	}
}

func TestBoundReportTruncatesOnARuneBoundary(t *testing.T) {
	t.Parallel()
	report, truncated := boundReport(strings.Repeat("é", maxSubagentReportBytes))
	if !truncated || !strings.HasSuffix(report, "[finding truncated]") {
		t.Fatalf("truncation marker missing: %q", report[len(report)-40:])
	}
	if !json.Valid(mustJSONString(t, report)) {
		t.Fatal("a truncated report is not valid UTF-8 JSON")
	}
	short, truncated := boundReport("  concise finding  ")
	if truncated || short != "concise finding" {
		t.Fatalf("short report = %q truncated=%v", short, truncated)
	}
}

func TestDelegationLengthMatchesJSONSchemaCharacterCounts(t *testing.T) {
	task := strings.Repeat("界", minDelegatedTaskChars)
	encoded, err := json.Marshal(delegationArgs{Agent: string(AgentTrend), Task: "  " + task + "  "})
	if err != nil {
		t.Fatal(err)
	}
	agent, gotTask, err := parseDelegation(string(encoded))
	if err != nil {
		t.Fatalf("unicode task rejected by byte length: %v", err)
	}
	if agent != AgentTrend || gotTask != task {
		t.Fatalf("delegation = %q/%q", agent, gotTask)
	}
	tooLong, _ := json.Marshal(delegationArgs{Agent: string(AgentTrend), Task: strings.Repeat("界", maxDelegatedTaskChars+1)})
	if _, _, err := parseDelegation(string(tooLong)); err == nil {
		t.Fatal("overlong unicode task accepted")
	}
}

func TestResultReportsNestedTruncation(t *testing.T) {
	for _, result := range []json.RawMessage{
		json.RawMessage(`{"ok":true,"data":{"truncated":true}}`),
		json.RawMessage(`{"ok":true,"data":{"sources":[{"trend_truncated":true}]}}`),
		json.RawMessage(`{"ok":true,"data":{"top_models_truncated":true}}`),
	} {
		if !resultReportsTruncation(result) {
			t.Fatalf("truncation not detected: %s", result)
		}
	}
	if resultReportsTruncation(json.RawMessage(`{"ok":true,"data":{"truncated":false}}`)) {
		t.Fatal("false truncation flag was treated as truncated")
	}
}

func mustJSONString(t *testing.T, value string) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
