package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	usagecache "opencode-dashboard/internal/cache"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func TestConsolidationDataMatchesInteractiveMessageMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src := New(st)
	pq := stats.PeriodQuery{Period: "all"}

	data, err := src.ConsolidationData(ctx, pq)
	if err != nil {
		t.Fatalf("ConsolidationData() failed: %v", err)
	}
	live, err := src.Messages(ctx, pq, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("Messages() failed: %v", err)
	}
	if len(data.Messages) != len(live.Messages) {
		t.Fatalf("message count = %d, want %d", len(data.Messages), len(live.Messages))
	}

	bulkByID := make(map[string]stats.MessageEntry, len(data.Messages))
	modelTokensByID := make(map[string]*stats.TokenStats, len(data.Messages))
	for _, message := range data.Messages {
		bulkByID[message.Entry.ID] = message.Entry
		modelTokensByID[message.Entry.ID] = message.ModelTokens
	}
	for _, want := range live.Messages {
		got, ok := bulkByID[want.ID]
		if !ok {
			t.Errorf("bulk snapshot omitted message %q", want.ID)
			continue
		}
		// The normal source wrapper annotates reported-cost provenance. Compare
		// the raw usage fields that the cache persists.
		want.SourceID, want.SessionTitle = "", ""
		want.CostStatus, want.CostProvenance = "", nil
		got.SourceID, got.SessionTitle = "", ""
		got.CostStatus, got.CostProvenance = "", nil
		if !reflect.DeepEqual(got, want) {
			t.Errorf("message %q mismatch\ngot:  %#v\nwant: %#v", want.ID, got, want)
		}
	}
	if got := modelTokensByID["msg-001-02"]; got == nil || *got != (stats.TokenStats{
		Input: 700, Output: 700, Reasoning: 150,
		Cache: stats.CacheStats{Read: 100, Write: 250},
	}) {
		t.Errorf("multi-step model tokens = %#v, want additive step-finish usage", got)
	}
	if got := modelTokensByID["msg-001-04"]; got != nil {
		t.Errorf("fallback message model tokens = %#v, want nil (use Entry.Tokens)", got)
	}
}

func TestConsolidationDataBoundsOpenRouterAndRedactsToolPayloads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	builder := fixture.NewBuilder().
		AddProject(fixture.NewProject("project-openrouter", "/private/customer/repository"))
	session := fixture.NewSession("session-openrouter", "project-openrouter").
		Title("private prompt-derived title").
		CreatedAt(base.Add(-time.Hour)).
		UpdatedAt(base.Add(3 * time.Hour))
	session.AddAssistantMessage("old", base.Add(-time.Millisecond), 1, "old-model", "openrouter", 1, 2, 3, 4, 5)
	session.AddUserMessage("user", base)
	session.AddAssistantMessage("openrouter", base.Add(time.Minute), 0.25, "anthropic/claude-sonnet-4", "openrouter", 100, 20, 3, 40, 5)
	session.AddAssistantMessage("at-end", base.Add(2*time.Hour), 2, "future-model", "openrouter", 9, 9, 9, 9, 9)
	builder.AddSession(session)
	builder.AddPart(fixture.NewPart(
		"tool-private", "session-openrouter",
		`{"type":"tool","callID":"call-1","tool":"bash","state":{"status":"completed","input":{"token":"DO_NOT_CACHE_INPUT"},"output":"DO_NOT_CACHE_OUTPUT"}}`,
	).MessageID("openrouter").CreatedAt(base.Add(time.Minute)))

	dbPath, err := builder.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))
	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	data, err := New(st).ConsolidationData(ctx, stats.PeriodQuery{
		FromTime: base,
		ToTime:   base.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ConsolidationData() failed: %v", err)
	}
	if len(data.Messages) != 2 {
		t.Fatalf("messages = %d, want the half-open window's user and assistant rows: %#v", len(data.Messages), data.Messages)
	}
	byID := make(map[string]struct {
		entry stats.MessageEntry
		tools int
	}, len(data.Messages))
	for _, message := range data.Messages {
		byID[message.Entry.ID] = struct {
			entry stats.MessageEntry
			tools int
		}{message.Entry, len(message.Tools)}
	}
	openrouter, ok := byID["openrouter"]
	if !ok {
		t.Fatal("OpenRouter assistant row missing")
	}
	if openrouter.entry.ProviderID != "openrouter" || openrouter.entry.ModelID != "anthropic/claude-sonnet-4" {
		t.Errorf("provider/model = %q/%q", openrouter.entry.ProviderID, openrouter.entry.ModelID)
	}
	if openrouter.tools != 1 {
		t.Errorf("OpenRouter tools = %d, want 1", openrouter.tools)
	}
	if len(data.Sessions) != 1 || data.Sessions[0].Title != "" || data.Sessions[0].ProjectName != "repository" {
		t.Errorf("cache-safe session metadata = %#v", data.Sessions)
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"DO_NOT_CACHE_INPUT", "DO_NOT_CACHE_OUTPUT", "private prompt-derived title", "/private/customer"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("consolidation payload leaked %q: %s", secret, encoded)
		}
	}
}

