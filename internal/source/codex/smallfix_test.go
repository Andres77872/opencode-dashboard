package codex

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestCodexLookupRateStripsDatedReleaseSuffix(t *testing.T) {
	pricing := New(Options{PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json")}).loadPricing(testContext(t))
	tokens := stats.TokenStats{Input: 500_000, Output: 100_000, Reasoning: 50_000, Cache: stats.CacheStats{Read: 250_000}}

	dated := computeCost("gpt-5.5-2026-01-15", "openai", tokens, 100_000, pricing)
	base := computeCost("gpt-5.5", "openai", tokens, 100_000, pricing)
	if dated.Status != stats.CostEstimatedAPIEquivalent {
		t.Fatalf("dated release status = %q, want %q", dated.Status, stats.CostEstimatedAPIEquivalent)
	}
	if !approxEqual(dated.Cost, base.Cost) {
		t.Errorf("dated release cost = %.9f, want base model cost %.9f", dated.Cost, base.Cost)
	}

	// Named variants may have entirely different rates; never guess by prefix.
	variant := computeCost("gpt-5.5-preview", "openai", tokens, 100_000, pricing)
	if variant.Status != stats.CostMissing {
		t.Errorf("named variant status = %q, want %q (no prefix guessing)", variant.Status, stats.CostMissing)
	}
}

func TestCodexLastOnlyUsageThenCumulativeCountsOnce(t *testing.T) {
	// A last_token_usage-only event must advance the running total, so a later
	// cumulative vector that already includes it does not count it twice.
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/05/rollout-2026-01-05T07-00-00Z-last-only.jsonl": {
			`{"timestamp":"2026-01-05T07:00:00Z","type":"session_meta","payload":{"id":"last-only-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-05T07:00:01Z","type":"turn_context","payload":{"turn_id":"lo-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-05T07:00:02Z","type":"event_msg","payload":{"type":"user_message","turn_id":"lo-1","message":"LO1"}}`,
			`{"timestamp":"2026-01-05T07:00:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"lo-1","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}`,
			`{"timestamp":"2026-01-05T07:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"lo-1","info":{"total_token_usage":{"input_tokens":1100,"cached_input_tokens":110,"output_tokens":70,"reasoning_output_tokens":30,"total_tokens":1195}}}}`,
		},
	})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// r0 raw (100,10,20,5) + r1 raw delta (1000,100,50,25), disjoint sums.
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 990, 110, 40, 30)
}

func TestCodexLongContextThresholdUsesLastUsageRawInput(t *testing.T) {
	// The long-context threshold compares the request's raw prompt size, which
	// last_token_usage reports directly. Here the cumulative delta (250000) is
	// below the 272000 threshold but the actual request input (280000) is above.
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/05/rollout-2026-01-05T08-00-00Z-long-last.jsonl": {
			`{"timestamp":"2026-01-05T08:00:00Z","type":"session_meta","payload":{"id":"long-last-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-05T08:00:01Z","type":"turn_context","payload":{"turn_id":"ll-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-05T08:00:02Z","type":"event_msg","payload":{"type":"token_count","turn_id":"ll-1","info":{"total_token_usage":{"input_tokens":50000,"cached_input_tokens":0,"output_tokens":100,"reasoning_output_tokens":0,"total_tokens":50100}}}}`,
			`{"timestamp":"2026-01-05T08:00:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"ll-1","info":{"last_token_usage":{"input_tokens":280000,"cached_input_tokens":0,"output_tokens":100,"reasoning_output_tokens":0,"total_tokens":280100},"total_token_usage":{"input_tokens":300000,"cached_input_tokens":0,"output_tokens":200,"reasoning_output_tokens":0,"total_tokens":300200}}}}`,
		},
	})
	entry := findMessage(t, readAllMessages(t, src), func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 250000
	})
	want := (float64(250000) * 5.0 * 2.0 / 1_000_000) + (float64(100) * 30.0 * 1.5 / 1_000_000)
	if !approxEqual(entry.Cost, want) {
		t.Errorf("cost = %.9f, want %.9f with long-context multipliers from last_token_usage input", entry.Cost, want)
	}
}

func TestCodexSubagentThreadsAreFlaggedAndKeepOwnSession(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/06/rollout-2026-01-06T07-00-00Z-agent-thread.jsonl": {
			`{"timestamp":"2026-01-06T07:00:00Z","type":"session_meta","payload":{"id":"agent-thread","parent_thread_id":"orchestrator-thread","agent_role":"worker","agent_nickname":"Volta","model_provider":"openai","cwd":"[REDACTED_PATH]/agentproj"}}`,
			`{"timestamp":"2026-01-06T07:00:01Z","type":"turn_context","payload":{"turn_id":"at-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-06T07:00:02Z","type":"event_msg","payload":{"type":"user_message","turn_id":"at-1","message":"AGENT_TASK"}}`,
			`{"timestamp":"2026-01-06T07:00:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"at-1","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":25,"total_tokens":1075}}}}`,
		},
	})
	messages := readAllMessages(t, src)
	if messages.Total != 2 {
		t.Fatalf("Messages total = %d, want 2", messages.Total)
	}
	for _, msg := range messages.Messages {
		if !msg.IsSubagent {
			t.Errorf("message %q IsSubagent = false, want true for agent_role thread", msg.ID)
		}
		if msg.SessionID != "agent-thread" {
			t.Errorf("message %q SessionID = %q, want the sub-agent's own session", msg.ID, msg.SessionID)
		}
	}
	detail, err := src.SessionByID(testContext(t), "agent-thread")
	if err != nil || detail == nil {
		t.Fatalf("SessionByID(agent-thread) = %v, %v", detail, err)
	}
	for _, msg := range detail.Messages {
		if !msg.IsSubagent {
			t.Errorf("session message %q IsSubagent = false, want true", msg.ID)
		}
	}
}

