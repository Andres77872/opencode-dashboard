package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	usagecache "opencode-dashboard/internal/cache"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func connectV2(t *testing.T, b *fixture.V2Builder) *store.Store {
	t.Helper()
	ctx := context.Background()
	path, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	st, err := store.Connect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func v2ConsolidationFixture(base time.Time) *fixture.V2Builder {
	tool := fixture.V2Tool("call_1", "shell", map[string]any{
		"status":  "completed",
		"input":   map[string]any{"command": "secret command"},
		"content": []any{map[string]any{"type": "text", "text": "secret output"}},
	})
	return fixture.NewV2Builder().
		AddProject(fixture.NewProject("prj", "/work/prj")).
		AddSession(fixture.V2Session{ID: "ses_parent", ProjectID: "prj", Created: base}).
		AddSession(fixture.V2Session{ID: "ses_fork", ProjectID: "prj", ForkOf: "ses_parent", Created: base.Add(time.Hour)}).
		AddRow(fixture.V2Row{ID: "msg_user", SessionID: "ses_parent", Type: "user", Seq: 0, Created: base.Add(time.Second), Data: fixture.V2User("secret prompt")}).
		AddRow(fixture.V2Row{ID: "msg_step", SessionID: "ses_parent", Type: "assistant", Seq: 1, Created: base.Add(2 * time.Second),
			Data: fixture.V2Assistant("anthropic", "claude-y", &fixture.V2Usage{Cost: 0.25, Input: 100, Output: 10, CacheRead: 5}, tool)}).
		AddRow(fixture.V2Row{ID: "msg_failed", SessionID: "ses_parent", Type: "assistant", Seq: 2, Created: base.Add(3 * time.Second),
			Data: map[string]any{"agent": "build", "model": map[string]any{"id": "claude-y", "providerID": "anthropic"}, "content": []any{}, "error": map[string]any{"type": "provider.error", "message": "overloaded"}}}).
		AddRow(fixture.V2Row{ID: "msg_copy", SessionID: "ses_fork", Type: "assistant", Seq: 1, Created: base.Add(2 * time.Second),
			Data: fixture.V2Assistant("anthropic", "claude-y", &fixture.V2Usage{Cost: 0.25, Input: 100, Output: 10}, tool)})
}

func TestV2ConsolidationDataMatchesInteractiveMessages(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	src := New(connectV2(t, v2ConsolidationFixture(base)))
	pq := stats.PeriodQuery{FromTime: base, ToTime: base.Add(2 * time.Hour)}

	data, err := src.ConsolidationData(ctx, pq)
	if err != nil {
		t.Fatalf("ConsolidationData: %v", err)
	}
	live, err := src.Messages(ctx, pq, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(data.Messages) != 3 || len(live.Messages) != 3 {
		t.Fatalf("messages = %d bulk / %d live, want 3 (the fork copy is excluded)", len(data.Messages), len(live.Messages))
	}
	if len(data.Sessions) != 1 || data.Sessions[0].ID != "ses_parent" || data.Sessions[0].MessageCount != 3 || data.Sessions[0].Cost != 0.25 {
		t.Errorf("sessions = %#v", data.Sessions)
	}

	liveByID := map[string]stats.MessageEntry{}
	for _, entry := range live.Messages {
		liveByID[entry.ID] = entry
	}
	for _, message := range data.Messages {
		bulk, interactive := message.Entry, liveByID[message.Entry.ID]
		if bulk.Role != interactive.Role || bulk.ModelID != interactive.ModelID || bulk.ProviderID != interactive.ProviderID ||
			bulk.Cost != interactive.Cost || bulk.UsageStatus != interactive.UsageStatus || bulk.UsageUnavailableReason != interactive.UsageUnavailableReason ||
			(bulk.Tokens == nil) != (interactive.Tokens == nil) || (bulk.Tokens != nil && *bulk.Tokens != *interactive.Tokens) {
			t.Errorf("message %s: bulk %#v != live %#v", bulk.ID, bulk, interactive)
		}
		if message.ModelTokens != nil {
			t.Errorf("message %s has a model-token override; 2.x step usage is already additive", bulk.ID)
		}
		switch bulk.ID {
		case "msg_step":
			if len(message.Tools) != 1 || message.Tools[0] != (source.ConsolidationTool{Name: "shell", Status: "completed"}) {
				t.Errorf("tools = %#v", message.Tools)
			}
		case "msg_failed":
			if bulk.Tokens != nil || bulk.UsageStatus != stats.UsageStatusUnavailable || bulk.UsageUnavailableReason != stats.UsageUnavailableFailed {
				t.Errorf("failed step = %#v", bulk)
			}
		}
	}

	// The cache snapshot crosses into the dashboard cache, which must never
	// store prompt, tool input, or tool output text.
	cacheStore, err := usagecache.Open(ctx, filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cacheStore.Close()
	report, err := cacheStore.SyncSourceWithOptions(ctx, src, usagecache.SyncOptions{Mode: usagecache.SyncModeRebuild, Cutoff: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("SyncSourceWithOptions: %v", err)
	}
	if report.Messages != 3 || report.Tools != 1 {
		t.Errorf("sync report = %#v, want 3 messages and 1 tool", report)
	}
	cachedOverview, err := cacheStore.Overview(ctx, "opencode", pq)
	if err != nil {
		t.Fatal(err)
	}
	rawOverview, err := src.Overview(ctx, pq)
	if err != nil {
		t.Fatal(err)
	}
	if cachedOverview.Cost != rawOverview.Cost || cachedOverview.Tokens != rawOverview.Tokens || cachedOverview.Requests != rawOverview.Requests {
		t.Errorf("cached overview %#v != raw %#v", cachedOverview, rawOverview)
	}
	raw, err := os.ReadFile(cacheStore.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret prompt", "secret command", "secret output"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("cache database contains %q", secret)
		}
	}
}

func TestV2InfoReportsLayoutAndImport(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	fresh := New(connectV2(t, v2ConsolidationFixture(base))).Info(context.Background())
	if !fresh.Available || fresh.DataLayout != layoutV2 || len(fresh.Warnings) != 0 {
		t.Errorf("fresh 2.x info = %#v", fresh)
	}

	importing := fixture.NewV2Builder().
		WithLegacyTables().
		AddProject(fixture.NewProject("prj", "/work/prj")).
		SetKV("migration.v1-v2", `{"phase":"sessions"}`).
		Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated) VALUES ('ses_old', 'prj', 'old', '/w', '1.18.34', 1, 1)`)
	info := New(connectV2(t, importing)).Info(context.Background())
	if !info.Available || info.DataLayout != "" {
		t.Errorf("mid-import info = %#v, want the 1.x layout", info)
	}
	if len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "0 of 1 sessions imported") || !strings.Contains(info.Warnings[0], "Showing the 1.x history") {
		t.Errorf("mid-import warnings = %q", info.Warnings)
	}
}
