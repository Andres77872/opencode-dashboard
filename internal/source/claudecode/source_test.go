package claudecode

import (
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestClaudeCodeSourceImplementsSourceInfoContract(t *testing.T) {
	var _ source.Source = New(Options{})

	src := newFixtureSource(t, "valid_home")
	info := src.Info(testContext(t))

	if info.ID != source.SourceClaudeCode {
		t.Errorf("Info().ID = %q, want %q", info.ID, source.SourceClaudeCode)
	}
	if info.Label != "Claude Code" {
		t.Errorf("Info().Label = %q, want Claude Code", info.Label)
	}
	if info.Kind != "jsonl" {
		t.Errorf("Info().Kind = %q, want jsonl", info.Kind)
	}
	if !info.Available {
		t.Errorf("Info().Available = false, want true")
	}
	if !info.ReadOnly || !info.LocalOnly {
		t.Errorf("Info() read_only/local_only = %v/%v, want true/true", info.ReadOnly, info.LocalOnly)
	}
	if info.CostPolicy.Status == "" || info.CostPolicy.Currency != "USD" {
		t.Errorf("Info().CostPolicy = %#v, want USD cost policy", info.CostPolicy)
	}
	if !info.Privacy.PlaintextTranscripts || !info.Privacy.ReadOnly || !info.Privacy.LocalOnly || !info.Privacy.Redaction {
		t.Errorf("Info().Privacy = %#v, want plaintext/read-only/local/redaction flags", info.Privacy)
	}
	warningText := strings.Join(append(info.Warnings, info.Privacy.Warnings...), " ")
	if !strings.Contains(strings.ToLower(warningText), "plaintext") {
		t.Errorf("warnings = %#v privacy warnings = %#v, want plaintext warning", info.Warnings, info.Privacy.Warnings)
	}
}

func TestClaudeCodeSourceReadOnlySafety(t *testing.T) {
	src, home := newCopiedFixtureSource(t, "valid_home")
	before := snapshotTree(t, home)
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	_ = src.Info(ctx)
	if _, err := src.Overview(ctx, period); err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	if _, err := src.Daily(ctx, period, stats.GranularityDay); err != nil {
		t.Fatalf("Daily() failed: %v", err)
	}
	if _, err := src.Models(ctx, period); err != nil {
		t.Fatalf("Models() failed: %v", err)
	}
	if _, err := src.Tools(ctx, period); err != nil {
		t.Fatalf("Tools() failed: %v", err)
	}
	if _, err := src.Projects(ctx, period); err != nil {
		t.Fatalf("Projects() failed: %v", err)
	}
	if _, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 10, Sort: stats.SessionSortNewest, Period: "all"}); err != nil {
		t.Fatalf("Sessions() failed: %v", err)
	}
	messages, err := src.Messages(ctx, period, 1, 200, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages() failed: %v", err)
	}
	if len(messages.Messages) == 0 {
		t.Fatalf("Messages() returned empty fixture list")
	}
	if _, err := src.MessageByID(ctx, messages.Messages[0].ID); err != nil {
		t.Fatalf("MessageByID() failed: %v", err)
	}
	if _, err := src.SessionByID(ctx, messages.Messages[0].SessionID); err != nil {
		t.Fatalf("SessionByID() failed: %v", err)
	}
	if _, err := src.Config(ctx); err != nil {
		t.Fatalf("Config() failed: %v", err)
	}

	after := snapshotTree(t, home)
	assertTreeUnchanged(t, before, after)
	for rel := range after {
		lower := strings.ToLower(rel)
		if strings.Contains(lower, "opencode-dashboard") || strings.HasSuffix(lower, ".db") || strings.Contains(lower, "index") {
			t.Errorf("scan created dashboard-owned persistent artifact %q; Claude v1 must remain passive/read-only", rel)
		}
	}
}
