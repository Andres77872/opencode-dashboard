package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestCodexAggregatesMapToExistingDashboardConcepts(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "overview uses grouped turns and Codex source id",
			run: func(t *testing.T) {
				got, err := src.Overview(ctx, period)
				if err != nil {
					t.Fatalf("Overview() failed: %v", err)
				}
				assertCodexSourceID(t, got.SourceID)
				// 2 user prompts + 2 assistant API requests across 1 session.
				if got.Sessions != 1 || got.Messages != 4 {
					t.Errorf("Overview sessions/messages = %d/%d, want 1/4 per-request rows", got.Sessions, got.Messages)
				}
				assertCodexTokenTotals(t, "Overview().Tokens", got.Tokens, 1200, 300, 45, 30)
				assertCodexEstimatedProvenance(t, got.CostStatus, got.CostProvenance)
			},
		},
		{
			name: "daily groups by deterministic Codex turn day",
			run: func(t *testing.T) {
				got, err := src.Daily(ctx, period, stats.GranularityDay)
				if err != nil {
					t.Fatalf("Daily() failed: %v", err)
				}
				assertCodexSourceID(t, got.SourceID)
				if len(got.Days) != 1 {
					t.Fatalf("Daily days len = %d, want 1: %#v", len(got.Days), got.Days)
				}
				day := got.Days[0]
				assertCodexSourceID(t, day.SourceID)
				if day.Date != "2026-01-02" || day.Messages != 4 || day.Sessions != 1 {
					t.Errorf("day = %#v, want 2026-01-02 with 4 messages/1 session", day)
				}
			},
		},
		{
			name: "models aggregate under OpenAI gpt-5.5",
			run: func(t *testing.T) {
				got, err := src.Models(ctx, period)
				if err != nil {
					t.Fatalf("Models() failed: %v", err)
				}
				assertCodexSourceID(t, got.SourceID)
				model := findModelEntryByID(t, got, "gpt-5.5")
				assertCodexSourceID(t, model.SourceID)
				if model.ProviderID != "openai" {
					t.Errorf("provider_id = %q, want openai", model.ProviderID)
				}
				if model.Messages != 2 || model.Sessions != 1 {
					t.Errorf("model messages/sessions = %d/%d, want 2/1 (2 assistant API requests)", model.Messages, model.Sessions)
				}
			},
		},
		{
			name: "tools aggregate folded invocations only",
			run: func(t *testing.T) {
				got, err := src.Tools(ctx, period)
				if err != nil {
					t.Fatalf("Tools() failed: %v", err)
				}
				assertCodexSourceID(t, got.SourceID)
				shell := findToolEntryByName(t, got, "shell")
				if shell.Invocations != 1 || shell.Successes != 1 || shell.Sessions != 1 {
					t.Errorf("shell aggregate = %#v, want one paired invocation in one session", shell)
				}
				for _, tool := range got.Tools {
					if tool.Name == "function_call_output" || tool.Name == "patch_apply_end" {
						t.Errorf("raw tool/status row counted as invocation: %#v", tool)
					}
				}
			},
		},
		{
			name: "projects sessions messages and config are source tagged",
			run: func(t *testing.T) {
				projects, err := src.Projects(ctx, period)
				if err != nil {
					t.Fatalf("Projects() failed: %v", err)
				}
				assertCodexSourceID(t, projects.SourceID)
				if len(projects.Projects) != 1 {
					t.Fatalf("Projects len = %d, want 1 redacted synthetic project", len(projects.Projects))
				}
				assertCodexSourceID(t, projects.Projects[0].SourceID)

				sessions, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 10, Period: "all"})
				if err != nil {
					t.Fatalf("Sessions() failed: %v", err)
				}
				assertCodexSourceID(t, sessions.SourceID)
				if sessions.Total != 1 || len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != "synthetic-session" {
					t.Errorf("Sessions = %#v, want one synthetic-session", sessions)
				}

				messages, err := src.Messages(ctx, period, 1, 50, chronologicalMessageSort())
				if err != nil {
					t.Fatalf("Messages() failed: %v", err)
				}
				assertCodexSourceID(t, messages.SourceID)
				if messages.Total != 4 {
					t.Errorf("Messages.Total = %d, want 4 source-aware Codex per-request rows", messages.Total)
				}
				wantIDs := map[string]bool{
					"codex:synthetic-session:turn-1:u0": false,
					"codex:synthetic-session:turn-1:u1": false,
					"codex:synthetic-session:turn-1:r0": false,
					"codex:synthetic-session:turn-1:r1": false,
				}
				for _, msg := range messages.Messages {
					if _, ok := wantIDs[msg.ID]; ok {
						wantIDs[msg.ID] = true
					}
				}
				for id, seen := range wantIDs {
					if !seen {
						t.Errorf("Messages missing expected per-request id %q: %#v", id, messages.Messages)
					}
				}

				config, err := src.Config(ctx)
				if err != nil {
					t.Fatalf("Config() failed: %v", err)
				}
				assertCodexSourceID(t, config.SourceID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestCodexMessageAndSessionDetailRemainSourceAwareAndRedacted(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)

	session, err := src.SessionByID(ctx, "synthetic-session")
	if err != nil {
		t.Fatalf("SessionByID(synthetic-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(synthetic-session) = nil")
	}
	assertCodexSourceID(t, session.SourceID)
	// 2 user prompts + 2 assistant API requests.
	if session.MessageCount != 4 || len(session.Messages) != 4 {
		t.Errorf("session message_count/messages len = %d/%d, want 4/4 per-request rows", session.MessageCount, len(session.Messages))
	}

	// The first user prompt is its own row.
	userDetail, err := src.MessageByID(ctx, "codex:synthetic-session:turn-1:u0")
	if err != nil {
		t.Fatalf("MessageByID(u0) failed: %v", err)
	}
	if userDetail == nil {
		t.Fatalf("MessageByID(u0) = nil")
	}
	if !detailTextContains(userDetail, "[REDACTED_USER_MESSAGE_PART_1]") {
		t.Errorf("user row detail missing redacted prompt: %#v", userDetail.Content.TextParts)
	}

	// All content attaches to the request row that closes first (r0).
	detail, err := src.MessageByID(ctx, "codex:synthetic-session:turn-1:r0")
	if err != nil {
		t.Fatalf("MessageByID(r0) failed: %v", err)
	}
	if detail == nil {
		t.Fatalf("MessageByID(r0) = nil")
	}
	assertCodexSourceID(t, detail.SourceID)
	if !detailTextContains(detail, "[REDACTED_ASSISTANT_SUMMARY]") {
		t.Errorf("detail missing redacted assistant text: %#v", detail.Content.TextParts)
	}
	if len(detail.Content.ReasoningParts) != 1 || !strings.Contains(strings.ToLower(detail.Content.ReasoningParts[0].Text), "reasoning") {
		t.Errorf("reasoning parts = %#v, want redacted reasoning placeholder/count", detail.Content.ReasoningParts)
	}
	tool := findToolPart(t, detail, "call-shell-1")
	if tool.Tool != "shell" || tool.State.Status != "completed" {
		t.Errorf("paired shell tool = %#v, want completed shell call", tool)
	}
	assertJSONDoesNotContain(t, detail, codexForbiddenText()...)
}

