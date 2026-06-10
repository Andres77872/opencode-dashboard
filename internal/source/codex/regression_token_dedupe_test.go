package codex

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestCodexTokenAggregationUsesPositiveCumulativeDeltas(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	assertCodexSourceID(t, overview.SourceID)
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 1200, 300, 45, 30)

	messages, err := src.Messages(ctx, period, 1, 50, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	// 2 user prompts + 2 assistant API requests.
	if messages.Total != 4 || len(messages.Messages) != 4 {
		t.Fatalf("Messages total/len = %d/%d, want 4 per-request rows", messages.Total, len(messages.Messages))
	}
	// Per-request deltas across the rows sum exactly to the cumulative totals.
	assertCodexTokenTotals(t, "sum(Messages().Tokens)", sumMessageTokens(messages), 1200, 300, 45, 30)

	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all) failed: %v", err)
	}
	model := findModelEntryByID(t, models, "gpt-5.5")
	assertCodexTokenTotals(t, "Models()[gpt-5.5].Tokens", model.Tokens, 1200, 300, 45, 30)
}

func TestCodexLastTokenUsageDoesNotInflateUnchangedOrLowerCumulativeVectors(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T07-00-00Z-token-session.jsonl": tokenDedupeLines("token-session"),
	})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 1200, 300, 45, 30)
}

func TestCodexDuplicateCopiedTranscriptsCountOnce(t *testing.T) {
	lines := tokenDedupeLines("copied-session")
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T07-00-00Z-copied-session.jsonl":      lines,
		"sessions/2026/01/03/rollout-2026-01-03T07-00-00Z-copied-session-copy.jsonl": lines,
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Copied transcript collapses to one logical turn. tokenDedupeLines drives
	// 2 positive cumulative deltas (r0, r1) plus a trailing assistant content
	// row (r2, no usage) since its response_item follows all token_count events,
	// alongside the single user prompt row.
	if overview.Sessions != 1 || overview.Messages != 4 {
		t.Errorf("overview sessions/messages = %d/%d, want 1/4 after copied transcript dedupe", overview.Sessions, overview.Messages)
	}
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 1200, 300, 45, 30)

	messages, err := src.Messages(ctx, period, 1, 50, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 4 || len(messages.Messages) != 4 {
		t.Fatalf("Messages total/len = %d/%d, want copied logical turn counted once", messages.Total, len(messages.Messages))
	}
	// Tokens are unchanged by the copy: per-request deltas sum to the cumulative totals.
	assertCodexTokenTotals(t, "sum(Messages().Tokens)", sumMessageTokens(messages), 1200, 300, 45, 30)
}

func tokenDedupeLines(sessionID string) []string {
	return []string{
		`{"timestamp":"2026-01-02T07:00:00Z","type":"session_meta","payload":{"id":"` + sessionID + `","model_provider":"openai","cwd":"[REDACTED_PATH]/tokens"}}`,
		`{"timestamp":"2026-01-02T07:00:01Z","type":"turn_context","payload":{"turn_id":"turn-token","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"2026-01-02T07:00:02Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-token"}}`,
		`{"timestamp":"2026-01-02T07:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"turn-token","message":"[REDACTED_TOKEN_PROMPT]"}}`,
		`{"timestamp":"2026-01-02T07:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-token","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":25,"total_tokens":1075},"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":25,"total_tokens":1075}}}}`,
		`{"timestamp":"2026-01-02T07:00:05Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-token","info":{"last_token_usage":{"input_tokens":500,"cached_input_tokens":200,"output_tokens":25,"reasoning_output_tokens":5,"total_tokens":500},"total_token_usage":{"input_tokens":1500,"cached_input_tokens":300,"output_tokens":75,"reasoning_output_tokens":30,"total_tokens":1575}}}}`,
		`{"timestamp":"2026-01-02T07:00:06Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-token","info":{"last_token_usage":{"input_tokens":999999,"cached_input_tokens":999999,"output_tokens":999999,"reasoning_output_tokens":999999,"total_tokens":3999996},"total_token_usage":{"input_tokens":1500,"cached_input_tokens":300,"output_tokens":75,"reasoning_output_tokens":30,"total_tokens":1575}}}}`,
		`{"timestamp":"2026-01-02T07:00:07Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-token","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":50,"output_tokens":10,"reasoning_output_tokens":5,"total_tokens":115},"total_token_usage":{"input_tokens":1490,"cached_input_tokens":250,"output_tokens":70,"reasoning_output_tokens":25,"total_tokens":1565}}}}`,
		`{"timestamp":"2026-01-02T07:00:08Z","type":"response_item","payload":{"turn_id":"turn-token","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"[REDACTED_TOKEN_ASSISTANT]"}]}}}`,
		`{"timestamp":"2026-01-02T07:00:09Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-token","status":"success"}}`,
	}
}

func sumMessageTokens(list stats.MessageList) stats.TokenStats {
	var total stats.TokenStats
	for _, msg := range list.Messages {
		if msg.Tokens == nil {
			continue
		}
		total.Input += msg.Tokens.Input
		total.Output += msg.Tokens.Output
		total.Reasoning += msg.Tokens.Reasoning
		total.Cache.Read += msg.Tokens.Cache.Read
		total.Cache.Write += msg.Tokens.Cache.Write
	}
	return total
}

func assertCodexTokenTotals(t *testing.T, label string, got stats.TokenStats, wantInput, wantCacheRead, wantOutput, wantReasoning int64) {
	t.Helper()
	if got.Input != wantInput || got.Cache.Read != wantCacheRead || got.Output != wantOutput || got.Reasoning != wantReasoning || got.Cache.Write != 0 {
		t.Errorf("%s = input/cache.read/output/reasoning/cache.write %d/%d/%d/%d/%d, want %d/%d/%d/%d/0", label, got.Input, got.Cache.Read, got.Output, got.Reasoning, got.Cache.Write, wantInput, wantCacheRead, wantOutput, wantReasoning)
	}
}
