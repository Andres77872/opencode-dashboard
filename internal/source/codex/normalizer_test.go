package codex

import (
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestNormalizerEmitsOneRowPerAPIRequestAndUserPrompt(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)

	if messages.SourceID != string(source.SourceCodex) {
		t.Errorf("MessageList.SourceID = %q, want %q", messages.SourceID, source.SourceCodex)
	}
	// 2 user prompts (u0, u1) + 2 assistant API requests (r0, r1).
	if messages.Total != 4 || len(messages.Messages) != 4 {
		t.Fatalf("Messages total/len = %d/%d, want 4 per-request Codex rows for turn-1", messages.Total, len(messages.Messages))
	}

	roles := map[string]int{}
	for _, msg := range messages.Messages {
		assertCodexSourceID(t, msg.SourceID)
		if msg.SessionID != "synthetic-session" {
			t.Errorf("SessionID = %q, want synthetic-session", msg.SessionID)
		}
		roles[msg.Role]++
	}
	if roles["user"] != 2 || roles["assistant"] != 2 {
		t.Errorf("role counts = %#v, want 2 user + 2 assistant", roles)
	}

	// r0 is the request that carries the first cumulative delta (raw
	// 1000/100/50/25, stored disjoint as Input 900) plus all of the turn's
	// content (it all precedes the token_count events).
	r0 := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 900
	})
	if r0.ID != "codex:synthetic-session:turn-1:r0" {
		t.Errorf("first request ID = %q, want codex:synthetic-session:turn-1:r0", r0.ID)
	}
	if r0.ModelID != "gpt-5.5" || r0.ProviderID != "openai" {
		t.Errorf("model/provider = %q/%q, want gpt-5.5/openai", r0.ModelID, r0.ProviderID)
	}

	// r1 is the usage-only second request delta (raw 500/200/25/5, stored
	// disjoint as Input 300), no content.
	r1 := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 300
	})
	if r1.ID != "codex:synthetic-session:turn-1:r1" {
		t.Errorf("second request ID = %q, want codex:synthetic-session:turn-1:r1", r1.ID)
	}

	// The two zero-delta token_count events (tc3/tc4) add no rows.
	assistantCount := 0
	for _, msg := range messages.Messages {
		if msg.Role == "assistant" {
			assistantCount++
		}
	}
	if assistantCount != 2 {
		t.Errorf("assistant rows = %d, want 2 (zero-delta token_count events add no rows)", assistantCount)
	}
}

func TestNormalizerFiltersRawChildRecordsFromTopLevelRows(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)

	for _, msg := range messages.Messages {
		for _, forbidden := range []string{"response_item", "token_count", "task_started", "task_complete", "context_compacted", "compacted", "agent_message"} {
			if strings.Contains(msg.ID, forbidden) || strings.Contains(msg.SessionTitle, forbidden) {
				t.Errorf("top-level message %#v exposes raw Codex child record %q", msg, forbidden)
			}
		}
	}

	// Each legitimate user_message is now its own user row (u0, u1).
	userRows := 0
	sawPart1, sawPart2 := false, false
	for _, msg := range messages.Messages {
		if msg.Role != "user" {
			continue
		}
		userRows++
		userDetail := mustMessageDetail(t, src, msg.ID)
		if detailTextContains(userDetail, "[REDACTED_USER_MESSAGE_PART_1]") {
			sawPart1 = true
		}
		if detailTextContains(userDetail, "[REDACTED_USER_MESSAGE_PART_2]") {
			sawPart2 = true
		}
	}
	if userRows != 2 {
		t.Errorf("user rows = %d, want 2 (one per user_message)", userRows)
	}
	if !sawPart1 || !sawPart2 {
		t.Errorf("user prompts seen part1/part2 = %v/%v, want both user_message rows present", sawPart1, sawPart2)
	}

	// All assistant/tool/reasoning content attaches to the request row that
	// closes first (r0), since it all precedes the token_count events.
	r0 := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 900
	})
	detail := mustMessageDetail(t, src, r0.ID)
	if detailTextOccurrences(detail, "[REDACTED_ASSISTANT_SUMMARY]") != 1 {
		t.Errorf("assistant mirror occurrences = %d, want exactly 1 after response_item/agent_message dedupe", detailTextOccurrences(detail, "[REDACTED_ASSISTANT_SUMMARY]"))
	}
	if len(detail.Content.ReasoningParts) != 1 {
		t.Errorf("ReasoningParts len = %d, want 1 placeholder/count for redacted reasoning event", len(detail.Content.ReasoningParts))
	}
	if len(detail.Content.ToolParts) != 3 {
		t.Errorf("ToolParts len = %d, want 3 tool/search calls on the request row", len(detail.Content.ToolParts))
	}
	assertJSONDoesNotContain(t, detail, "[REDACTED_REPLAYED_USER_MESSAGE]", "[REDACTED_DEVELOPER_REPLAY]", "[REDACTED_USER_REPLAY]", "[REDACTED_ASSISTANT_REPLAY]")
}

