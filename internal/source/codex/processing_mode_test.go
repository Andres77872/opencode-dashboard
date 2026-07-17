package codex

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestNormalizeProcessingMode(t *testing.T) {
	tests := []struct {
		serviceTier string
		want        stats.ProcessingMode
	}{
		{serviceTier: "priority", want: stats.ProcessingModeFast},
		{serviceTier: "fast", want: stats.ProcessingModeFast},
		{serviceTier: " PRIORITY ", want: stats.ProcessingModeFast},
		{serviceTier: "default", want: stats.ProcessingModeStandard},
		{serviceTier: "standard", want: stats.ProcessingModeStandard},
		{serviceTier: "flex", want: stats.ProcessingModeFlex},
		{serviceTier: "", want: stats.ProcessingModeUnknown},
		{serviceTier: "experimental", want: stats.ProcessingModeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.serviceTier, func(t *testing.T) {
			if got := normalizeProcessingMode(tt.serviceTier); got != tt.want {
				t.Errorf("normalizeProcessingMode(%q) = %q, want %q", tt.serviceTier, got, tt.want)
			}
		})
	}
}

func TestCodexProcessingModeTracksEachAssistantRequestBoundary(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/07/17/rollout-2026-07-17T09-00-00Z-tier-session.jsonl": {
			`{"timestamp":"2026-07-17T09:00:00Z","type":"session_meta","payload":{"id":"tier-session","model_provider":"openai","cwd":"[REDACTED_PATH]/tier-project"}}`,
			`{"timestamp":"2026-07-17T09:00:01Z","type":"turn_context","payload":{"turn_id":"tier-turn","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T09:00:02Z","type":"event_msg","payload":{"type":"user_message","turn_id":"tier-turn","message":"TIER TEST"}}`,
			// No marker exists for r0, so the request must remain explicitly unknown.
			`{"timestamp":"2026-07-17T09:00:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"tier-turn","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}`,
			`{"timestamp":"2026-07-17T09:00:04Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}`,
			`{"timestamp":"2026-07-17T09:00:05Z","type":"event_msg","payload":{"type":"token_count","turn_id":"tier-turn","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":30,"output_tokens":50,"reasoning_output_tokens":10,"total_tokens":350}}}}`,
			// The change applies to the immediately following request boundary.
			`{"timestamp":"2026-07-17T09:00:06Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"default"}}}`,
			`{"timestamp":"2026-07-17T09:00:07Z","type":"event_msg","payload":{"type":"token_count","turn_id":"tier-turn","info":{"total_token_usage":{"input_tokens":600,"cached_input_tokens":60,"output_tokens":90,"reasoning_output_tokens":20,"total_tokens":690}}}}`,
			// Preserve an unknown raw value while normalizing it conservatively.
			`{"timestamp":"2026-07-17T09:00:08Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"experimental"}}}`,
			`{"timestamp":"2026-07-17T09:00:09Z","type":"event_msg","payload":{"type":"token_count","turn_id":"tier-turn","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":140,"reasoning_output_tokens":30,"total_tokens":1140}}}}`,
			`{"timestamp":"2026-07-17T09:00:10Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"flex"}}}`,
			`{"timestamp":"2026-07-17T09:00:11Z","type":"event_msg","payload":{"type":"token_count","turn_id":"tier-turn","info":{"total_token_usage":{"input_tokens":1500,"cached_input_tokens":150,"output_tokens":200,"reasoning_output_tokens":40,"total_tokens":1700}}}}`,
		},
	})

	messages := readAllMessages(t, src)
	if messages.Total != 6 {
		t.Fatalf("Messages.Total = %d, want 1 user + 5 assistant requests", messages.Total)
	}
	wants := map[string]struct {
		serviceTier string
		mode        stats.ProcessingMode
		input       int64
	}{
		"codex:tier-session:tier-turn:r0": {mode: stats.ProcessingModeUnknown, input: 90},
		"codex:tier-session:tier-turn:r1": {serviceTier: "priority", mode: stats.ProcessingModeFast, input: 180},
		"codex:tier-session:tier-turn:r2": {serviceTier: "default", mode: stats.ProcessingModeStandard, input: 270},
		"codex:tier-session:tier-turn:r3": {serviceTier: "experimental", mode: stats.ProcessingModeUnknown, input: 360},
		"codex:tier-session:tier-turn:r4": {serviceTier: "flex", mode: stats.ProcessingModeFlex, input: 450},
	}
	for _, msg := range messages.Messages {
		if msg.Role == "user" {
			if msg.ServiceTier != "" || msg.ProcessingMode != "" {
				t.Errorf("user row tier/mode = %q/%q, want both omitted", msg.ServiceTier, msg.ProcessingMode)
			}
			continue
		}
		want, ok := wants[msg.ID]
		if !ok {
			t.Errorf("unexpected assistant message %q", msg.ID)
			continue
		}
		if msg.ServiceTier != want.serviceTier || msg.ProcessingMode != want.mode {
			t.Errorf("message %q tier/mode = %q/%q, want %q/%q", msg.ID, msg.ServiceTier, msg.ProcessingMode, want.serviceTier, want.mode)
		}
		if msg.Tokens == nil || msg.Tokens.Input != want.input {
			t.Errorf("message %q input tokens = %+v, want disjoint input %d", msg.ID, msg.Tokens, want.input)
		}
	}

	session, err := src.SessionByID(testContext(t), "tier-session")
	if err != nil {
		t.Fatalf("SessionByID(tier-session) failed: %v", err)
	}
	if session == nil {
		t.Fatal("SessionByID(tier-session) = nil")
	}
	for _, msg := range session.Messages {
		want, ok := wants[msg.ID]
		if !ok {
			continue
		}
		if msg.ServiceTier != want.serviceTier || msg.ProcessingMode != want.mode {
			t.Errorf("session message %q tier/mode = %q/%q, want %q/%q", msg.ID, msg.ServiceTier, msg.ProcessingMode, want.serviceTier, want.mode)
		}
	}

	dimension, err := src.DailyDimension(testContext(t), "processing_mode", stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("DailyDimension(processing_mode) failed: %v", err)
	}
	if dimension.Dimension != "processing_mode" {
		t.Errorf("DailyDimension.Dimension = %q, want processing_mode", dimension.Dimension)
	}
	type modeAggregate struct {
		messages int64
		input    int64
	}
	gotModes := make(map[string]modeAggregate)
	for _, day := range dimension.Days {
		gotModes[day.Dimension] = modeAggregate{messages: day.Messages, input: day.Tokens.Input}
	}
	wantModes := map[string]modeAggregate{
		"unknown":  {messages: 2, input: 450},
		"fast":     {messages: 1, input: 180},
		"standard": {messages: 1, input: 270},
		"flex":     {messages: 1, input: 450},
	}
	for mode, want := range wantModes {
		if got := gotModes[mode]; got != want {
			t.Errorf("processing mode %q aggregate = %+v, want %+v", mode, got, want)
		}
	}
}