func TestOpenCodeCacheSyncUsesBulkSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	builder := fixture.NewBuilder().AddProject(fixture.NewProject("project", "/workspace/project").Name("Project"))
	session := fixture.NewSession("session", "project").CreatedAt(base).UpdatedAt(base)
	session.AddUserMessage("user", base)
	session.AddAssistantMessage("assistant", base.Add(time.Minute), 0.5, "openai/gpt-5", "openrouter", 10, 20, 3, 4, 5)
	// Empty model ids are not a model group and must be excluded consistently
	// by raw totals, raw trend, consolidation overrides, and cached rollups.
	session.AddAssistantMessage("no-model", base.Add(2*time.Minute), 0.1, "", "openrouter", 9, 9, 9, 9, 9)
	builder.AddSession(session)
	builder.AddPart(fixture.NewStepFinishPart(
		"step-1", "session", "assistant", 100, 10, 1, 2, 3, 0.2,
	).CreatedAt(base.Add(time.Minute)))
	builder.AddPart(fixture.NewStepFinishPart(
		"step-2", "session", "assistant", 200, 20, 4, 5, 6, 0.3,
	).CreatedAt(base.Add(time.Minute)))
	builder.AddPart(fixture.NewPart(
		"tool", "session", `{"type":"tool","tool":"read","state":{"status":"completed","output":"not cached"}}`,
	).MessageID("assistant").CreatedAt(base.Add(time.Minute)))
	dbPath, err := builder.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))
	liveStore, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer liveStore.Close()
	src := New(liveStore)
	pq := stats.PeriodQuery{FromTime: base, ToTime: base.Add(2 * time.Hour)}
	rawModels, err := src.Models(ctx, pq)
	if err != nil {
		t.Fatalf("raw Models() failed: %v", err)
	}
	rawTrend, err := src.DailyDimension(ctx, "model", pq)
	if err != nil {
		t.Fatalf("raw DailyDimension(model) failed: %v", err)
	}
	rawOverview, err := src.Overview(ctx, pq)
	if err != nil {
		t.Fatalf("raw Overview() failed: %v", err)
	}
	wantModelTokens := stats.TokenStats{
		Input: 300, Output: 30, Reasoning: 5,
		Cache: stats.CacheStats{Read: 7, Write: 9},
	}
	if len(rawModels.Models) != 1 || rawModels.Models[0].Tokens != wantModelTokens {
		t.Fatalf("raw model tokens = %#v, want %#v", rawModels.Models, wantModelTokens)
	}
	if len(rawTrend.Days) != 1 || rawTrend.Days[0].Tokens != wantModelTokens {
		t.Fatalf("raw model trend tokens = %#v, want %#v", rawTrend.Days, wantModelTokens)
	}

	cacheStore, err := usagecache.Open(ctx, filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cacheStore.Close()
	report, err := cacheStore.SyncSourceWithOptions(ctx, src, usagecache.SyncOptions{
		Mode:   usagecache.SyncModeRebuild,
		Cutoff: base.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("SyncSourceWithOptions() failed: %v", err)
	}
	if report.Messages != 3 || report.Tools != 1 {
		t.Errorf("bulk sync report = %#v, want 3 messages and 1 tool", report)
	}
	cached, err := cacheStore.MessageByID(ctx, "opencode", "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil || cached.ProviderID != "openrouter" || cached.ModelID != "openai/gpt-5" {
		t.Errorf("cached OpenRouter row = %#v", cached)
	}
	// Message and Overview accounting intentionally retain the upstream row;
	// only model analytics use the additive step-finish override.
	if cached == nil || cached.Tokens == nil || *cached.Tokens != (stats.TokenStats{
		Input: 10, Output: 20, Reasoning: 3,
		Cache: stats.CacheStats{Read: 4, Write: 5},
	}) {
		t.Errorf("cached message tokens = %#v, want original message-level usage", cached)
	}
	cachedModels, err := cacheStore.Models(ctx, "opencode", pq)
	if err != nil {
		t.Fatalf("cached Models() failed: %v", err)
	}
	cachedTrend, err := cacheStore.DailyDimension(ctx, "opencode", "model", pq)
	if err != nil {
		t.Fatalf("cached DailyDimension(model) failed: %v", err)
	}
	if len(cachedModels.Models) != 1 {
		t.Fatalf("cached model rows = %#v, want one", cachedModels.Models)
	}
	if cachedModels.Models[0].Tokens != rawModels.Models[0].Tokens {
		t.Errorf("cached model tokens = %#v, want raw %#v", cachedModels.Models, rawModels.Models)
	}
	if len(cachedTrend.Days) != 1 {
		t.Fatalf("cached model trend rows = %#v, want one", cachedTrend.Days)
	}
	if cachedTrend.Days[0].Tokens != rawTrend.Days[0].Tokens {
		t.Errorf("cached model trend = %#v, want raw %#v", cachedTrend.Days, rawTrend.Days)
	}
	if cachedModels.Models[0].Messages != cachedTrend.Days[0].Messages ||
		cachedModels.Models[0].Cost != cachedTrend.Days[0].Cost {
		t.Errorf("cached model totals disagree with trend: %#v / %#v", cachedModels.Models, cachedTrend.Days)
	}
	cachedOverview, err := cacheStore.Overview(ctx, "opencode", pq)
	if err != nil {
		t.Fatalf("cached Overview() failed: %v", err)
	}
	if cachedOverview.Tokens != rawOverview.Tokens {
		t.Errorf("cached Overview tokens = %#v, want raw message basis %#v", cachedOverview.Tokens, rawOverview.Tokens)
	}
}

// BenchmarkConsolidationDataLargeDB is opt-in so regular tests never touch a
// developer database. Unlike the old generic collector, one iteration is a
// fixed three-query snapshot regardless of the number of messages.
func BenchmarkConsolidationDataLargeDB(b *testing.B) {
	path := os.Getenv("OPENCODE_BENCH_DB")
	if path == "" {
		b.Skip("set OPENCODE_BENCH_DB to benchmark a real OpenCode database")
	}
	st, err := store.Connect(context.Background(), path)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	period := os.Getenv("OPENCODE_BENCH_PERIOD")
	if period == "" {
		period = "30d"
	}
	src := New(st)

	b.ResetTimer()
	for range b.N {
		data, err := src.ConsolidationData(context.Background(), stats.PeriodQuery{Period: period})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(data.Messages)), "messages/op")
		toolCount := 0
		for _, message := range data.Messages {
			toolCount += len(message.Tools)
		}
		b.ReportMetric(float64(toolCount), "tools/op")
	}
}