func TestNormalizerRepeatedTurnContextUpdatesMetadataOnly(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)

	// Repeated turn_context records update metadata only; they never add rows.
	// The per-request layout is 2 user + 2 assistant rows.
	if messages.Total != 4 {
		t.Fatalf("Messages total = %d, want 4 despite repeated turn_context rows", messages.Total)
	}

	assistant := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 900
	})
	if assistant.ModelID != "gpt-5.5" {
		t.Errorf("ModelID = %q, want updated turn_context model gpt-5.5", assistant.ModelID)
	}
	if assistant.SessionTitle == "" {
		t.Errorf("SessionTitle is empty, want redacted session title")
	}
}

func TestNormalizerFallbackIdentityIsStableWhenTurnIDMissing(t *testing.T) {
	files := map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T05-00-00Z-fallback-session.jsonl": {
			`{"timestamp":"2026-01-02T05:00:00Z","type":"session_meta","payload":{"id":"fallback-session","model_provider":"openai","cwd":"[REDACTED_PATH]/fallback"}}`,
			`{"timestamp":"2026-01-02T05:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"[REDACTED_FALLBACK_PROMPT]"}}`,
			`{"timestamp":"2026-01-02T05:00:02Z","type":"turn_context","payload":{"model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-02T05:00:03Z","type":"response_item","payload":{"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"[REDACTED_FALLBACK_ASSISTANT]"}]}}}`,
			`{"timestamp":"2026-01-02T05:00:04Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0,"total_tokens":15}}}}`,
		},
	}
	src := newTempCodexSource(t, files)

	first := readAllMessages(t, src)
	second := readAllMessages(t, src)
	// 1 user prompt + 1 API request (token_count closes the assistant content).
	if first.Total != 2 || second.Total != 2 {
		t.Fatalf("Messages totals = %d/%d, want stable 2 fallback rows", first.Total, second.Total)
	}
	for i := range first.Messages {
		if !strings.HasPrefix(first.Messages[i].ID, "codex:fallback-session:turn:") {
			t.Errorf("fallback ID = %q, want codex:fallback-session:turn:*", first.Messages[i].ID)
		}
		if first.Messages[i].ID != second.Messages[i].ID {
			t.Errorf("fallback ID changed between reads: %q vs %q", first.Messages[i].ID, second.Messages[i].ID)
		}
	}
}

func TestNormalizerDoesNotPromoteReplayUserDeveloperRows(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)

	// Legitimate user_message events DO produce user rows (u0, u1); only the
	// replayed response_item role=user / developer / compacted rows must not leak.
	userRows := 0
	for _, msg := range messages.Messages {
		if strings.Contains(msg.ID, "msg-user-replay") {
			t.Errorf("replayed response_item user row leaked: %#v", msg)
		}
		if msg.Role == "user" {
			userRows++
		}
	}
	if userRows != 2 {
		t.Errorf("user rows = %d, want exactly 2 legitimate user_message rows", userRows)
	}
	if messages.Total != 4 {
		t.Errorf("Messages total = %d, want 4 per-request rows after compaction/replay filtering", messages.Total)
	}
	// No replayed/compacted content may leak into any row detail.
	for _, msg := range messages.Messages {
		detail := mustMessageDetail(t, src, msg.ID)
		assertJSONDoesNotContain(t, detail, "[REDACTED_REPLAYED_USER_MESSAGE]", "[REDACTED_DEVELOPER_REPLAY]", "[REDACTED_USER_REPLAY]", "[REDACTED_ASSISTANT_REPLAY]")
	}
}

func TestNormalizerMissingOptionalFieldsRemainUnknownNotZeroSpend(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T06-00-00Z-partial-session.jsonl": {
			`{"timestamp":"2026-01-02T06:00:00Z","type":"session_meta","payload":{"id":"partial-session"}}`,
			`{"timestamp":"2026-01-02T06:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"partial-turn"}}`,
			`{"timestamp":"2026-01-02T06:00:02Z","type":"event_msg","payload":{"type":"user_message","turn_id":"partial-turn","message":"[REDACTED_PARTIAL_PROMPT]"}}`,
			`{"timestamp":"2026-01-02T06:00:03Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"partial-turn"}}`,
		},
	})
	messages := readAllMessages(t, src)
	if messages.Total != 1 || len(messages.Messages) != 1 {
		t.Fatalf("Messages total/len = %d/%d, want 1 partial interaction", messages.Total, len(messages.Messages))
	}
	entry := messages.Messages[0]
	if entry.ModelID != "" || entry.ProviderID != "" {
		t.Errorf("model/provider = %q/%q, want empty unknown optional fields", entry.ModelID, entry.ProviderID)
	}
	if entry.CostStatus != stats.CostMissing {
		t.Errorf("CostStatus = %q, want %q for missing token/model pricing", entry.CostStatus, stats.CostMissing)
	}
	if entry.Cost != 0 {
		t.Errorf("Cost = %.9f, want zero compatibility value for missing/unknown cost only", entry.Cost)
	}
}
