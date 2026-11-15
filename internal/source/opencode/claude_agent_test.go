package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	usagecache "opencode-dashboard/internal/cache"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/claudecode"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func claudeAgentFixture(t *testing.T, v2 bool) (*Source, time.Time) {
	t.Helper()
	base := time.Now().UTC().Add(-48 * time.Hour).Truncate(24 * time.Hour)
	usage := &fixture.V2Usage{Cost: 1, Input: 10, Output: 5, Reasoning: 2, CacheRead: 1, CacheWrite: 3}
	claudeUsage := &fixture.V2Usage{Cost: 99, Input: 999, Output: 999, CacheRead: 999, CacheWrite: 999}
	tool := fixture.V2Tool("call", "shell", map[string]any{"status": "completed"})
	rows := []fixture.V2Row{
		{ID: "claude-user", SessionID: "claude", Type: "user", Seq: 1, Created: base, Data: fixture.V2User("prompt")},
		{ID: "claude-response", SessionID: "claude", Type: "assistant", Seq: 2, Created: base.Add(time.Minute), Data: fixture.V2Assistant("claude-agent", "claude-sonnet-4-5", claudeUsage, tool)},
		{ID: "claude-failed", SessionID: "claude", Type: "assistant", Seq: 3, Created: base.Add(2 * time.Minute), Data: fixture.V2Assistant("claude-agent", "claude-sonnet-4-5", nil)},
		{ID: "mixed-claude-user", SessionID: "mixed", Type: "user", Seq: 1, Created: base, Data: fixture.V2User("Claude prompt")},
		{ID: "mixed-claude", SessionID: "mixed", Type: "assistant", Seq: 2, Created: base.Add(time.Minute), Data: fixture.V2Assistant("claude-agent", "claude-sonnet-4-5", claudeUsage, tool)},
		{ID: "claude-compaction", SessionID: "mixed", Type: "compaction", Seq: 3, Created: base.Add(2 * time.Minute), Data: fixture.V2Compaction("completed", claudeUsage, nil)},
		{ID: "openai-user", SessionID: "mixed", Type: "user", Seq: 4, Created: base.Add(9 * time.Minute), Data: fixture.V2User("OpenAI prompt")},
		{ID: "openai", SessionID: "mixed", Type: "assistant", Seq: 5, Created: base.Add(10 * time.Minute), Data: fixture.V2Assistant("openai", "gpt-5", usage, tool)},
		{ID: "openai-compaction", SessionID: "mixed", Type: "compaction", Seq: 6, Created: base.Add(11 * time.Minute), Data: fixture.V2Compaction("completed", usage, nil)},
		{ID: "anthropic-user", SessionID: "mixed", Type: "user", Seq: 7, Created: base.Add(2 * time.Hour), Data: fixture.V2User("paid Claude API prompt")},
		{ID: "anthropic", SessionID: "mixed", Type: "assistant", Seq: 8, Created: base.Add(2*time.Hour + time.Minute), Data: fixture.V2Assistant("anthropic", "claude-sonnet-4-5", usage, tool)},
		{ID: "unknown", SessionID: "unknown", Type: "assistant", Seq: 1, Created: base.Add(3 * time.Hour), Data: fixture.V2Assistant("", "custom", usage)},
	}
	var path string
	var err error
	if v2 {
		b := fixture.NewV2Builder().AddProject(fixture.NewProject("project", "/work/project"))
		for _, id := range []string{"claude", "mixed", "unknown"} {
			b.AddSession(fixture.V2Session{ID: id, ProjectID: "project", Created: base})
		}
		for _, row := range rows {
			b.AddRow(row)
		}
		// The current model is Claude Agent even though earlier paid-provider
		// requests must stay counted. It must never determine session ownership.
		b.Exec(`UPDATE session_v2 SET model = '{"providerID":"claude-agent","id":"sonnet"}' WHERE id = 'mixed'`)
		path, err = b.Build(context.Background())
	} else {
		b := fixture.NewBuilder().AddProject(fixture.NewProject("project", "/work/project"))
		for _, id := range []string{"claude", "mixed", "unknown"} {
			session := fixture.NewSession(id, "project").CreatedAt(base).UpdatedAt(base.Add(4 * time.Hour))
			for _, row := range rows {
				if row.SessionID != id || row.Type == "compaction" {
					continue
				}
				if row.Type == "user" {
					session.AddUserMessage(row.ID, row.Created)
					continue
				}
				model := row.Data["model"].(map[string]any)
				u := usage
				if model["providerID"] == "claude-agent" {
					u = claudeUsage
				}
				session.AddAssistantMessage(row.ID, row.Created, u.Cost, model["id"].(string), model["providerID"].(string), u.Input, u.Output, u.Reasoning, u.CacheRead, u.CacheWrite)
				if row.ID != "unknown" {
					b.AddPart(fixture.NewPart("tool-"+row.ID, id, `{"type":"tool","tool":"shell","state":{"status":"completed"}}`).MessageID(row.ID).CreatedAt(row.Created))
				}
			}
			b.AddSession(session)
		}
		path, err = b.Build(context.Background())
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	st, err := store.Connect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st), base
}

