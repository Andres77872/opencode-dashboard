package claudecode

import (
	"strings"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestClaudeInteractionGroupingMultiToolLoopPromptFoldsRawAssistantRecordsIntoOneDetail(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-grouping/multi-tool-loop-session.jsonl": multiToolLoopDeltaLines(),
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Messages != 1 {
		t.Errorf("Overview().Messages = %d, want 1 user-facing interaction for one prompt with a multi-tool loop", overview.Messages)
	}
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1", overview.Sessions)
	}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 1 {
		t.Errorf("Messages().Total = %d, want 1 dashboard row representing the user prompt, not raw assistant/tool-loop lines", messages.Total)
	}
	if len(messages.Messages) != 1 {
		t.Fatalf("Messages().Messages len = %d, want 1 interaction row to inspect detail", len(messages.Messages))
	}

	detail := mustMessageDetail(t, src, messages.Messages[0].ID)
	assertDetailToolCompleted(t, detail, "toolu_read_grouping", "read completed")
	assertDetailToolCompleted(t, detail, "toolu_grep_grouping", "grep completed")
	if len(detail.Content.ToolParts) != 2 {
		t.Errorf("MessageDetail().Content.ToolParts len = %d, want 2 folded tool calls", len(detail.Content.ToolParts))
	}
	assertDetailTextContains(t, detail, "Both tool calls completed and the answer is ready.")

	session, err := src.SessionByID(ctx, "multi-tool-loop-session")
	if err != nil {
		t.Fatalf("SessionByID(multi-tool-loop-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(multi-tool-loop-session) = nil, want session detail")
	}
	if session.MessageCount != 1 {
		t.Errorf("SessionByID().MessageCount = %d, want 1 user-facing interaction", session.MessageCount)
	}
	if len(session.Messages) != 1 {
		t.Errorf("SessionByID().Messages len = %d, want 1 user-facing interaction", len(session.Messages))
	}
}