func TestCodexProcessingModeLabelsContentOnlyAssistantAtFlush(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/07/17/rollout-2026-07-17T10-00-00Z-flush-tier.jsonl": {
			`{"timestamp":"2026-07-17T10:00:00Z","type":"session_meta","payload":{"id":"flush-tier","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T10:00:01Z","type":"turn_context","payload":{"turn_id":"flush-turn","model":"gpt-5.5"}}`,
			`{"timestamp":"2026-07-17T10:00:02Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}`,
			`{"timestamp":"2026-07-17T10:00:03Z","type":"response_item","payload":{"turn_id":"flush-turn","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"content without usage"}]}}}`,
			`{"timestamp":"2026-07-17T10:00:04Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"flush-turn"}}`,
		},
	})
	messages := readAllMessages(t, src)
	if messages.Total != 1 {
		t.Fatalf("Messages.Total = %d, want one content-only assistant row", messages.Total)
	}
	msg := messages.Messages[0]
	if msg.ServiceTier != "priority" || msg.ProcessingMode != stats.ProcessingModeFast {
		t.Errorf("content-only tier/mode = %q/%q, want priority/fast", msg.ServiceTier, msg.ProcessingMode)
	}
	if msg.Tokens != nil {
		t.Errorf("content-only tokens = %+v, want nil", msg.Tokens)
	}
}

