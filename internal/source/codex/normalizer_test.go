package codex

import (
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestNormalizerEmitsExactlyOneInteractionPerTurnID(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)

	if messages.SourceID != string(source.SourceCodex) {
		t.Errorf("MessageList.SourceID = %q, want %q", messages.SourceID, source.SourceCodex)
	}
	if messages.Total != 1 || len(messages.Messages) != 1 {
		t.Fatalf("Messages total/len = %d/%d, want exactly one grouped Codex interaction for turn-1", messages.Total, len(messages.Messages))
	}

	entry := messages.Messages[0]
	assertCodexSourceID(t, entry.SourceID)
	if entry.ID != "codex:synthetic-session:turn-1" {
		t.Errorf("Message ID = %q, want codex:synthetic-session:turn-1", entry.ID)
	}
	if entry.SessionID != "synthetic-session" {
		t.Errorf("SessionID = %q, want synthetic-session", entry.SessionID)
	}
	if entry.Role != "assistant" {
		t.Errorf("Role = %q, want assistant after folded assistant output", entry.Role)
	}
	if entry.ModelID != "gpt-5.5" || entry.ProviderID != "openai" {
		t.Errorf("model/provider = %q/%q, want gpt-5.5/openai", entry.ModelID, entry.ProviderID)
	}
	if entry.FoldedAssistantCalls != 1 {
		t.Errorf("FoldedAssistantCalls = %d, want 1 deduped assistant message", entry.FoldedAssistantCalls)
	}
	if entry.FoldedToolCalls != 3 {
		t.Errorf("FoldedToolCalls = %d, want 3 deduped tool/search calls", entry.FoldedToolCalls)
	}
	if entry.FoldedTokenUpdates != 4 {
		t.Errorf("FoldedTokenUpdates = %d, want 4 token_count rows folded into one interaction", entry.FoldedTokenUpdates)
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

	detail := mustMessageDetail(t, src, messages.Messages[0].ID)
	if !detailTextContains(detail, "[REDACTED_USER_MESSAGE_PART_1]") || !detailTextContains(detail, "[REDACTED_USER_MESSAGE_PART_2]") {
		t.Errorf("detail text parts = %#v, want both user_message rows folded into one interaction", detail.Content.TextParts)
	}
	if detailTextOccurrences(detail, "[REDACTED_ASSISTANT_SUMMARY]") != 1 {
		t.Errorf("assistant mirror occurrences = %d, want exactly 1 after response_item/agent_message dedupe", detailTextOccurrences(detail, "[REDACTED_ASSISTANT_SUMMARY]"))
	}
	if len(detail.Content.ReasoningParts) != 1 {
		t.Errorf("ReasoningParts len = %d, want 1 placeholder/count for redacted reasoning event", len(detail.Content.ReasoningParts))
	}
	if len(detail.Content.ToolParts) != 3 {
		t.Errorf("ToolParts len = %d, want 3 folded tool/search calls", len(detail.Content.ToolParts))
	}
	assertJSONDoesNotContain(t, detail, "[REDACTED_REPLAYED_USER_MESSAGE]", "[REDACTED_DEVELOPER_REPLAY]", "[REDACTED_USER_REPLAY]", "[REDACTED_ASSISTANT_REPLAY]")
}

func TestNormalizerRepeatedTurnContextUpdatesMetadataOnly(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)
	entry := messages.Messages[0]

	if messages.Total != 1 {
		t.Fatalf("Messages total = %d, want 1 despite repeated turn_context rows", messages.Total)
	}
	if entry.ModelID != "gpt-5.5" {
		t.Errorf("ModelID = %q, want updated turn_context model gpt-5.5", entry.ModelID)
	}
	if entry.SessionTitle == "" {
		t.Errorf("SessionTitle is empty, want folded redacted user prompt/title")
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
	if first.Total != 1 || second.Total != 1 {
		t.Fatalf("Messages totals = %d/%d, want stable one fallback interaction", first.Total, second.Total)
	}
	if !strings.HasPrefix(first.Messages[0].ID, "codex:fallback-session:fallback:") {
		t.Errorf("fallback ID = %q, want codex:fallback-session:fallback:*", first.Messages[0].ID)
	}
	if first.Messages[0].ID != second.Messages[0].ID {
		t.Errorf("fallback ID changed between reads: %q vs %q", first.Messages[0].ID, second.Messages[0].ID)
	}
}

func TestNormalizerDoesNotPromoteReplayUserDeveloperRows(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)

	for _, msg := range messages.Messages {
		if msg.Role == "user" || strings.Contains(msg.ID, "msg-user-replay") {
			t.Errorf("top-level replay/raw user row leaked: %#v", msg)
		}
	}
	if messages.Total != 1 {
		t.Errorf("Messages total = %d, want one interaction after compaction/replay filtering", messages.Total)
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