func TestClaudeInteractionGroupingCostAndTokenSemantics(t *testing.T) {
	tests := []struct {
		name        string
		sessionID   string
		lines       []string
		wantCost    float64
		wantInput   int64
		wantOutput  int64
		wantMessage string
	}{
		{
			name:        "per-call delta assistant records sum unique API-call deltas once",
			sessionID:   "multi-tool-loop-session",
			lines:       multiToolLoopDeltaLines(),
			wantCost:    7.50,
			wantInput:   600,
			wantOutput:  90,
			wantMessage: "delta assistant API-call records should fold into one interaction while preserving summed cost/tokens",
		},
		{
			name:        "cumulative-looking assistant records use final max cumulative values",
			sessionID:   "cumulative-tool-loop-session",
			lines:       cumulativeToolLoopLines(),
			wantCost:    6.00,
			wantInput:   600,
			wantOutput:  60,
			wantMessage: "cumulative totals must not be linearly overcounted across raw assistant records",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTempRegressionSource(t, map[string][]string{
				"-home-andres-projects-grouping/" + tt.sessionID + ".jsonl": tt.lines,
			})
			ctx := testContext(t)
			period := stats.PeriodQuery{Period: "all"}

			overview, err := src.Overview(ctx, period)
			if err != nil {
				t.Fatalf("Overview(all) failed: %v", err)
			}
			if overview.Messages != 1 {
				t.Errorf("Overview().Messages = %d, want 1 grouped interaction; %s", overview.Messages, tt.wantMessage)
			}
			if !approxEqual(overview.Cost, tt.wantCost) {
				t.Errorf("Overview().Cost = %.6f, want %.6f; %s", overview.Cost, tt.wantCost, tt.wantMessage)
			}
			if overview.Tokens.Input != tt.wantInput || overview.Tokens.Output != tt.wantOutput {
				t.Errorf("Overview().Tokens input/output = %d/%d, want %d/%d; %s", overview.Tokens.Input, overview.Tokens.Output, tt.wantInput, tt.wantOutput, tt.wantMessage)
			}

			messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
			if err != nil {
				t.Fatalf("Messages(all) failed: %v", err)
			}
			if messages.Total != 1 {
				t.Errorf("Messages().Total = %d, want 1 grouped interaction; %s", messages.Total, tt.wantMessage)
			}
			if len(messages.Messages) != 1 {
				t.Fatalf("Messages().Messages len = %d, want 1 interaction row to verify aggregate fields", len(messages.Messages))
			}
			entry := messages.Messages[0]
			if !approxEqual(entry.Cost, tt.wantCost) {
				t.Errorf("Messages()[0].Cost = %.6f, want %.6f; %s", entry.Cost, tt.wantCost, tt.wantMessage)
			}
			if entry.Tokens == nil {
				t.Fatalf("Messages()[0].Tokens = nil, want grouped token totals")
			}
			if entry.Tokens.Input != tt.wantInput || entry.Tokens.Output != tt.wantOutput {
				t.Errorf("Messages()[0].Tokens input/output = %d/%d, want %d/%d; %s", entry.Tokens.Input, entry.Tokens.Output, tt.wantInput, tt.wantOutput, tt.wantMessage)
			}

			session, err := src.SessionByID(ctx, tt.sessionID)
			if err != nil {
				t.Fatalf("SessionByID(%q) failed: %v", tt.sessionID, err)
			}
			if session == nil {
				t.Fatalf("SessionByID(%q) = nil, want session detail", tt.sessionID)
			}
			if session.MessageCount != 1 {
				t.Errorf("SessionByID().MessageCount = %d, want 1 grouped interaction; %s", session.MessageCount, tt.wantMessage)
			}
			if !approxEqual(session.TotalCost, tt.wantCost) {
				t.Errorf("SessionByID().TotalCost = %.6f, want %.6f; %s", session.TotalCost, tt.wantCost, tt.wantMessage)
			}
			if session.TotalTokens.Input != tt.wantInput || session.TotalTokens.Output != tt.wantOutput {
				t.Errorf("SessionByID().TotalTokens input/output = %d/%d, want %d/%d; %s", session.TotalTokens.Input, session.TotalTokens.Output, tt.wantInput, tt.wantOutput, tt.wantMessage)
			}

			models, err := src.Models(ctx, period)
			if err != nil {
				t.Fatalf("Models(all) failed: %v", err)
			}
			model := findModelEntryByID(t, models, "claude-test-computed")
			if model.Messages != 1 {
				t.Errorf("Models()[claude-test-computed].Messages = %d, want 1 grouped interaction; %s", model.Messages, tt.wantMessage)
			}
			if !approxEqual(model.Cost, tt.wantCost) {
				t.Errorf("Models()[claude-test-computed].Cost = %.6f, want %.6f; %s", model.Cost, tt.wantCost, tt.wantMessage)
			}
			if model.Tokens.Input != tt.wantInput || model.Tokens.Output != tt.wantOutput {
				t.Errorf("Models()[claude-test-computed].Tokens input/output = %d/%d, want %d/%d; %s", model.Tokens.Input, model.Tokens.Output, tt.wantInput, tt.wantOutput, tt.wantMessage)
			}
		})
	}
}

func TestClaudeInteractionGroupingFiltersInternalMetadataRows(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-grouping/metadata-session.jsonl": metadataNoiseLines(),
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Messages != 1 {
		t.Errorf("Overview().Messages = %d, want 1 user-facing interaction; summary/progress/isMeta records must not become UNKNOWN $0.00 rows", overview.Messages)
	}
	if !approxEqual(overview.Cost, 0.42) {
		t.Errorf("Overview().Cost = %.6f, want 0.420000 from the assistant interaction only", overview.Cost)
	}
	if overview.Tokens.Input != 42 || overview.Tokens.Output != 7 {
		t.Errorf("Overview().Tokens input/output = %d/%d, want 42/7 from the assistant interaction only", overview.Tokens.Input, overview.Tokens.Output)
	}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 1 {
		t.Errorf("Messages().Total = %d, want 1 user-facing interaction with internal metadata filtered", messages.Total)
	}
	for _, msg := range messages.Messages {
		if msg.Role == "unknown" {
			t.Errorf("Messages() contains unknown metadata row %#v; internal metadata must be filtered before top-level message creation", msg)
		}
	}

	session, err := src.SessionByID(ctx, "metadata-session")
	if err != nil {
		t.Fatalf("SessionByID(metadata-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(metadata-session) = nil, want session detail")
	}
	if session.MessageCount != 1 {
		t.Errorf("SessionByID().MessageCount = %d, want 1 user-facing interaction with internal metadata filtered", session.MessageCount)
	}
	for _, msg := range session.Messages {
		if msg.Role == "unknown" {
			t.Errorf("SessionByID().Messages contains unknown metadata row %#v; internal metadata must not appear as top-level session messages", msg)
		}
	}
}