// BenchmarkGenericCollectionLargeDB retains the former per-message shape as
// an opt-in comparison benchmark. It intentionally omits session pagination,
// so its result is a conservative lower bound for the old cache collector.
func BenchmarkGenericCollectionLargeDB(b *testing.B) {
	path := os.Getenv("OPENCODE_BENCH_DB")
	if path == "" {
		b.Skip("set OPENCODE_BENCH_DB to benchmark a real OpenCode database")
	}
	st, err := store.Connect(context.Background(), path)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	period := os.Getenv("OPENCODE_BENCH_PERIOD")
	if period == "" {
		period = "30d"
	}
	src := New(st)
	pq := stats.PeriodQuery{Period: period}
	sort := stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc}

	b.ResetTimer()
	for range b.N {
		seen := 0
		for page := 1; ; page++ {
			list, err := src.Messages(context.Background(), pq, page, 100, sort)
			if err != nil {
				b.Fatal(err)
			}
			for _, message := range list.Messages {
				if _, err := src.MessageByID(context.Background(), message.ID); err != nil {
					b.Fatal(err)
				}
			}
			seen += len(list.Messages)
			if len(list.Messages) == 0 || int64(page*list.PageSize) >= list.Total {
				break
			}
		}
		b.ReportMetric(float64(seen), "messages/op")
	}
}
