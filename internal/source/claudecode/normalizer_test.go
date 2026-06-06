package claudecode

import (
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestNormalizerSynthesizesProjectsSessionsMessagesAndProviderIdentity(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)

	projects, err := src.Projects(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Projects(all) failed: %v", err)
	}
	assertAllSourceID(t, projects.SourceID)
	if len(projects.Projects) < 2 {
		t.Fatalf("Projects len = %d, want at least 2", len(projects.Projects))
	}
	var alphaProject stats.ProjectEntry
	for _, project := range projects.Projects {
		assertAllSourceID(t, project.SourceID)
		if project.ProjectID == "-home-andres-projects-alpha" {
			alphaProject = project
		}
	}
	if alphaProject.ProjectID == "" {
		t.Fatalf("alpha project ID not found in %#v", projects.Projects)
	}
	if alphaProject.ProjectName != "alpha" {
		t.Errorf("alpha project name = %q, want alpha", alphaProject.ProjectName)
	}
	if alphaProject.Messages == 0 || alphaProject.Sessions == 0 {
		t.Errorf("alpha project aggregate messages/sessions = %d/%d, want non-zero", alphaProject.Messages, alphaProject.Sessions)
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 50, Sort: stats.SessionSortNewest, Period: "all"})
	if err != nil {
		t.Fatalf("Sessions(all) failed: %v", err)
	}
	assertAllSourceID(t, sessions.SourceID)
	if sessions.Total < 7 {
		t.Errorf("Sessions().Total = %d, want at least 7 fixture sessions", sessions.Total)
	}
	for _, session := range sessions.Sessions {
		assertAllSourceID(t, session.SourceID)
		if session.ProjectID == "" {
			t.Errorf("session %q ProjectID is empty", session.ID)
		}
		if session.TimeCreated.IsZero() || session.TimeUpdated.IsZero() {
			t.Errorf("session %q has zero timestamps: %v/%v", session.ID, session.TimeCreated, session.TimeUpdated)
		}
	}

	messages := readAllMessages(t, src)
	seenIDs := make(map[string]struct{}, len(messages.Messages))
	for _, msg := range messages.Messages {
		assertAllSourceID(t, msg.SourceID)
		if msg.ID == "" {
			t.Errorf("message in session %q has empty source-aware ID", msg.SessionID)
		}
		if _, exists := seenIDs[msg.ID]; exists {
			t.Errorf("duplicate message ID %q; IDs must be stable within selected source", msg.ID)
		}
		seenIDs[msg.ID] = struct{}{}
		if msg.ProviderID == string(source.SourceClaudeCode) {
			t.Errorf("message %q provider_id = claude_code; provider_id must remain AI provider identity", msg.ID)
		}
		if msg.Role == "assistant" && msg.ModelID != "" && msg.ProviderID != "anthropic" {
			t.Errorf("assistant %q provider_id = %q, want anthropic", msg.ID, msg.ProviderID)
		}
	}
}

func TestNormalizerPairsToolsAndPreservesUnpairedToolAsPartial(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	list := readAllMessages(t, src)
	toolUseTime := time.Date(2026, 1, 5, 9, 0, 1, 0, time.UTC)

	pairedMsg := findMessage(t, list, func(msg stats.MessageEntry) bool {
		return msg.SessionID == "tools-session" && msg.Role == "assistant" && msg.TimeCreated.Equal(toolUseTime)
	})
	pairedDetail := mustMessageDetail(t, src, pairedMsg.ID)
	pairedTool := findToolPart(t, pairedDetail, "toolu_read_1")
	if pairedTool.Tool != "Read" {
		t.Errorf("paired tool name = %q, want Read", pairedTool.Tool)
	}
	if pairedTool.State.Status != "completed" {
		t.Errorf("paired tool status = %q, want completed", pairedTool.State.Status)
	}
	if !strings.Contains(pairedTool.State.Output, "enabled") {
		t.Errorf("paired tool output = %q, want tool_result content", pairedTool.State.Output)
	}

	unpairedMsg := findMessage(t, list, func(msg stats.MessageEntry) bool {
		return msg.SessionID == "unpaired-tool-session" && msg.Role == "assistant"
	})
	unpairedDetail := mustMessageDetail(t, src, unpairedMsg.ID)
	unpairedTool := findToolPart(t, unpairedDetail, "toolu_unpaired_1")
	if unpairedTool.Tool != "Bash" {
		t.Errorf("unpaired tool name = %q, want Bash", unpairedTool.Tool)
	}
	if unpairedTool.State.Status != "partial" && unpairedTool.State.Status != "pending" {
		t.Errorf("unpaired tool status = %q, want partial or pending", unpairedTool.State.Status)
	}
	if unpairedTool.State.Output != "" {
		t.Errorf("unpaired tool output = %q, want empty output", unpairedTool.State.Output)
	}
}
