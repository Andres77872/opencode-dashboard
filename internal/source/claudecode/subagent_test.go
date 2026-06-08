package claudecode

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

// TestSubagentUsageRollsIntoParentSessionWithoutDoubleCounting verifies the core
// reporting fix: subagent (sidechain) transcripts under <session>/subagents/ are
// discovered, counted as their own API-request messages, attributed to the parent
// session, flagged with their agent name, and never double-counted against the
// parent's Task tool call.
func TestSubagentUsageRollsIntoParentSessionWithoutDoubleCounting(t *testing.T) {
	mainLines := []string{
		`{"type":"user","uuid":"main-user-1","sessionId":"main-session","timestamp":"2026-05-01T10:00:00Z","cwd":"/home/andres/projects/sub","message":{"role":"user","content":"Investigate the repo using a subagent."}}`,
		`{"type":"assistant","uuid":"main-assistant-1","sessionId":"main-session","requestId":"req_main_1","timestamp":"2026-05-01T10:00:01Z","cwd":"/home/andres/projects/sub","message":{"id":"msg_main_1","role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_task_1","name":"Task","input":{"subagent_type":"Explore"}}],"usage":{"input_tokens":1000,"output_tokens":200}}}`,
		`{"type":"user","uuid":"main-tool-result-1","sessionId":"main-session","timestamp":"2026-05-01T10:00:30Z","cwd":"/home/andres/projects/sub","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_task_1","content":"subagent finished","is_error":false}]}}`,
		`{"type":"assistant","uuid":"main-assistant-2","sessionId":"main-session","requestId":"req_main_2","timestamp":"2026-05-01T10:00:31Z","cwd":"/home/andres/projects/sub","message":{"id":"msg_main_2","role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Here is the final answer."}],"usage":{"input_tokens":300,"output_tokens":50}}}`,
	}
	// Real Claude subagent transcripts live under <session>/subagents/ and carry the
	// PARENT sessionId, isSidechain:true, and attributionAgent. They use their own
	// requestIds, so their usage is additional, not a duplicate of the Task tool call.
	subagentLines := []string{
		`{"type":"user","uuid":"sub-user-1","sessionId":"main-session","isSidechain":true,"agentId":"agent-explore-1","timestamp":"2026-05-01T10:00:05Z","cwd":"/home/andres/projects/sub","message":{"role":"user","content":"Explore the codebase and report structure."}}`,
		`{"type":"assistant","uuid":"sub-assistant-1","sessionId":"main-session","isSidechain":true,"attributionAgent":"Explore","requestId":"req_sub_1","timestamp":"2026-05-01T10:00:06Z","cwd":"/home/andres/projects/sub","message":{"id":"msg_sub_1","role":"assistant","model":"claude-test-approx","content":[{"type":"text","text":"The repo has packages a and b."}],"usage":{"input_tokens":500,"output_tokens":100}}}`,
	}
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-sub/main-session.jsonl":                         mainLines,
		"-home-andres-projects-sub/main-session/subagents/agent-explore.jsonl": subagentLines,
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1 (subagent rolls into the parent session)", overview.Sessions)
	}
	// 2 main prompts/requests rows (1 user + ... actually 1 user + 2 assistant) + 2 subagent rows.
	if overview.Messages != 5 {
		t.Errorf("Overview().Messages = %d, want 5 (main: 1 user + 2 API requests; subagent: 1 user + 1 API request)", overview.Messages)
	}
	// Cost: main computed (1000*3+200*15 + 300*3+50*15)/1e6 = (3000+3000+900+750)/1e6 = 0.00765
	// subagent approx (500*2+100*10)/1e6 = 0.002 -> total 0.00965
	if !approxEqual(overview.Cost, 0.00965) {
		t.Errorf("Overview().Cost = %.6f, want 0.009650 including subagent usage", overview.Cost)
	}
	// Tokens must include the subagent (input 1000+300+500=1800, output 200+50+100=350).
	if overview.Tokens.Input != 1800 || overview.Tokens.Output != 350 {
		t.Errorf("Overview().Tokens input/output = %d/%d, want 1800/350 including subagent usage", overview.Tokens.Input, overview.Tokens.Output)
	}

	// Models must split the main model from the subagent model.
	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all) failed: %v", err)
	}
	mainModel := findModelEntryByID(t, models, "claude-test-computed")
	if mainModel.Messages != 2 {
		t.Errorf("Models()[claude-test-computed].Messages = %d, want 2 main API requests", mainModel.Messages)
	}
	subModel := findModelEntryByID(t, models, "claude-test-approx")
	if subModel.Messages != 1 {
		t.Errorf("Models()[claude-test-approx].Messages = %d, want 1 subagent API request", subModel.Messages)
	}
	if subModel.Tokens.Input != 500 || subModel.Tokens.Output != 100 {
		t.Errorf("Models()[claude-test-approx].Tokens input/output = %d/%d, want 500/100", subModel.Tokens.Input, subModel.Tokens.Output)
	}

	// The subagent assistant message must be flagged with its agent name and roll up
	// under the parent session, not a separate one.
	session, err := src.SessionByID(ctx, "main-session")
	if err != nil {
		t.Fatalf("SessionByID(main-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(main-session) = nil, want detail")
	}
	if session.MessageCount != 5 {
		t.Errorf("SessionByID().MessageCount = %d, want 5", session.MessageCount)
	}
	foundSubAssistant := false
	for _, msg := range session.Messages {
		if msg.ID == "claude_code:main-session:sub-assistant-1" {
			foundSubAssistant = true
			if !msg.IsSubagent {
				t.Errorf("subagent assistant message IsSubagent = false, want true")
			}
			if msg.Agent != "Explore" {
				t.Errorf("subagent assistant message Agent = %q, want Explore", msg.Agent)
			}
			if msg.ModelID != "claude-test-approx" {
				t.Errorf("subagent assistant message ModelID = %q, want claude-test-approx", msg.ModelID)
			}
		}
	}
	if !foundSubAssistant {
		t.Errorf("subagent assistant message not present in parent session detail")
	}

	// Main rows must NOT be flagged as subagent.
	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	mainAssistant := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.ID == "claude_code:main-session:main-assistant-1"
	})
	if mainAssistant.IsSubagent {
		t.Errorf("main assistant message IsSubagent = true, want false")
	}
}