func TestClaudeAgentAccountingAcrossLiveAndCachedViews(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		name := "v1"
		if v2 {
			name = "v2"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			live, base := claudeAgentFixture(t, v2)
			cache, err := usagecache.Open(ctx, filepath.Join(t.TempDir(), "cache.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cache.Close() })
			// One retained request is before the cutoff; the other two are in
			// the live gap. The same event must never reappear at that boundary.
			if _, err := cache.SyncSourceWithOptions(ctx, live, usagecache.SyncOptions{Cutoff: base.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			cached := usagecache.WrapSource(cache, live)
			t.Cleanup(func() { cached.Close() })
			for _, src := range []source.Source{live, cached} {
				assertClaudeAgentExcluded(t, src, v2)
			}
		})
	}
}

func assertClaudeAgentExcluded(t *testing.T, src source.Source, v2 bool) {
	t.Helper()
	ctx := context.Background()
	pq := stats.PeriodQuery{Period: "all"}
	requests, messages := int64(3), int64(5)
	if v2 {
		requests++
		messages++
	}
	wantTokens := stats.TokenStats{Input: 10 * requests, Output: 5 * requests, Reasoning: 2 * requests, Cache: stats.CacheStats{Read: requests, Write: 3 * requests}}
	got, err := src.Overview(ctx, pq)
	if err != nil || got.Requests != requests || got.Messages != messages || got.Sessions != 2 || got.Cost != float64(requests) || got.Tokens != wantTokens {
		t.Fatalf("%T Overview = %+v, %v; want %d requests, %d messages, 2 sessions, tokens %+v", src, got, err, requests, messages, wantTokens)
	}
	for _, gran := range []stats.Granularity{stats.GranularityDay, stats.GranularityHour} {
		daily, err := src.Daily(ctx, pq, gran)
		if err != nil {
			t.Fatal(err)
		}
		var cost float64
		var count int64
		for _, day := range daily.Days {
			cost += day.Cost
			count += day.Requests
		}
		if cost != float64(requests) || count != requests {
			t.Fatalf("%T Daily(%s) = cost %v requests %d", src, gran, cost, count)
		}
	}
	models, err := src.Models(ctx, pq)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models.Models {
		if model.ProviderID == "claude-agent" {
			t.Fatalf("Claude Agent model leaked into %T", src)
		}
	}
	tools, err := src.Tools(ctx, pq)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Invocations != 2 {
		t.Fatalf("%T tools = %+v, %v; want 2 retained tool calls", src, tools, err)
	}
	sessions, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 20})
	if err != nil || sessions.Total != 2 {
		t.Fatalf("%T sessions = %+v, %v; want 2", src, sessions, err)
	}
	project, err := src.ProjectByID(ctx, "project", pq, 1, 20)
	if err != nil || project == nil || project.TotalSessions != 2 || project.Cost != float64(requests) || project.Tokens != wantTokens {
		t.Fatalf("%T project = %+v, %v", src, project, err)
	}
	list, err := src.Messages(ctx, pq, 1, 100, stats.DefaultMessageSort())
	if err != nil || list.Total != messages {
		t.Fatalf("%T messages = %+v, %v; want %d", src, list, err, messages)
	}
	for _, entry := range list.Messages {
		if entry.ProviderID == "claude-agent" || entry.ID == "mixed-claude-user" || entry.ID == "claude-user" {
			t.Fatalf("excluded message leaked: %+v", entry)
		}
	}
	if detail, err := src.SessionByID(ctx, "claude"); err != nil || detail != nil {
		t.Fatalf("pure Claude Agent detail = %+v, %v; want absent", detail, err)
	}
	if detail, err := src.MessageByID(ctx, "claude-response"); err != nil || detail != nil {
		t.Fatalf("excluded message detail = %+v, %v; want absent", detail, err)
	}
	detail, err := src.SessionByID(ctx, "mixed")
	if err != nil || detail == nil || detail.TotalCost != float64(requests-1) {
		t.Fatalf("mixed session detail = %+v, %v", detail, err)
	}
}