func TestClaudeInteractionGroupingSkipsIsMetaUserRowsAfterToolResultPairing(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-grouping/is-meta-tool-result-session.jsonl": isMetaInterleavedToolResultLines(),
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Messages != 1 {
		t.Errorf("Overview().Messages = %d, want 1 legitimate interaction; isMeta:true user-shaped rows must not create or steal interactions", overview.Messages)
	}
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1", overview.Sessions)
	}
	if !approxEqual(overview.Cost, 5) {
		t.Errorf("Overview().Cost = %.6f, want 5.000000 folded onto the real prompt", overview.Cost)
	}
	if overview.Tokens.Input != 50 || overview.Tokens.Output != 15 {
		t.Errorf("Overview().Tokens input/output = %d/%d, want 50/15 from both assistant calls folded into the real prompt", overview.Tokens.Input, overview.Tokens.Output)
	}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 1 {
		t.Errorf("Messages().Total = %d, want 1 legitimate interaction; metadata prompt text must not become a row", messages.Total)
	}
	assertJSONDoesNotContain(t, messages, "METADATA PROMPT-LIKE TEXT")
	for _, msg := range messages.Messages {
		if msg.Role == "unknown" {
			t.Errorf("Messages() contains unknown metadata row %#v", msg)
		}
	}

	entry := findMessage(t, messages, func(msg stats.MessageEntry) bool {
		return msg.ID == "claude_code:is-meta-tool-result-session:is-meta-real-user"
	})
	if !approxEqual(entry.Cost, 5) {
		t.Errorf("real prompt Cost = %.6f, want 5.000000; assistant cost must not be stolen by metadata row", entry.Cost)
	}
	if entry.Tokens == nil {
		t.Fatalf("real prompt Tokens = nil, want folded assistant token totals")
	}
	if entry.Tokens.Input != 50 || entry.Tokens.Output != 15 {
		t.Errorf("real prompt Tokens input/output = %d/%d, want 50/15", entry.Tokens.Input, entry.Tokens.Output)
	}
	if entry.FoldedAssistantCalls != 2 {
		t.Errorf("FoldedAssistantCalls = %d, want 2 assistant records folded into one interaction", entry.FoldedAssistantCalls)
	}
	if entry.FoldedToolCalls != 1 {
		t.Errorf("FoldedToolCalls = %d, want 1 tool use folded into one interaction", entry.FoldedToolCalls)
	}

	detail := mustMessageDetail(t, src, entry.ID)
	assertDetailToolCompleted(t, detail, "toolu_is_meta_read", "metadata tool result completed")
	assertDetailTextContains(t, detail, "Final answer stays attached to the legitimate prompt.")
	if detailContainsText(detail, "METADATA PROMPT-LIKE TEXT") {
		t.Errorf("MessageDetail(%q) leaked metadata prompt-like text in %#v", detail.ID, detail.Content.TextParts)
	}
	if detail.FoldedAssistantCalls != 2 {
		t.Errorf("detail FoldedAssistantCalls = %d, want 2", detail.FoldedAssistantCalls)
	}
	if detail.FoldedToolCalls != 1 {
		t.Errorf("detail FoldedToolCalls = %d, want 1", detail.FoldedToolCalls)
	}

	session, err := src.SessionByID(ctx, "is-meta-tool-result-session")
	if err != nil {
		t.Fatalf("SessionByID(is-meta-tool-result-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(is-meta-tool-result-session) = nil, want session detail")
	}
	if session.MessageCount != 1 {
		t.Errorf("SessionByID().MessageCount = %d, want 1 legitimate interaction", session.MessageCount)
	}
	if !approxEqual(session.TotalCost, 5) {
		t.Errorf("SessionByID().TotalCost = %.6f, want 5.000000 folded onto the real prompt", session.TotalCost)
	}
	assertJSONDoesNotContain(t, session, "METADATA PROMPT-LIKE TEXT")
}