func TestCodexMcpToolCallEndUpdatesToolStatus(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/06/rollout-2026-01-06T08-00-00Z-mcp-session.jsonl": {
			`{"timestamp":"2026-01-06T08:00:00Z","type":"session_meta","payload":{"id":"mcp-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-06T08:00:01Z","type":"turn_context","payload":{"turn_id":"mcp-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-06T08:00:02Z","type":"response_item","payload":{"turn_id":"mcp-1","item":{"type":"function_call","id":"fc-mcp","call_id":"call-mcp-1","name":"mcp__search","arguments":"{}"}}}`,
			`{"timestamp":"2026-01-06T08:00:03Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","turn_id":"mcp-1","call_id":"call-mcp-1","status":"success"}}`,
			`{"timestamp":"2026-01-06T08:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"mcp-1","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":0,"output_tokens":50,"reasoning_output_tokens":0,"total_tokens":1050}}}}`,
		},
	})
	messages := readAllMessages(t, src)
	entry := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.Role == "assistant" })
	detail := mustMessageDetail(t, src, entry.ID)
	tool := findToolPart(t, detail, "call-mcp-1")
	if tool.State.Status != "completed" {
		t.Errorf("mcp tool status = %q, want completed after mcp_tool_call_end", tool.State.Status)
	}
	if tool.State.Time == nil || tool.State.Time.End == 0 {
		t.Errorf("mcp tool end time = %#v, want set by mcp_tool_call_end", tool.State.Time)
	}
}

func TestCodexRevisitedTurnKeepsMessageIDsUnique(t *testing.T) {
	// Transcripts can return to an earlier turn id; its row counters must
	// resume, not restart, or the synthesized ids collide and the cache's
	// (source_id, message_id) primary key silently collapses rows.
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/07/rollout-2026-01-07T07-00-00Z-revisit.jsonl": {
			`{"timestamp":"2026-01-07T07:00:00Z","type":"session_meta","payload":{"id":"revisit-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-07T07:00:01Z","type":"turn_context","payload":{"turn_id":"turn-a","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-07T07:00:02Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-a","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":110}}}}`,
			`{"timestamp":"2026-01-07T07:00:03Z","type":"turn_context","payload":{"turn_id":"turn-b","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-07T07:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-b","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":0,"total_tokens":330}}}}`,
			`{"timestamp":"2026-01-07T07:00:05Z","type":"turn_context","payload":{"turn_id":"turn-a","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-07T07:00:06Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-a","info":{"total_token_usage":{"input_tokens":600,"cached_input_tokens":0,"output_tokens":60,"reasoning_output_tokens":0,"total_tokens":660}}}}`,
		},
	})
	messages := readAllMessages(t, src)
	if messages.Total != 3 {
		t.Fatalf("Messages total = %d, want 3 rows across revisited turns", messages.Total)
	}
	ids := map[string]bool{}
	for _, msg := range messages.Messages {
		if ids[msg.ID] {
			t.Errorf("duplicate message id %q", msg.ID)
		}
		ids[msg.ID] = true
	}
	findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == "codex:revisit-session:turn-a:r0" })
	findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == "codex:revisit-session:turn-b:r0" })
	findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == "codex:revisit-session:turn-a:r1" })
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 600, 0, 60, 0)
}

func TestCodexTurnAbortedFlushesPendingContent(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/06/rollout-2026-01-06T09-00-00Z-aborted.jsonl": {
			`{"timestamp":"2026-01-06T09:00:00Z","type":"session_meta","payload":{"id":"aborted-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-06T09:00:01Z","type":"turn_context","payload":{"turn_id":"ab-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-06T09:00:02Z","type":"event_msg","payload":{"type":"user_message","turn_id":"ab-1","message":"AB1"}}`,
			`{"timestamp":"2026-01-06T09:00:03Z","type":"response_item","payload":{"turn_id":"ab-1","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ABORTED_PARTIAL_REPLY"}]}}}`,
			`{"timestamp":"2026-01-06T09:00:04Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":"ab-1"}}`,
		},
	})
	messages := readAllMessages(t, src)
	if messages.Total != 2 {
		t.Fatalf("Messages total = %d, want user prompt + flushed aborted content", messages.Total)
	}
	entry := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.Role == "assistant" })
	if entry.Tokens != nil {
		t.Errorf("aborted row tokens = %+v, want none (no token_count arrived)", entry.Tokens)
	}
	detail := mustMessageDetail(t, src, entry.ID)
	if !detailTextContains(detail, "ABORTED_PARTIAL_REPLY") {
		t.Errorf("aborted row is missing its partial content")
	}
}
