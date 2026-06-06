package claudecode

import (
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestClaudeCodeAggregatesReturnExistingStatsShapes(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "overview includes source and counts",
			run: func(t *testing.T) {
				got, err := src.Overview(ctx, period)
				if err != nil {
					t.Fatalf("Overview() failed: %v", err)
				}
				assertAllSourceID(t, got.SourceID)
				if got.Sessions != 7 || got.Messages != 8 {
					t.Errorf("Overview sessions/messages = %d/%d, want 7/8 grouped user-facing interactions", got.Sessions, got.Messages)
				}
			},
		},
		{
			name: "daily groups by deterministic dates",
			run: func(t *testing.T) {
				got, err := src.Daily(ctx, period, stats.GranularityDay)
				if err != nil {
					t.Fatalf("Daily() failed: %v", err)
				}
				assertAllSourceID(t, got.SourceID)
				if len(got.Days) < 7 {
					t.Errorf("Daily days = %d, want at least 7 fixture days", len(got.Days))
				}
				for _, day := range got.Days {
					assertAllSourceID(t, day.SourceID)
					if _, err := time.Parse("2006-01-02", day.Date); err != nil {
						t.Errorf("day date = %q, want YYYY-MM-DD: %v", day.Date, err)
					}
				}
			},
		},
		{
			name: "models preserve anthropic provider identity",
			run: func(t *testing.T) {
				got, err := src.Models(ctx, period)
				if err != nil {
					t.Fatalf("Models() failed: %v", err)
				}
				assertAllSourceID(t, got.SourceID)
				if len(got.Models) == 0 {
					t.Fatalf("Models empty, want Claude fixture models")
				}
				for _, model := range got.Models {
					assertAllSourceID(t, model.SourceID)
					if model.ProviderID != "anthropic" {
						t.Errorf("model %q provider_id = %q, want anthropic", model.ModelID, model.ProviderID)
					}
				}
			},
		},
		{
			name: "tools aggregate paired and unpaired calls",
			run: func(t *testing.T) {
				got, err := src.Tools(ctx, period)
				if err != nil {
					t.Fatalf("Tools() failed: %v", err)
				}
				assertAllSourceID(t, got.SourceID)
				if len(got.Tools) < 2 {
					t.Fatalf("Tools len = %d, want Read and Bash fixtures", len(got.Tools))
				}
				for _, tool := range got.Tools {
					assertAllSourceID(t, tool.SourceID)
					if tool.Name == "" || tool.Invocations == 0 {
						t.Errorf("tool aggregate = %#v, want name and invocation count", tool)
					}
				}
			},
		},
		{
			name: "config is source tagged",
			run: func(t *testing.T) {
				got, err := src.Config(ctx)
				if err != nil {
					t.Fatalf("Config() failed: %v", err)
				}
				assertAllSourceID(t, got.SourceID)
				if !got.Exists {
					t.Errorf("Config().Exists = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestClaudeCodePeriodFilteringPaginationAndDetailLookups(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)

	messages, err := src.Messages(ctx, stats.PeriodQuery{From: "2026-01-05", To: "2026-01-05"}, 1, 2, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages(custom day) failed: %v", err)
	}
	assertAllSourceID(t, messages.SourceID)
	if messages.Total != 1 {
		t.Errorf("Messages Jan 05 Total = %d, want 1 grouped user-facing interaction for the tool loop", messages.Total)
	}
	if len(messages.Messages) != 1 {
		t.Errorf("Messages Jan 05 page len = %d, want 1", len(messages.Messages))
	}
	for _, msg := range messages.Messages {
		if msg.TimeCreated.Format("2006-01-02") != "2026-01-05" {
			t.Errorf("message %q date = %s, want 2026-01-05", msg.ID, msg.TimeCreated.Format("2006-01-02"))
		}
		if msg.ID != "claude_code:tools-session:tools-user-1" {
			t.Errorf("message ID = %q, want grouped interaction ID based on user prompt row", msg.ID)
		}
		if msg.Role != "assistant" {
			t.Errorf("message role = %q, want assistant after folding assistant API calls into the prompt interaction", msg.Role)
		}
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 2, Sort: stats.SessionSortNewest, Period: "all"})
	if err != nil {
		t.Fatalf("Sessions(page) failed: %v", err)
	}
	assertAllSourceID(t, sessions.SourceID)
	if sessions.Page != 1 || sessions.PageSize != 2 {
		t.Errorf("Sessions page/page_size = %d/%d, want 1/2", sessions.Page, sessions.PageSize)
	}
	if len(sessions.Sessions) != 2 {
		t.Errorf("Sessions page len = %d, want 2", len(sessions.Sessions))
	}
	if sessions.Total < int64(len(sessions.Sessions)) {
		t.Errorf("Sessions total = %d, page len = %d", sessions.Total, len(sessions.Sessions))
	}

	if len(sessions.Sessions) > 0 {
		detail, err := src.SessionByID(ctx, sessions.Sessions[0].ID)
		if err != nil {
			t.Fatalf("SessionByID(%q) failed: %v", sessions.Sessions[0].ID, err)
		}
		if detail == nil {
			t.Fatalf("SessionByID(%q) = nil", sessions.Sessions[0].ID)
		}
		assertAllSourceID(t, detail.SourceID)
		if detail.ID != sessions.Sessions[0].ID {
			t.Errorf("SessionByID ID = %q, want %q", detail.ID, sessions.Sessions[0].ID)
		}
	}
}

func TestClaudeCodeSafeShapeFixtureAggregatesOnlyLegitimateInteraction(t *testing.T) {
	src := newFixtureSource(t, "safe_shape_home")
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	assertAllSourceID(t, overview.SourceID)
	if overview.Messages != 1 {
		t.Errorf("Overview().Messages = %d, want 1 legitimate safe-shape interaction; isMeta/user-shaped metadata and nested support transcripts must not count", overview.Messages)
	}
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1", overview.Sessions)
	}
	assertSafeShapeTokens(t, "Overview().Tokens", overview.Tokens)
	if overview.Cost <= 0 {
		t.Errorf("Overview().Cost = %.9f, want positive computed cost from assistant token usage", overview.Cost)
	}
	if overview.CostStatus != stats.CostComputed {
		t.Errorf("Overview().CostStatus = %q, want %q because safe fixture has usage but no reported cost fields", overview.CostStatus, stats.CostComputed)
	}

	daily, err := src.Daily(ctx, period, stats.GranularityDay)
	if err != nil {
		t.Fatalf("Daily(all) failed: %v", err)
	}
	assertAllSourceID(t, daily.SourceID)
	if len(daily.Days) != 1 {
		t.Fatalf("Daily().Days len = %d, want one safe-shape fixture day: %#v", len(daily.Days), daily.Days)
	}
	day := daily.Days[0]
	if day.Date != "2026-04-01" {
		t.Errorf("Daily()[0].Date = %q, want 2026-04-01", day.Date)
	}
	if day.Messages != 1 || day.Sessions != 1 {
		t.Errorf("Daily()[0] messages/sessions = %d/%d, want 1/1 legitimate interaction", day.Messages, day.Sessions)
	}
	assertSafeShapeTokens(t, "Daily()[0].Tokens", day.Tokens)

	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all) failed: %v", err)
	}
	assertAllSourceID(t, models.SourceID)
	if len(models.Models) != 1 {
		t.Fatalf("Models().Models len = %d, want one model for the legitimate folded interaction: %#v", len(models.Models), models.Models)
	}
	model := findModelEntryByID(t, models, "claude-test-computed")
	if model.Messages != 1 || model.Sessions != 1 {
		t.Errorf("Models()[claude-test-computed] messages/sessions = %d/%d, want 1/1", model.Messages, model.Sessions)
	}
	assertSafeShapeTokens(t, "Models()[claude-test-computed].Tokens", model.Tokens)
	if model.CostStatus != stats.CostComputed {
		t.Errorf("Models()[claude-test-computed].CostStatus = %q, want %q", model.CostStatus, stats.CostComputed)
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 10, Sort: stats.SessionSortNewest, Period: "all"})
	if err != nil {
		t.Fatalf("Sessions(all) failed: %v", err)
	}
	assertAllSourceID(t, sessions.SourceID)
	if sessions.Total != 1 || len(sessions.Sessions) != 1 {
		t.Fatalf("Sessions total/page len = %d/%d, want one safe-shape session: %#v", sessions.Total, len(sessions.Sessions), sessions.Sessions)
	}
	if sessions.Sessions[0].ID != "safe-shape-session" {
		t.Errorf("Sessions()[0].ID = %q, want safe-shape-session", sessions.Sessions[0].ID)
	}
	if sessions.Sessions[0].MessageCount != 1 {
		t.Errorf("Sessions()[0].MessageCount = %d, want 1 legitimate interaction", sessions.Sessions[0].MessageCount)
	}
	assertJSONDoesNotContain(t, sessions, safeShapeForbiddenText()...)

	session, err := src.SessionByID(ctx, "safe-shape-session")
	if err != nil {
		t.Fatalf("SessionByID(safe-shape-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(safe-shape-session) = nil, want detail")
	}
	if session.MessageCount != 1 || len(session.Messages) != 1 {
		t.Errorf("SessionByID().MessageCount/messages len = %d/%d, want 1/1", session.MessageCount, len(session.Messages))
	}
	assertSafeShapeTokens(t, "SessionByID().TotalTokens", session.TotalTokens)
	for _, msg := range session.Messages {
		if msg.Role == "unknown" {
			t.Errorf("SessionByID().Messages contains role unknown metadata row %#v", msg)
		}
	}
	assertJSONDoesNotContain(t, session, safeShapeForbiddenText()...)

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	assertAllSourceID(t, messages.SourceID)
	if messages.Total != 1 || len(messages.Messages) != 1 {
		t.Fatalf("Messages total/page len = %d/%d, want one legitimate interaction: %#v", messages.Total, len(messages.Messages), messages.Messages)
	}
	entry := messages.Messages[0]
	if entry.ID != "claude_code:safe-shape-session:safe-user-1" {
		t.Errorf("Messages()[0].ID = %q, want interaction ID based on legitimate user prompt", entry.ID)
	}
	if entry.Role == "unknown" {
		t.Errorf("Messages()[0].Role = unknown, metadata rows must not leak")
	}
	if entry.Tokens == nil {
		t.Fatalf("Messages()[0].Tokens = nil, want folded assistant usage")
	}
	assertSafeShapeTokens(t, "Messages()[0].Tokens", *entry.Tokens)
	if entry.FoldedAssistantCalls != 2 {
		t.Errorf("Messages()[0].FoldedAssistantCalls = %d, want 2", entry.FoldedAssistantCalls)
	}
	if entry.FoldedToolCalls != 1 {
		t.Errorf("Messages()[0].FoldedToolCalls = %d, want 1", entry.FoldedToolCalls)
	}
	assertJSONDoesNotContain(t, messages, safeShapeForbiddenText()...)

	detail := mustMessageDetail(t, src, entry.ID)
	assertDetailTextContains(t, detail, "Legitimate prompt: inspect the synthetic safe-shape fixture.")
	assertDetailTextContains(t, detail, "Final answer after the folded tool loop.")
	assertDetailToolCompleted(t, detail, "toolu_safe_read", "synthetic tool result from redacted fixture")
	if detail.FoldedAssistantCalls != 2 {
		t.Errorf("MessageByID().FoldedAssistantCalls = %d, want 2", detail.FoldedAssistantCalls)
	}
	if detail.FoldedToolCalls != 1 {
		t.Errorf("MessageByID().FoldedToolCalls = %d, want 1", detail.FoldedToolCalls)
	}
	assertJSONDoesNotContain(t, detail, safeShapeForbiddenText()...)
}

func assertSafeShapeTokens(t *testing.T, label string, got stats.TokenStats) {
	t.Helper()
	if got.Input != 250 || got.Output != 40 || got.Cache.Read != 15 || got.Cache.Write != 19 {
		t.Errorf("%s = input/output/cache.read/cache.write %d/%d/%d/%d, want 250/40/15/19 from top-level assistant rows only", label, got.Input, got.Output, got.Cache.Read, got.Cache.Write)
	}
}

func safeShapeForbiddenText() []string {
	return []string{
		"METADATA PROMPT-LIKE TEXT",
		"metadata mode event must not leak",
		"metadata title event must not leak",
		"metadata last prompt event must not leak",
		"metadata permission event must not leak",
		"metadata file history event must not leak",
		"metadata agent event must not leak",
		"metadata attachment event must not leak",
		"metadata queue event must not leak",
		"metadata system event must not leak",
		"Nested subagent prompt must be skipped",
		"Nested tool-results prompt must be skipped",
		"Nested debug prompt must be skipped",
	}
}