func TestClaudeInteractionGroupingHybridToolResultTextStartsNextPrompt(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-grouping/hybrid-tool-result-session.jsonl": hybridToolResultTextLines(),
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Messages != 2 {
		t.Errorf("Overview().Messages = %d, want 2 user prompts/interactions; hybrid tool_result+text must not add a raw metadata row", overview.Messages)
	}
	if !approxEqual(overview.Cost, 12) {
		t.Errorf("Overview().Cost = %.6f, want 12.000000 from the two assistant API calls", overview.Cost)
	}
	if overview.Tokens.Input != 170 || overview.Tokens.Output != 35 {
		t.Errorf("Overview().Tokens input/output = %d/%d, want 170/35", overview.Tokens.Input, overview.Tokens.Output)
	}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 2 {
		t.Errorf("Messages().Total = %d, want 2 user prompts/interactions; hybrid tool_result+text should split into tool detail plus next prompt", messages.Total)
	}
	if len(messages.Messages) != 2 {
		t.Fatalf("Messages().Messages len = %d, want 2 interactions to inspect details", len(messages.Messages))
	}

	details := messageDetails(t, src, messages.Messages)
	if !detailsContainCompletedTool(t, details, "toolu_hybrid_inspect", "hybrid tool completed") {
		t.Errorf("Message details do not contain completed paired tool_result for %q", "toolu_hybrid_inspect")
	}
	if !detailsContainText(details, "Now summarize the inspection result.") {
		t.Errorf("Message details do not contain the hybrid text as the next user prompt/interaction")
	}
	if !detailsContainText(details, "Inspection summary is ready.") {
		t.Errorf("Message details do not contain the following assistant response text")
	}

	session, err := src.SessionByID(ctx, "hybrid-tool-result-session")
	if err != nil {
		t.Fatalf("SessionByID(hybrid-tool-result-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(hybrid-tool-result-session) = nil, want session detail")
	}
	if session.MessageCount != 2 {
		t.Errorf("SessionByID().MessageCount = %d, want 2 user prompts/interactions", session.MessageCount)
	}
}

func multiToolLoopDeltaLines() []string {
	return []string{
		`{"type":"user","uuid":"grouping-user-1","session_id":"multi-tool-loop-session","timestamp":"2026-03-01T10:00:00Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":"Inspect the repo with multiple tools and give me one answer."}}`,
		`{"type":"assistant","uuid":"grouping-assistant-tool-1","session_id":"multi-tool-loop-session","timestamp":"2026-03-01T10:00:01Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_read_grouping","name":"Read","input":{"file_path":"README.md"}}],"usage":{"input_tokens":100,"output_tokens":20}},"costUSD":1.25}`,
		`{"type":"user","uuid":"grouping-tool-result-1","session_id":"multi-tool-loop-session","timestamp":"2026-03-01T10:00:02Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read_grouping","content":"read completed","is_error":false}]}}`,
		`{"type":"assistant","uuid":"grouping-assistant-tool-2","session_id":"multi-tool-loop-session","timestamp":"2026-03-01T10:00:03Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_grep_grouping","name":"Grep","input":{"pattern":"TODO"}}],"usage":{"input_tokens":200,"output_tokens":30}},"costUSD":2.50}`,
		`{"type":"user","uuid":"grouping-tool-result-2","session_id":"multi-tool-loop-session","timestamp":"2026-03-01T10:00:04Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_grep_grouping","content":"grep completed","is_error":false}]}}`,
		`{"type":"assistant","uuid":"grouping-assistant-final","session_id":"multi-tool-loop-session","timestamp":"2026-03-01T10:00:05Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Both tool calls completed and the answer is ready."}],"usage":{"input_tokens":300,"output_tokens":40}},"costUSD":3.75}`,
	}
}