// TestStreamingChunksSharingRequestIDCountAsOneAPIRequest verifies that multiple
// transcript lines that share one requestId (streamed thinking/text/tool chunks of
// the same API response) collapse into a single API-request message with usage
// counted once.
func TestStreamingChunksSharingRequestIDCountAsOneAPIRequest(t *testing.T) {
	usage := `"usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":40,"cache_creation_input_tokens":12}`
	prefix := `{"type":"assistant","session_id":"stream-session","requestId":"req_stream_1","timestamp":"2026-05-02T10:00:01Z","cwd":"/home/andres/projects/stream","message":{"id":"msg_stream_1","role":"assistant","model":"claude-test-computed",`
	suffix := `,` + usage + `}}`
	lines := []string{
		`{"type":"user","uuid":"stream-user-1","session_id":"stream-session","timestamp":"2026-05-02T10:00:00Z","cwd":"/home/andres/projects/stream","message":{"role":"user","content":"Answer with a streamed response."}}`,
		prefix + `"content":[{"type":"thinking","thinking":"Considering the answer."}]` + suffix,
		prefix + `"content":[{"type":"text","text":"Streamed answer block."}]` + suffix,
		prefix + `"content":[{"type":"tool_use","id":"toolu_stream","name":"Read","input":{"file_path":"x.txt"}}]` + suffix,
	}
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-stream/stream-session.jsonl": lines,
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// 1 user prompt + 1 merged API request.
	if overview.Messages != 2 {
		t.Errorf("Overview().Messages = %d, want 2 (one prompt + one merged API request)", overview.Messages)
	}
	// Usage counted once despite three chunks: (1000*3+200*15+40*0.3+12*3.75)/1e6.
	wantCost := (1000*3.0 + 200*15.0 + 40*0.3 + 12*3.75) / 1_000_000
	if !approxEqual(overview.Cost, wantCost) {
		t.Errorf("Overview().Cost = %.9f, want %.9f from one billed request", overview.Cost, wantCost)
	}
	if overview.Tokens.Input != 1000 || overview.Tokens.Output != 200 || overview.Tokens.Cache.Read != 40 || overview.Tokens.Cache.Write != 12 {
		t.Errorf("Overview().Tokens = %#v, want one usage contribution despite three streamed chunks", overview.Tokens)
	}
	if overview.CostProvenance == nil || overview.CostProvenance.ComputedCount != 1 {
		t.Errorf("Overview().CostProvenance = %#v, want exactly one computed billed request", overview.CostProvenance)
	}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	assistant := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.Role == "assistant" })
	detail := mustMessageDetail(t, src, assistant.ID)
	if len(detail.Content.ReasoningParts) != 1 {
		t.Errorf("merged request ReasoningParts len = %d, want 1 thinking chunk", len(detail.Content.ReasoningParts))
	}
	if !detailContainsText(detail, "Streamed answer block.") {
		t.Errorf("merged request missing text chunk: %#v", detail.Content.TextParts)
	}
	if len(detail.Content.ToolParts) != 1 {
		t.Errorf("merged request ToolParts len = %d, want 1 tool chunk", len(detail.Content.ToolParts))
	}
}