func TestCodexProcessingModeDoesNotDoubleCountForkReplay(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/07/17/rollout-2026-07-17T11-00-00Z-tier-parent.jsonl": {
			`{"timestamp":"2026-07-17T11:00:00Z","type":"session_meta","payload":{"id":"tier-parent","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T11:00:01Z","type":"turn_context","payload":{"turn_id":"parent-turn","model":"gpt-5.5"}}`,
			`{"timestamp":"2026-07-17T11:00:02Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}`,
			`{"timestamp":"2026-07-17T11:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"parent-turn","message":"parent prompt"}}`,
			`{"timestamp":"2026-07-17T11:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"parent-turn","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}`,
		},
		"sessions/2026/07/17/rollout-2026-07-17T12-00-00Z-tier-fork.jsonl": {
			`{"timestamp":"2026-07-17T12:00:00Z","type":"session_meta","payload":{"id":"tier-fork","session_id":"tier-parent","forked_from_id":"tier-parent","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T12:00:01Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}`,
			`{"timestamp":"2026-07-17T12:00:02Z","type":"event_msg","payload":{"type":"token_count","turn_id":"parent-turn","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}`,
			`{"timestamp":"2026-07-17T12:00:03Z","type":"turn_context","payload":{"turn_id":"fork-turn","model":"gpt-5.5"}}`,
			`{"timestamp":"2026-07-17T12:00:04Z","type":"event_msg","payload":{"type":"user_message","turn_id":"fork-turn","message":"fork prompt"}}`,
			`{"timestamp":"2026-07-17T12:00:05Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"default"}}}`,
			`{"timestamp":"2026-07-17T12:00:06Z","type":"event_msg","payload":{"type":"token_count","turn_id":"fork-turn","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":30,"output_tokens":50,"reasoning_output_tokens":10,"total_tokens":350}}}}`,
		},
	})

	messages := readAllMessages(t, src)
	parent := findMessage(t, messages, func(msg stats.MessageEntry) bool { return msg.ID == "codex:tier-parent:parent-turn:r0" })
	fork := findMessage(t, messages, func(msg stats.MessageEntry) bool { return msg.ID == "codex:tier-fork:fork-turn:r0" })
	if parent.ServiceTier != "priority" || parent.ProcessingMode != stats.ProcessingModeFast {
		t.Errorf("parent tier/mode = %q/%q, want priority/fast", parent.ServiceTier, parent.ProcessingMode)
	}
	if fork.ServiceTier != "default" || fork.ProcessingMode != stats.ProcessingModeStandard {
		t.Errorf("fork tier/mode = %q/%q, want default/standard", fork.ServiceTier, fork.ProcessingMode)
	}
	if parent.Tokens == nil || parent.Tokens.Input != 90 || fork.Tokens == nil || fork.Tokens.Input != 180 {
		t.Errorf("parent/fork tokens = %+v / %+v, want replay suppressed and inputs 90/180", parent.Tokens, fork.Tokens)
	}
	assistantRows := 0
	for _, msg := range messages.Messages {
		if msg.Role == "assistant" {
			assistantRows++
		}
	}
	if assistantRows != 2 {
		t.Errorf("assistant rows = %d, want 2 (replayed token_count adds no row)", assistantRows)
	}
}