func TestClaudeAgentConsumptionCountedOnlyByClaudeCode(t *testing.T) {
	ctx := context.Background()
	openCode, base := claudeAgentFixture(t, true)
	home := t.TempDir()
	project := filepath.Join(home, "projects", "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	// This is the same response saved as claude-response in OpenCode. Claude
	// Code must remain authoritative regardless of whether either source uses
	// the dashboard cache.
	entry := map[string]any{
		"type": "assistant", "uuid": "claude-response", "sessionId": "claude", "timestamp": base.Add(time.Minute).Format(time.RFC3339Nano), "costUSD": 99,
		"message": map[string]any{"id": "api-response", "role": "assistant", "model": "claude-sonnet-4-5", "content": []any{},
			"usage": map[string]int{"input_tokens": 999, "output_tokens": 999, "cache_read_input_tokens": 999, "cache_creation_input_tokens": 999}},
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "claude.jsonl"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	claude := claudecode.New(claudecode.Options{ClaudeHome: home})
	cache, err := usagecache.Open(ctx, filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Close() })
	for _, cached := range []bool{false, true} {
		reg := source.NewRegistry(source.SourceOpenCode)
		for _, src := range []source.Source{openCode, claude} {
			if cached {
				if _, err := cache.SyncSourceWithOptions(ctx, src, usagecache.SyncOptions{Cutoff: base.Add(24 * time.Hour)}); err != nil {
					t.Fatal(err)
				}
				wrapper := usagecache.WrapSource(cache, src)
				t.Cleanup(func() { wrapper.Close() })
				src = wrapper
			}
			if err := reg.Register(src); err != nil {
				t.Fatal(err)
			}
		}
		all, err := source.AggregateOverview(ctx, reg, stats.PeriodQuery{Period: "all"}, source.AggregateOptions{IncludeTrend: true})
		if err != nil || len(all.Errors) != 0 || all.Total.Requests != 5 || all.Total.Tokens.Input != 1039 || all.Total.Tokens.Cache.Read != 1003 {
			t.Fatalf("combined (cached=%v) = %+v, %v; want 4 OpenCode requests + 1 Claude Code request", cached, all, err)
		}
	}
}

func TestClaudeAgentCompactionRespectsProviderSwitch(t *testing.T) {
	base := time.Now().UTC().Add(-24 * time.Hour)
	b := fixture.NewV2Builder().AddProject(fixture.NewProject("p", "/work/p")).
		AddSession(fixture.V2Session{ID: "mixed", ProjectID: "p", Created: base})
	rows := []struct {
		kind string
		data map[string]any
	}{
		{"assistant", fixture.V2Assistant("claude-agent", "sonnet", &fixture.V2Usage{Cost: 100})},
		{"model-switched", map[string]any{"model": map[string]any{"providerID": "openai", "id": "gpt-5"}}},
		{"compaction", fixture.V2Compaction("completed", &fixture.V2Usage{Cost: 1}, nil)},
		{"model-switched", map[string]any{"model": map[string]any{"providerID": "claude-agent", "id": "sonnet"}}},
		{"compaction", fixture.V2Compaction("completed", &fixture.V2Usage{Cost: 100}, nil)},
		{"compaction", fixture.V2Compaction("completed", &fixture.V2Usage{Cost: 2}, map[string]any{"providerID": "anthropic", "id": "sonnet"})},
		{"model-switched", map[string]any{"unrecognized_model_format": true}},
		{"compaction", fixture.V2Compaction("completed", &fixture.V2Usage{Cost: 3}, nil)},
	}
	for seq, row := range rows {
		b.AddRow(fixture.V2Row{ID: string(rune('a' + seq)), SessionID: "mixed", Type: row.kind, Seq: seq, Created: base.Add(time.Duration(seq) * time.Minute), Data: row.data})
	}
	src := New(connectV2(t, b))
	got, err := src.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil || got.Requests != 3 || got.Cost != 6 {
		t.Fatalf("compactions across provider switches = %+v, %v; want 3 non-Claude-Agent requests costing 6", got, err)
	}
}

type preDedupOpenCode struct {
	*Source
}

func (s preDedupOpenCode) ConsolidationData(ctx context.Context, pq stats.PeriodQuery) (source.ConsolidationData, error) {
	data, err := s.Source.ConsolidationData(ctx, pq)
	if err != nil {
		return data, err
	}
	// Simulate the old collector, which persisted an excluded request and its
	// tool invocation into historical message indexes and hourly rollups.
	data.Messages = append(data.Messages, source.ConsolidationMessage{
		Entry: stats.MessageEntry{ID: "old-duplicate", SessionID: "mixed", Role: "assistant", ProviderID: "claude-agent", TimeCreated: time.Now().UTC().Add(-72 * time.Hour), Cost: 500, Tokens: &stats.TokenStats{Input: 500}},
		Tools: []source.ConsolidationTool{{Name: "duplicated-tool", Status: "completed"}},
	})
	return data, nil
}

func TestClaudeAgentHistoricalCacheIsInvalidatedBeforeReads(t *testing.T) {
	ctx := context.Background()
	live, base := claudeAgentFixture(t, true)
	path := filepath.Join(t.TempDir(), "cache.sqlite")
	cache, err := usagecache.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.SyncSourceWithOptions(ctx, preDedupOpenCode{live}, usagecache.SyncOptions{Cutoff: base.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE source_state SET data_version = data_version - 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	cache, err = usagecache.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Close() })
	wrapper := usagecache.WrapSource(cache, live)
	t.Cleanup(func() { wrapper.Close() })
	assertClaudeAgentExcluded(t, wrapper, true)
	if _, err := cache.SyncSourceWithOptions(ctx, live, usagecache.SyncOptions{Cutoff: base.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	assertClaudeAgentExcluded(t, wrapper, true)
}
