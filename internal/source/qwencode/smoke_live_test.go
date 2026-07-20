package qwencode

import (
	"os"
	"testing"

	"opencode-dashboard/internal/stats"
)

// Opt-in smoke check against a real Qwen home; run with:
// QWEN_CODE_SMOKE_HOME=$HOME/.qwen go test ./internal/source/qwencode -run Smoke -v
func TestQwenCodeLiveSmoke(t *testing.T) {
	home := os.Getenv("QWEN_CODE_SMOKE_HOME")
	if home == "" {
		t.Skip("QWEN_CODE_SMOKE_HOME is not set")
	}
	src := New(Options{QwenHome: home})
	ctx := testContext(t)
	info := src.Info(ctx)
	t.Logf("info: available=%v status=%q scanned=%d malformed=%d unsupported=%d reason=%q",
		info.Available, info.Diagnostics.Status, info.Diagnostics.ScannedFiles,
		info.Diagnostics.MalformedLines, info.Diagnostics.UnsupportedEvents, info.Diagnostics.Reason)
	if !info.Available {
		t.Fatal("live home not available")
	}
	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	t.Logf("overview: sessions=%d messages=%d tokens=%+v cost=%.6f status=%s",
		overview.Sessions, overview.Messages, overview.Tokens, overview.Cost, overview.CostStatus)
	models, err := src.Models(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	for _, m := range models.Models {
		t.Logf("model: %s provider=%s messages=%d tokens=%+v cost=%.6f status=%s",
			m.ModelID, m.ProviderID, m.Messages, m.Tokens, m.Cost, m.CostStatus)
	}
	tools, err := src.Tools(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	for _, tool := range tools.Tools {
		t.Logf("tool: %s invocations=%d ok=%d fail=%d", tool.Name, tool.Invocations, tool.Successes, tool.Failures)
	}
	sessions, err := src.Sessions(ctx, stats.SessionQuery{Period: "all", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	t.Logf("sessions total=%d", sessions.Total)
	for _, s := range sessions.Sessions {
		t.Logf("session: id=%s project=%s messages=%d cost=%.6f", s.ID[:8], s.ProjectName, s.MessageCount, s.Cost)
	}
	projects, err := src.Projects(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	for _, p := range projects.Projects {
		t.Logf("project: %s sessions=%d messages=%d", p.ProjectName, p.Sessions, p.Messages)
	}
}