func TestCodexAggregatesRedactAbsoluteProjectPaths(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T08-00-00Z-path-session.jsonl": {
			`{"timestamp":"2026-01-02T08:00:00Z","type":"session_meta","payload":{"id":"path-session","model_provider":"openai","cwd":"/home/synthetic/private-project"}}`,
			`{"timestamp":"2026-01-02T08:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"path-turn"}}`,
			`{"timestamp":"2026-01-02T08:00:02Z","type":"turn_context","payload":{"turn_id":"path-turn","model":"gpt-5.5","model_provider":"openai","cwd":"/home/synthetic/private-project"}}`,
			`{"timestamp":"2026-01-02T08:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"path-turn","message":"[REDACTED_PROMPT]"}}`,
			`{"timestamp":"2026-01-02T08:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"path-turn","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0,"total_tokens":11}}}}`,
			`{"timestamp":"2026-01-02T08:00:05Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"path-turn"}}`,
		},
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	session, err := src.SessionByID(ctx, "path-session")
	if err != nil {
		t.Fatalf("SessionByID(path-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(path-session) = nil")
	}
	if strings.Contains(session.Directory, "/home/synthetic") {
		t.Fatalf("SessionDetail.Directory leaked raw absolute path: %q", session.Directory)
	}
	if session.Directory != "[REDACTED_PATH]/private-project" {
		t.Fatalf("SessionDetail.Directory = %q, want redacted basename path", session.Directory)
	}

	project, err := src.ProjectByID(ctx, "private-project", period, 1, 10)
	if err != nil {
		t.Fatalf("ProjectByID(private-project) failed: %v", err)
	}
	if project == nil {
		t.Fatalf("ProjectByID(private-project) = nil")
	}
	if strings.Contains(project.Worktree, "/home/synthetic") {
		t.Fatalf("ProjectDetail.Worktree leaked raw absolute path: %q", project.Worktree)
	}

	encoded, err := json.Marshal(struct {
		Session *stats.SessionDetail `json:"session"`
		Project *stats.ProjectDetail `json:"project"`
	}{Session: session, Project: project})
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	if strings.Contains(string(encoded), "/home/synthetic") {
		t.Fatalf("Codex aggregate details leaked raw absolute path: %s", string(encoded))
	}
}

func assertCodexEstimatedProvenance(t *testing.T, status stats.CostStatus, provenance *stats.CostProvenance) {
	t.Helper()
	if status != stats.CostEstimatedAPIEquivalent {
		t.Errorf("CostStatus = %q, want %q", status, stats.CostEstimatedAPIEquivalent)
	}
	if provenance == nil {
		t.Fatalf("CostProvenance = nil")
	}
	if provenance.Status != stats.CostEstimatedAPIEquivalent {
		t.Errorf("provenance status = %q, want %q", provenance.Status, stats.CostEstimatedAPIEquivalent)
	}
	if !strings.Contains(strings.ToLower(provenance.Note), "api-equivalent") || !strings.Contains(strings.ToLower(provenance.Note), "not actual") {
		t.Errorf("provenance note = %q, want API-equivalent/not actual caveat", provenance.Note)
	}
}