func cumulativeToolLoopLines() []string {
	return []string{
		`{"type":"user","uuid":"cumulative-user-1","session_id":"cumulative-tool-loop-session","timestamp":"2026-03-02T10:00:00Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":"Use tools and report one final answer with cumulative transcript counters."}}`,
		`{"type":"assistant","uuid":"cumulative-assistant-tool-1","session_id":"cumulative-tool-loop-session","timestamp":"2026-03-02T10:00:01Z","cwd":"/home/andres/projects/grouping","total_cost_usd":1.00,"message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_cumulative_read","name":"Read","input":{"file_path":"README.md"}}],"usage":{"input_tokens":100,"output_tokens":10}}}`,
		`{"type":"user","uuid":"cumulative-tool-result-1","session_id":"cumulative-tool-loop-session","timestamp":"2026-03-02T10:00:02Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_cumulative_read","content":"read completed","is_error":false}]}}`,
		`{"type":"assistant","uuid":"cumulative-assistant-tool-2","session_id":"cumulative-tool-loop-session","timestamp":"2026-03-02T10:00:03Z","cwd":"/home/andres/projects/grouping","total_cost_usd":3.00,"message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_cumulative_grep","name":"Grep","input":{"pattern":"usage"}}],"usage":{"input_tokens":300,"output_tokens":30}}}`,
		`{"type":"user","uuid":"cumulative-tool-result-2","session_id":"cumulative-tool-loop-session","timestamp":"2026-03-02T10:00:04Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_cumulative_grep","content":"grep completed","is_error":false}]}}`,
		`{"type":"assistant","uuid":"cumulative-assistant-final","session_id":"cumulative-tool-loop-session","timestamp":"2026-03-02T10:00:05Z","cwd":"/home/andres/projects/grouping","total_cost_usd":6.00,"message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Final answer with cumulative counters."}],"usage":{"input_tokens":600,"output_tokens":60}}}`,
	}
}

func metadataNoiseLines() []string {
	return []string{
		`{"type":"user","uuid":"metadata-user-1","session_id":"metadata-session","timestamp":"2026-03-03T10:00:00Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":"Answer while ignoring internal metadata records."}}`,
		`{"type":"summary","uuid":"metadata-summary-1","session_id":"metadata-session","timestamp":"2026-03-03T10:00:01Z","cwd":"/home/andres/projects/grouping","content":"Internal summary should not become an unknown dashboard row."}`,
		`{"type":"progress","uuid":"metadata-progress-1","session_id":"metadata-session","timestamp":"2026-03-03T10:00:02Z","cwd":"/home/andres/projects/grouping","content":[{"type":"text","text":"Internal progress should not become an unknown dashboard row."}]}`,
		`{"type":"system","uuid":"metadata-meta-1","session_id":"metadata-session","timestamp":"2026-03-03T10:00:03Z","cwd":"/home/andres/projects/grouping","isMeta":true,"message":{"content":[{"type":"text","text":"Meta event without message role should be filtered."}]}}`,
		`{"type":"assistant","uuid":"metadata-assistant-final","session_id":"metadata-session","timestamp":"2026-03-03T10:00:04Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Metadata was ignored."}],"usage":{"input_tokens":42,"output_tokens":7}},"costUSD":0.42}`,
	}
}

