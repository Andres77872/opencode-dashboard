package codex

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

// TestCodexEmitsOneMessagePerTokenCountAPIRequest verifies the reporting change:
// a single turn that drives several model API requests (each closed by a token_count
// event) is un-folded into one assistant row per request plus one user-prompt row,
// with each request carrying its own per-request token delta and cost, and the row
// totals exactly matching the turn's cumulative usage.
func TestCodexEmitsOneMessagePerTokenCountAPIRequest(t *testing.T) {
	lines := []string{
		`{"type":"session_meta","timestamp":"2026-02-01T10:00:00.000Z","payload":{"id":"req-session","cwd":"/home/u/proj","model_provider":"openai"}}`,
		`{"type":"turn_context","timestamp":"2026-02-01T10:00:00.100Z","payload":{"turn_id":"t1","model":"gpt-5.5","cwd":"/home/u/proj"}}`,
		`{"type":"event_msg","timestamp":"2026-02-01T10:00:00.200Z","payload":{"type":"task_started","turn_id":"t1"}}`,
		`{"type":"event_msg","timestamp":"2026-02-01T10:00:00.300Z","payload":{"type":"user_message","turn_id":"t1","message":"Do the task."}}`,
		// --- API request 1: reasoning + assistant text + one tool call+output, then token_count ---
		`{"type":"response_item","timestamp":"2026-02-01T10:00:01.000Z","payload":{"turn_id":"t1","type":"reasoning","summary":[{"type":"summary_text","text":"Planning step one."}]}}`,
		`{"type":"response_item","timestamp":"2026-02-01T10:00:01.100Z","payload":{"turn_id":"t1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Running the first command."}]}}`,
		`{"type":"response_item","timestamp":"2026-02-01T10:00:01.200Z","payload":{"turn_id":"t1","type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"ls\"}","call_id":"call_a"}}`,
		`{"type":"response_item","timestamp":"2026-02-01T10:00:01.300Z","payload":{"turn_id":"t1","type":"function_call_output","call_id":"call_a","output":"file1\nfile2"}}`,
		`{"type":"event_msg","timestamp":"2026-02-01T10:00:01.400Z","payload":{"type":"token_count","turn_id":"t1","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":20,"total_tokens":1070},"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":20,"total_tokens":1070}}}}`,
		// --- API request 2: assistant text + one tool call+output, then token_count ---
		`{"type":"response_item","timestamp":"2026-02-01T10:00:02.000Z","payload":{"turn_id":"t1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Now the second command."}]}}`,
		`{"type":"response_item","timestamp":"2026-02-01T10:00:02.100Z","payload":{"turn_id":"t1","type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}","call_id":"call_b"}}`,
		`{"type":"response_item","timestamp":"2026-02-01T10:00:02.200Z","payload":{"turn_id":"t1","type":"function_call_output","call_id":"call_b","output":"/home/u/proj"}}`,
		`{"type":"event_msg","timestamp":"2026-02-01T10:00:02.300Z","payload":{"type":"token_count","turn_id":"t1","info":{"last_token_usage":{"input_tokens":500,"cached_input_tokens":50,"output_tokens":30,"reasoning_output_tokens":10,"total_tokens":590},"total_token_usage":{"input_tokens":1500,"cached_input_tokens":150,"output_tokens":80,"reasoning_output_tokens":30,"total_tokens":1660}}}}`,
		`{"type":"event_msg","timestamp":"2026-02-01T10:00:02.400Z","payload":{"type":"task_complete","turn_id":"t1"}}`,
	}
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/02/01/rollout-2026-02-01T10-00-00Z-req-session.jsonl": lines,
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1", overview.Sessions)
	}
	// 1 user prompt + 2 assistant API requests.
	if overview.Messages != 3 {
		t.Errorf("Overview().Messages = %d, want 3 (1 user + 2 API requests)", overview.Messages)
	}
	// Per-request deltas sum exactly to the turn's cumulative usage.
	if overview.Tokens.Input != 1500 || overview.Tokens.Cache.Read != 150 || overview.Tokens.Output != 80 || overview.Tokens.Reasoning != 30 {
		t.Errorf("Overview().Tokens in/cache/out/reason = %d/%d/%d/%d, want 1500/150/80/30",
			overview.Tokens.Input, overview.Tokens.Cache.Read, overview.Tokens.Output, overview.Tokens.Reasoning)
	}
	// Cost: r0=(900*5+100*0.5+70*30)/1e6=0.00665, r1=(450*5+50*0.5+40*30)/1e6=0.003475 => 0.010125
	if !approxEqual(overview.Cost, 0.010125) {
		t.Errorf("Overview().Cost = %.9f, want 0.010125 (sum of per-request estimated costs)", overview.Cost)
	}
	if overview.CostStatus != stats.CostEstimatedAPIEquivalent {
		t.Errorf("Overview().CostStatus = %q, want %q", overview.CostStatus, stats.CostEstimatedAPIEquivalent)
	}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 3 || len(messages.Messages) != 3 {
		t.Fatalf("Messages total/len = %d/%d, want 3/3: %#v", messages.Total, len(messages.Messages), messages.Messages)
	}
	roles := map[string]int{}
	for _, m := range messages.Messages {
		roles[m.Role]++
	}
	if roles["user"] != 1 || roles["assistant"] != 2 {
		t.Errorf("role counts = %#v, want 1 user + 2 assistant", roles)
	}

	// Request 1 row carries its own delta and tool call_a.
	r0 := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 1000
	})
	if r0.Tokens.Output != 50 || r0.Tokens.Reasoning != 20 || r0.Tokens.Cache.Read != 100 {
		t.Errorf("request 1 tokens = %#v, want input 1000 / out 50 / reason 20 / cache 100", *r0.Tokens)
	}
	if !approxEqual(r0.Cost, 0.00665) {
		t.Errorf("request 1 cost = %.9f, want 0.00665", r0.Cost)
	}
	if r0.ModelID != "gpt-5.5" || r0.ProviderID != "openai" {
		t.Errorf("request 1 model/provider = %q/%q, want gpt-5.5/openai", r0.ModelID, r0.ProviderID)
	}
	r0detail := mustMessageDetail(t, src, r0.ID)
	_ = findToolPart(t, r0detail, "call_a")
	if !detailTextContains(r0detail, "Running the first command.") {
		t.Errorf("request 1 detail missing its assistant text: %#v", r0detail.Content.TextParts)
	}

	// Request 2 row carries the second delta and tool call_b (not call_a).
	r1 := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 500
	})
	if !approxEqual(r1.Cost, 0.003475) {
		t.Errorf("request 2 cost = %.9f, want 0.003475", r1.Cost)
	}
	r1detail := mustMessageDetail(t, src, r1.ID)
	_ = findToolPart(t, r1detail, "call_b")
	for _, tp := range r1detail.Content.ToolParts {
		if tp.CallID == "call_a" {
			t.Errorf("request 2 detail should not contain request 1's tool call_a")
		}
	}

	// Models splits per request under the turn's model.
	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all) failed: %v", err)
	}
	model := findModelEntryByID(t, models, "gpt-5.5")
	if model.Messages != 2 {
		t.Errorf("Models()[gpt-5.5].Messages = %d, want 2 API requests", model.Messages)
	}

	// Session totals include all requests; count is unchanged at 1.
	session, err := src.SessionByID(ctx, "req-session")
	if err != nil {
		t.Fatalf("SessionByID failed: %v", err)
	}
	if session == nil || session.MessageCount != 3 {
		t.Fatalf("session message_count = %v, want 3", session)
	}
	if session.TotalTokens.Input != 1500 || session.TotalTokens.Output != 80 {
		t.Errorf("session totals in/out = %d/%d, want 1500/80", session.TotalTokens.Input, session.TotalTokens.Output)
	}
}
