package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestParserRecognizesCodexTopLevelRecordFamilies(t *testing.T) {
	file := transcriptFile{Path: "synthetic-rollout.jsonl", SessionID: "fallback-session"}

	tests := []struct {
		name   string
		line   string
		assert func(t *testing.T, record codexRecord)
	}{
		{
			name: "session metadata preserves non-sensitive source fields",
			line: `{"timestamp":"2026-01-02T03:04:05Z","type":"session_meta","payload":{"id":"synthetic-session","cli_version":"codex-synthetic-1.0.0","source":"codex_cli","model_provider":"openai","thread_source":"local_jsonl","cwd":"[REDACTED_PATH]/synthetic-project","instructions":"[REDACTED_INSTRUCTIONS]"}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.TopType != "session_meta" {
					t.Errorf("TopType = %q, want session_meta", record.TopType)
				}
				if record.SessionMeta == nil {
					t.Fatalf("SessionMeta = nil")
				}
				if record.SessionMeta.ID != "synthetic-session" {
					t.Errorf("SessionMeta.ID = %q, want synthetic-session", record.SessionMeta.ID)
				}
				if record.SessionMeta.ModelProvider != "openai" {
					t.Errorf("SessionMeta.ModelProvider = %q, want openai", record.SessionMeta.ModelProvider)
				}
			},
		},
		{
			name: "turn context attaches model metadata by turn id",
			line: `{"timestamp":"2026-01-02T03:04:06Z","type":"turn_context","payload":{"turn_id":"turn-1","model":"gpt-5.5","model_provider":"openai","cwd":"[REDACTED_PATH]/synthetic-project","approval_policy":"on-request","sandbox_policy":"workspace-write","model_context_window":300000}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.TurnContext == nil {
					t.Fatalf("TurnContext = nil")
				}
				if record.TurnContext.TurnID != "turn-1" || record.TurnContext.Model != "gpt-5.5" {
					t.Errorf("TurnContext turn/model = %q/%q, want turn-1/gpt-5.5", record.TurnContext.TurnID, record.TurnContext.Model)
				}
				if record.TurnContext.ModelContextWindow != 300000 {
					t.Errorf("ModelContextWindow = %d, want 300000", record.TurnContext.ModelContextWindow)
				}
			},
		},
		{
			name: "event message task started has lifecycle type and turn id",
			line: `{"timestamp":"2026-01-02T03:04:07Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1"}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil {
					t.Fatalf("Event = nil")
				}
				if record.Event.PayloadType != "task_started" || record.Event.TurnID != "turn-1" {
					t.Errorf("Event payload/turn = %q/%q, want task_started/turn-1", record.Event.PayloadType, record.Event.TurnID)
				}
			},
		},
		{
			name: "event message user message preserves redacted text for folding",
			line: `{"timestamp":"2026-01-02T03:04:08Z","type":"event_msg","payload":{"type":"user_message","turn_id":"turn-1","message":"[REDACTED_USER_MESSAGE_PART_1]"}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil || record.Event.PayloadType != "user_message" {
					t.Fatalf("Event = %#v, want user_message", record.Event)
				}
				if !strings.Contains(record.Event.Text, "[REDACTED_USER_MESSAGE_PART_1]") {
					t.Errorf("Event.Text = %q, want redacted user text", record.Event.Text)
				}
			},
		},
		{
			name: "event message token count preserves cumulative usage and rate-limit plan",
			line: `{"timestamp":"2026-01-02T03:04:19Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-1","info":{"last_token_usage":{"input_tokens":999,"cached_input_tokens":888,"output_tokens":777,"reasoning_output_tokens":666,"total_tokens":3330},"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":25,"total_tokens":1075},"model_context_window":300000,"rate_limits":{"plan_type":"plus"}}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil || record.Event.PayloadType != "token_count" {
					t.Fatalf("Event = %#v, want token_count", record.Event)
				}
				if record.Event.TotalUsage.Input != 1000 || record.Event.TotalUsage.Cached != 100 || record.Event.TotalUsage.Output != 50 || record.Event.TotalUsage.Reasoning != 25 {
					t.Errorf("TotalUsage = %#v, want input/cache/output/reasoning 1000/100/50/25", record.Event.TotalUsage)
				}
				if record.Event.LastUsage.Input != 999 {
					t.Errorf("LastUsage.Input = %d, want last_token_usage preserved but not aggregated", record.Event.LastUsage.Input)
				}
				if !record.Event.HasTotalUsage || !record.Event.HasLastUsage {
					t.Errorf("HasTotalUsage/HasLastUsage = %v/%v, want both true when both snapshots present", record.Event.HasTotalUsage, record.Event.HasLastUsage)
				}
				if record.Event.PlanType != "plus" {
					t.Errorf("PlanType = %q, want plus", record.Event.PlanType)
				}
			},
		},
		{
			name: "event message token count with only last usage marks total absent",
			line: `{"timestamp":"2026-01-02T03:04:19Z","type":"event_msg","payload":{"type":"token_count","turn_id":"turn-1","info":{"last_token_usage":{"input_tokens":200,"cached_input_tokens":10,"output_tokens":40,"reasoning_output_tokens":5,"total_tokens":255}}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil || record.Event.PayloadType != "token_count" {
					t.Fatalf("Event = %#v, want token_count", record.Event)
				}
				if record.Event.HasTotalUsage {
					t.Errorf("HasTotalUsage = true, want false when total_token_usage is absent")
				}
				if !record.Event.HasLastUsage || record.Event.LastUsage.Input != 200 {
					t.Errorf("LastUsage = %#v / HasLastUsage = %v, want last_token_usage fallback present", record.Event.LastUsage, record.Event.HasLastUsage)
				}
			},
		},
		{
			name: "event message context compacted is parsed as metadata",
			line: `{"timestamp":"2026-01-02T03:04:23Z","type":"event_msg","payload":{"type":"context_compacted","turn_id":"turn-1","summary":"[REDACTED_COMPACTION_SUMMARY]"}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil || record.Event.PayloadType != "context_compacted" || record.Event.TurnID != "turn-1" {
					t.Fatalf("Event = %#v, want context_compacted metadata for turn-1", record.Event)
				}
				if !record.Event.Compaction {
					t.Errorf("Event.Compaction = false, want true for context_compacted")
				}
			},
		},
		{
			name: "event message tool status preserves call identity without becoming row",
			line: `{"timestamp":"2026-01-02T03:04:17Z","type":"event_msg","payload":{"type":"patch_apply_end","turn_id":"turn-1","call_id":"call-apply-patch-1","status":"success","changed_files":1,"paths":["[REDACTED_PATH]/synthetic.go"]}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil || record.Event.PayloadType != "patch_apply_end" || record.Event.CallID != "call-apply-patch-1" {
					t.Fatalf("Event = %#v, want patch_apply_end status for call-apply-patch-1", record.Event)
				}
				if record.Event.Status != "success" {
					t.Errorf("Event.Status = %q, want success", record.Event.Status)
				}
			},
		},
		{
			name: "response item assistant message is parsed as folded detail candidate",
			line: `{"timestamp":"2026-01-02T03:04:11Z","type":"response_item","payload":{"turn_id":"turn-1","item":{"type":"message","id":"msg-assistant-1","role":"assistant","content":[{"type":"output_text","text":"[REDACTED_ASSISTANT_SUMMARY]"}]}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Response == nil {
					t.Fatalf("Response = nil")
				}
				if record.Response.ItemType != "message" || record.Response.Role != "assistant" {
					t.Errorf("Response item/role = %q/%q, want message/assistant", record.Response.ItemType, record.Response.Role)
				}
			},
		},
		{
			name: "response item reasoning is parsed without exposing encrypted content as text",
			line: `{"timestamp":"2026-01-02T03:04:13Z","type":"response_item","payload":{"turn_id":"turn-1","item":{"type":"reasoning","id":"rsn-1","summary":[],"encrypted_content":"[REDACTED_ENCRYPTED_REASONING]"}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Response == nil || record.Response.ItemType != "reasoning" {
					t.Fatalf("Response = %#v, want reasoning", record.Response)
				}
				if strings.Contains(record.Response.Text, "ENCRYPTED") {
					t.Errorf("Response.Text = %q, want encrypted reasoning omitted/redacted", record.Response.Text)
				}
			},
		},
		{
			name: "response item function call preserves call identity and tool name",
			line: `{"timestamp":"2026-01-02T03:04:14Z","type":"response_item","payload":{"turn_id":"turn-1","item":{"type":"function_call","id":"fc-1","call_id":"call-shell-1","name":"shell","arguments":"[REDACTED_TOOL_ARGS]"}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Response == nil || record.Response.CallID != "call-shell-1" || record.Response.ToolName != "shell" {
					t.Fatalf("Response = %#v, want shell call call-shell-1", record.Response)
				}
			},
		},
		{
			name: "response item function output preserves matching call identity",
			line: `{"timestamp":"2026-01-02T03:04:15Z","type":"response_item","payload":{"turn_id":"turn-1","item":{"type":"function_call_output","call_id":"call-shell-1","output":"[REDACTED_TOOL_OUTPUT]"}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Response == nil || record.Response.ItemType != "function_call_output" || record.Response.CallID != "call-shell-1" {
					t.Fatalf("Response = %#v, want function_call_output for call-shell-1", record.Response)
				}
			},
		},
		{
			name: "response item custom tool call preserves tool identity",
			line: `{"timestamp":"2026-01-02T03:04:16Z","type":"response_item","payload":{"turn_id":"turn-1","item":{"type":"custom_tool_call","id":"ctc-1","call_id":"call-apply-patch-1","name":"apply_patch","input":"[REDACTED_PATCH]"}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Response == nil || record.Response.ItemType != "custom_tool_call" || record.Response.ToolName != "apply_patch" || record.Response.CallID != "call-apply-patch-1" {
					t.Fatalf("Response = %#v, want apply_patch custom tool call", record.Response)
				}
			},
		},
		{
			name: "response item web search call preserves search identity",
			line: `{"timestamp":"2026-01-02T03:04:18Z","type":"response_item","payload":{"turn_id":"turn-1","item":{"type":"web_search_call","id":"search-1","call_id":"call-search-1","action":{"type":"search","query":"[REDACTED_SEARCH_QUERY]"},"status":"completed"}}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Response == nil || record.Response.ItemType != "web_search_call" || record.Response.CallID != "call-search-1" {
					t.Fatalf("Response = %#v, want web_search_call call-search-1", record.Response)
				}
				if record.Response.Status != "completed" {
					t.Errorf("Response.Status = %q, want completed", record.Response.Status)
				}
			},
		},
		{
			name: "event message task complete has lifecycle status",
			line: `{"timestamp":"2026-01-02T03:04:25Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","status":"success"}}`,
			assert: func(t *testing.T, record codexRecord) {
				if record.Event == nil || record.Event.PayloadType != "task_complete" || record.Event.Status != "success" {
					t.Fatalf("Event = %#v, want task_complete success", record.Event)
				}
			},
		},
		{
			name: "compacted record is recognized as compaction metadata",
			line: `{"timestamp":"2026-01-02T03:04:24Z","type":"compacted","payload":{"turn_id":"turn-1","replacement_history":[{"role":"developer","content":"[REDACTED_DEVELOPER_REPLAY]"}]}}`,
			assert: func(t *testing.T, record codexRecord) {
				if !record.Compacted || record.TopType != "compacted" {
					t.Errorf("Compacted/TopType = %v/%q, want true/compacted", record.Compacted, record.TopType)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, ok, malformed := parseLine(file, 1, tt.line)
			if malformed {
				t.Fatalf("parseLine() malformed = true")
			}
			if !ok {
				t.Fatalf("parseLine() ok = false")
			}
			tt.assert(t, record)
		})
	}
}

func TestParserToleratesMalformedUnknownAndPartialData(t *testing.T) {
	file := transcriptFile{Path: syntheticRolloutPath(t), SessionID: "synthetic-session"}
	records, diag, err := parseTranscriptFile(testContext(t), file)
	if err != nil {
		t.Fatalf("parseTranscriptFile() failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("parseTranscriptFile() returned no records, want valid records around malformed/unknown lines")
	}
	if diag.MalformedLines != 1 {
		t.Errorf("MalformedLines = %d, want 1", diag.MalformedLines)
	}
	if diag.UnsupportedEvents != 1 {
		t.Errorf("UnsupportedEvents = %d, want 1 unknown top-level record", diag.UnsupportedEvents)
	}

	record, ok, malformed := parseLine(file, 99, `{"timestamp":"2026-01-02T03:04:07Z","type":"event_msg","payload":{"type":"task_started"}}`)
	if malformed || !ok {
		t.Fatalf("partial task_started parse ok/malformed = %v/%v, want true/false", ok, malformed)
	}
	if record.Event == nil || record.Event.TurnID != "" {
		t.Errorf("partial Event = %#v, want missing turn_id represented as empty", record.Event)
	}
}

func TestParserRejectsMalformedAndUnknownRowsWithoutRawPayloadLeak(t *testing.T) {
	file := transcriptFile{Path: "synthetic-rollout.jsonl", SessionID: "fallback-session"}

	if _, ok, malformed := parseLine(file, 1, `{this is not valid json}`); !malformed || ok {
		t.Fatalf("malformed parse ok/malformed = %v/%v, want false/true", ok, malformed)
	}
	if _, ok, malformed := parseLine(file, 2, `{"timestamp":"2026-01-02T03:04:27Z","type":"unknown_top_level","payload":{"secret":"SYNTHETIC_UNKNOWN_SECRET_MUST_NOT_LEAK"}}`); malformed || ok {
		t.Fatalf("unknown parse ok/malformed = %v/%v, want false/false", ok, malformed)
	}

	src := newFixtureSource(t, "valid_home")
	if _, err := src.Messages(testContext(t), stats.PeriodQuery{Period: "all"}, 1, 50, stats.DefaultMessageSort()); err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	info := src.Info(testContext(t))
	if info.ID != source.SourceCodex {
		t.Errorf("Info().ID = %q, want %q", info.ID, source.SourceCodex)
	}
	encoded, err := json.Marshal(info.Diagnostics)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	for _, forbidden := range []string{"this is malformed synthetic", "SYNTHETIC_UNKNOWN_SECRET_MUST_NOT_LEAK", "[REDACTED_UNKNOWN_PAYLOAD]"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("diagnostics leaked raw payload %q in %s", forbidden, string(encoded))
		}
	}
}