func hybridToolResultTextLines() []string {
	return []string{
		`{"type":"user","uuid":"hybrid-user-1","session_id":"hybrid-tool-result-session","timestamp":"2026-03-04T10:00:00Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":"Inspect the fixture with the tool."}}`,
		`{"type":"assistant","uuid":"hybrid-assistant-tool-1","session_id":"hybrid-tool-result-session","timestamp":"2026-03-04T10:00:01Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_hybrid_inspect","name":"Read","input":{"file_path":"fixture.txt"}}],"usage":{"input_tokens":100,"output_tokens":20}},"costUSD":5.00}`,
		`{"type":"user","uuid":"hybrid-user-2","session_id":"hybrid-tool-result-session","timestamp":"2026-03-04T10:00:02Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_hybrid_inspect","content":"hybrid tool completed","is_error":false},{"type":"text","text":"Now summarize the inspection result."}]}}`,
		`{"type":"assistant","uuid":"hybrid-assistant-final","session_id":"hybrid-tool-result-session","timestamp":"2026-03-04T10:00:03Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Inspection summary is ready."}],"usage":{"input_tokens":70,"output_tokens":15}},"costUSD":7.00}`,
	}
}

func isMetaInterleavedToolResultLines() []string {
	return []string{
		`{"type":"user","uuid":"is-meta-real-user","session_id":"is-meta-tool-result-session","timestamp":"2026-03-05T10:00:00Z","cwd":"/home/andres/projects/grouping","message":{"role":"user","content":"Use the tool and then answer as one real prompt."}}`,
		`{"type":"assistant","uuid":"is-meta-assistant-tool","session_id":"is-meta-tool-result-session","timestamp":"2026-03-05T10:00:01Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_is_meta_read","name":"Read","input":{"file_path":"fixture.txt"}}],"usage":{"input_tokens":20,"output_tokens":5}},"costUSD":2.00}`,
		`{"type":"user","uuid":"is-meta-user-shaped-row","session_id":"is-meta-tool-result-session","timestamp":"2026-03-05T10:00:02Z","cwd":"/home/andres/projects/grouping","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_is_meta_read","content":"metadata tool result completed","is_error":false},{"type":"text","text":"METADATA PROMPT-LIKE TEXT: do not start a dashboard interaction from this row."}]}}`,
		`{"type":"assistant","uuid":"is-meta-assistant-final","session_id":"is-meta-tool-result-session","timestamp":"2026-03-05T10:00:03Z","cwd":"/home/andres/projects/grouping","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Final answer stays attached to the legitimate prompt."}],"usage":{"input_tokens":30,"output_tokens":10}},"costUSD":3.00}`,
	}
}

func chronologicalMessageSort() stats.MessageSort {
	return stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc}
}

func assertDetailToolCompleted(t *testing.T, detail *stats.MessageDetail, callID, outputContains string) {
	t.Helper()
	tool := findToolPart(t, detail, callID)
	if tool.State.Status != "completed" {
		t.Errorf("tool %q status = %q, want completed", callID, tool.State.Status)
	}
	if !strings.Contains(tool.State.Output, outputContains) {
		t.Errorf("tool %q output = %q, want to contain %q", callID, tool.State.Output, outputContains)
	}
}

func assertDetailTextContains(t *testing.T, detail *stats.MessageDetail, want string) {
	t.Helper()
	if !detailContainsText(detail, want) {
		t.Errorf("MessageDetail(%q) text parts do not contain %q; got %#v", detail.ID, want, detail.Content.TextParts)
	}
}

func messageDetails(t *testing.T, src *Source, messages []stats.MessageEntry) []*stats.MessageDetail {
	t.Helper()
	details := make([]*stats.MessageDetail, 0, len(messages))
	for _, msg := range messages {
		details = append(details, mustMessageDetail(t, src, msg.ID))
	}
	return details
}

func detailsContainCompletedTool(t *testing.T, details []*stats.MessageDetail, callID, outputContains string) bool {
	t.Helper()
	for _, detail := range details {
		for _, tool := range detail.Content.ToolParts {
			if tool.CallID == callID && tool.State.Status == "completed" && strings.Contains(tool.State.Output, outputContains) {
				return true
			}
		}
	}
	return false
}

func detailsContainText(details []*stats.MessageDetail, want string) bool {
	for _, detail := range details {
		if detailContainsText(detail, want) {
			return true
		}
	}
	return false
}

func detailContainsText(detail *stats.MessageDetail, want string) bool {
	for _, part := range detail.Content.TextParts {
		if strings.Contains(part.Text, want) {
			return true
		}
	}
	return false
}
