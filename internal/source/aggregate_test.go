package source

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// aggFake is a configurable Source for exercising AggregateOverview.
type aggFake struct {
	info          SourceInfo
	overview      stats.OverviewStats
	models        []stats.ModelEntry
	tools         []stats.ToolEntry
	projects      []stats.ProjectEntry
	trend         []stats.DayStats
	modelTrend    []stats.DimensionDayStats
	overviewErr   error
	modelsErr     error
	toolsErr      error
	projectsErr   error
	trendErr      error
	modelTrendErr error
	block         time.Duration
}

func newAggFake(id SourceID) *aggFake {
	return &aggFake{info: SourceInfo{ID: id, Label: string(id) + " label", Available: true}}
}

func (s *aggFake) Info(context.Context) SourceInfo { return s.info }

func (s *aggFake) Overview(ctx context.Context, _ stats.PeriodQuery) (stats.OverviewStats, error) {
	if s.overviewErr != nil {
		return stats.OverviewStats{}, s.overviewErr
	}
	if s.block > 0 {
		select {
		case <-time.After(s.block):
		case <-ctx.Done():
			return stats.OverviewStats{}, ctx.Err()
		}
	}
	return s.overview, nil
}

func (s *aggFake) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	if s.trendErr != nil {
		return stats.DailyStats{}, s.trendErr
	}
	return stats.DailyStats{Days: s.trend}, nil
}

func (s *aggFake) DailyDimension(_ context.Context, dimension string, _ stats.PeriodQuery, _ ...stats.Granularity) (stats.DailyDimensionStats, error) {
	if s.modelTrendErr != nil {
		return stats.DailyDimensionStats{}, s.modelTrendErr
	}
	return stats.DailyDimensionStats{Dimension: dimension, Days: s.modelTrend}, nil
}

func (s *aggFake) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	if s.modelsErr != nil {
		return stats.ModelStats{}, s.modelsErr
	}
	return stats.ModelStats{Models: s.models}, nil
}

func (s *aggFake) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	if s.toolsErr != nil {
		return stats.ToolStats{}, s.toolsErr
	}
	return stats.ToolStats{Tools: s.tools}, nil
}

func (s *aggFake) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	if s.projectsErr != nil {
		return stats.ProjectStats{}, s.projectsErr
	}
	return stats.ProjectStats{Projects: s.projects}, nil
}

func (s *aggFake) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}

func (s *aggFake) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return stats.SessionList{}, nil
}

func (s *aggFake) SessionByID(context.Context, string) (*stats.SessionDetail, error) { return nil, nil }

func (s *aggFake) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	return stats.MessageList{}, nil
}

func (s *aggFake) MessageByID(context.Context, string) (*stats.MessageDetail, error) { return nil, nil }

func (s *aggFake) Config(context.Context) (stats.ConfigView, error) { return stats.ConfigView{}, nil }

func aggTestRegistry(t *testing.T, sources ...Source) *Registry {
	t.Helper()
	reg := NewRegistry(SourceOpenCode)
	for _, src := range sources {
		if err := reg.Register(src); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}
	return reg
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAggregateOverviewMerges(t *testing.T) {
	a := newAggFake(SourceOpenCode)
	a.overview = stats.OverviewStats{Sessions: 8, Messages: 160, Cost: 6, Days: 3,
		Tokens: stats.TokenStats{Input: 1000, Output: 500}, CostStatus: stats.CostReported}
	a.models = []stats.ModelEntry{{ModelID: "claude-x", Messages: 160, Cost: 6, Tokens: stats.TokenStats{Input: 1000, Output: 500}}}
	a.tools = []stats.ToolEntry{{Name: "bash", Invocations: 30}}
	a.projects = []stats.ProjectEntry{{ProjectID: "p1", ProjectName: "Proj1", Messages: 160, Tokens: stats.TokenStats{Input: 1000, Output: 500}}}

	b := newAggFake(SourceCodex)
	b.overview = stats.OverviewStats{Sessions: 4, Messages: 80, Cost: 3, Days: 2,
		Tokens: stats.TokenStats{Input: 200, Output: 100}, CostStatus: stats.CostEstimatedAPIEquivalent}
	b.models = []stats.ModelEntry{{ModelID: "gpt-y", Messages: 80, Cost: 3, Tokens: stats.TokenStats{Input: 200, Output: 100}}}
	b.tools = []stats.ToolEntry{{Name: "edit", Invocations: 50}}
	b.projects = []stats.ProjectEntry{{ProjectID: "p2", ProjectName: "Proj2", Messages: 80, Tokens: stats.TokenStats{Input: 200, Output: 100}}}

	reg := aggTestRegistry(t, a, b)
	got, err := AggregateOverview(context.Background(), reg, stats.PeriodQuery{Period: "all"}, AggregateOptions{TopN: 10})
	if err != nil {
		t.Fatalf("AggregateOverview error: %v", err)
	}

	if got.Total.Sessions != 12 || got.Total.Messages != 240 {
		t.Errorf("totals = sessions %d, messages %d; want 12, 240", got.Total.Sessions, got.Total.Messages)
	}
	if got.Total.Tokens.Input != 1200 || got.Total.Tokens.Output != 600 {
		t.Errorf("token totals = in %d, out %d; want 1200, 600", got.Total.Tokens.Input, got.Total.Tokens.Output)
	}
	if got.Total.Days != 3 { // max of per-source days when no trend
		t.Errorf("Total.Days = %d, want 3", got.Total.Days)
	}
	if !approx(got.MessagesPerSession, 20) {
		t.Errorf("MessagesPerSession = %v, want 20", got.MessagesPerSession)
	}
	if !approx(got.TokensPerMessage.Input, 5) || !approx(got.TokensPerMessage.Output, 2.5) {
		t.Errorf("TokensPerMessage = %+v, want input 5 output 2.5", got.TokensPerMessage)
	}
	if got.TokenDistribution != got.Total.Tokens {
		t.Errorf("TokenDistribution %+v != Total.Tokens %+v", got.TokenDistribution, got.Total.Tokens)
	}

	if len(got.Sources) != 2 {
		t.Fatalf("Sources len = %d, want 2", len(got.Sources))
	}
	// Shares should sum to ~1.
	var msgShare, tokShare float64
	for _, s := range got.Sources {
		msgShare += s.MessageShare
		tokShare += s.TokenShare
	}
	if !approx(msgShare, 1) || !approx(tokShare, 1) {
		t.Errorf("shares msg=%v tok=%v; want ~1 each", msgShare, tokShare)
	}

	// Top signals merged, sorted by tokens desc, each tagged with SourceID.
	if len(got.TopModels) != 2 || got.TopModels[0].ModelID != "claude-x" {
		t.Fatalf("TopModels = %+v; want claude-x first", got.TopModels)
	}
	if got.TopModels[0].SourceID != string(SourceOpenCode) || got.TopModels[1].SourceID != string(SourceCodex) {
		t.Errorf("TopModels source tags = %q, %q", got.TopModels[0].SourceID, got.TopModels[1].SourceID)
	}
	if len(got.TopTools) != 2 || got.TopTools[0].Name != "edit" { // 50 invocations > 30
		t.Errorf("TopTools = %+v; want edit first", got.TopTools)
	}
	if len(got.TopProjects) != 2 || got.TopProjects[0].ProjectID != "p1" {
		t.Errorf("TopProjects = %+v; want p1 first", got.TopProjects)
	}
	if len(got.Errors) != 0 {
		t.Errorf("Errors = %+v, want none", got.Errors)
	}
}

func TestAggregateOverviewTrendDayCount(t *testing.T) {
	a := newAggFake(SourceOpenCode)
	a.overview = stats.OverviewStats{Sessions: 1, Messages: 1, Days: 1}
	a.trend = []stats.DayStats{{Date: "2026-01-01"}, {Date: "2026-01-02"}}
	b := newAggFake(SourceCodex)
	b.overview = stats.OverviewStats{Sessions: 1, Messages: 1, Days: 1}
	b.trend = []stats.DayStats{{Date: "2026-01-02"}, {Date: "2026-01-03"}}

	reg := aggTestRegistry(t, a, b)
	got, err := AggregateOverview(context.Background(), reg, stats.PeriodQuery{Period: "all"}, AggregateOptions{IncludeTrend: true})
	if err != nil {
		t.Fatalf("AggregateOverview error: %v", err)
	}
	if got.Total.Days != 3 { // distinct dates: 01,02,03
		t.Errorf("Total.Days = %d, want 3 distinct dates", got.Total.Days)
	}
	for _, s := range got.Sources {
		if len(s.Trend) == 0 {
			t.Errorf("source %s missing trend", s.SourceID)
		}
	}
}

func TestAggregateModelUsageIsCompleteAndSourceTagged(t *testing.T) {
	a := newAggFake(SourceOpenCode)
	a.overview = stats.OverviewStats{Sessions: 1, Messages: 4, Days: 1}
	a.models = []stats.ModelEntry{
		{ModelID: "anthropic/claude", ProviderID: "openrouter", Messages: 3, Tokens: stats.TokenStats{Input: 30}},
		{ModelID: "gpt", ProviderID: "openai", Messages: 1, Tokens: stats.TokenStats{Input: 10}},
	}
	a.modelTrend = []stats.DimensionDayStats{
		{Date: "2026-01-01", Dimension: "anthropic/claude", Messages: 3, Tokens: stats.TokenStats{Input: 30}},
	}
	b := newAggFake(SourceCodex)
	b.overview = stats.OverviewStats{Sessions: 1, Messages: 2, Days: 1}
	b.models = []stats.ModelEntry{{ModelID: "gpt", ProviderID: "openai", Messages: 2, Tokens: stats.TokenStats{Input: 20}}}
	b.modelTrend = []stats.DimensionDayStats{
		{Date: "2026-01-02", Dimension: "gpt", Messages: 2, Tokens: stats.TokenStats{Input: 20}},
	}

	got, err := AggregateModelUsage(
		context.Background(),
		aggTestRegistry(t, a, b),
		stats.PeriodQuery{Period: "all"},
		ModelUsageOptions{IncludeTrend: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ModelUsage) != 3 {
		t.Fatalf("ModelUsage len = %d, want all 3 model/source entries", len(got.ModelUsage))
	}
	if len(got.ModelTrend) != 2 {
		t.Fatalf("ModelTrend len = %d, want 2", len(got.ModelTrend))
	}
	if got.ModelTrend[0].SourceID != string(SourceOpenCode) || got.ModelTrend[1].SourceID != string(SourceCodex) {
		t.Fatalf("ModelTrend source tags = %q, %q", got.ModelTrend[0].SourceID, got.ModelTrend[1].SourceID)
	}
	for _, model := range got.ModelUsage {
		if model.SourceID == "" {
			t.Fatalf("model usage row was not source tagged: %+v", model)
		}
	}
}

func TestAggregateModelUsageTrendFailureIsPartial(t *testing.T) {
	src := newAggFake(SourceOpenCode)
	src.overview = stats.OverviewStats{Sessions: 1, Messages: 1}
	src.models = []stats.ModelEntry{{ModelID: "m", Messages: 1}}
	src.modelTrendErr = errors.New("private model trend failure")

	got, err := AggregateModelUsage(
		context.Background(),
		aggTestRegistry(t, src),
		stats.PeriodQuery{Period: "7d"},
		ModelUsageOptions{IncludeTrend: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ModelUsage) != 1 || len(got.Errors) != 0 {
		t.Fatalf("model trend failure should not drop model totals: %+v", got)
	}
	if len(got.PartialErrors) != 1 || got.PartialErrors[0].Dimension != "model_trend" {
		t.Fatalf("PartialErrors = %+v, want model_trend", got.PartialErrors)
	}
}

func TestAggregateModelUsageKeepsTrendWhenModelTotalsFail(t *testing.T) {
	src := newAggFake(SourceOpenCode)
	src.modelsErr = errors.New("private model totals failure")
	src.modelTrend = []stats.DimensionDayStats{{Date: "2026-01-01", Dimension: "m", Messages: 2}}

	got, err := AggregateModelUsage(
		context.Background(),
		aggTestRegistry(t, src),
		stats.PeriodQuery{Period: "7d"},
		ModelUsageOptions{IncludeTrend: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Errors) != 0 || len(got.ModelTrend) != 1 {
		t.Fatalf("daily model fallback was dropped: %+v", got)
	}
	if len(got.PartialErrors) != 1 || got.PartialErrors[0].Dimension != "models" {
		t.Fatalf("PartialErrors = %+v, want models", got.PartialErrors)
	}
	if got.ModelTrend[0].SourceID != string(SourceOpenCode) {
		t.Fatalf("fallback trend source_id = %q", got.ModelTrend[0].SourceID)
	}
}

func TestAggregateOverviewSkipsErroredSource(t *testing.T) {
	a := newAggFake(SourceOpenCode)
	a.overview = stats.OverviewStats{Sessions: 5, Messages: 100}
	bad := newAggFake(SourceCodex)
	bad.overviewErr = errors.New("boom")

	reg := aggTestRegistry(t, a, bad)
	got, err := AggregateOverview(context.Background(), reg, stats.PeriodQuery{Period: "all"}, AggregateOptions{})
	if err != nil {
		t.Fatalf("AggregateOverview error: %v", err)
	}
	if got.Total.Sessions != 5 || got.Total.Messages != 100 {
		t.Errorf("totals = %d sessions, %d messages; want 5, 100 (errored source excluded)", got.Total.Sessions, got.Total.Messages)
	}
	if len(got.Sources) != 1 {
		t.Errorf("Sources len = %d, want 1", len(got.Sources))
	}
	if len(got.Errors) != 1 || got.Errors[0].SourceID != string(SourceCodex) {
		t.Fatalf("Errors = %+v; want one for codex", got.Errors)
	}
}

func TestAggregateOverviewReportsPartialDimensionFailures(t *testing.T) {
	src := newAggFake(SourceOpenCode)
	src.overview = stats.OverviewStats{Sessions: 2, Messages: 4}
	src.modelsErr = errors.New("private model failure")
	src.projectsErr = errors.New("private project failure")
	src.trendErr = errors.New("private trend failure")

	got, err := AggregateOverview(
		context.Background(),
		aggTestRegistry(t, src),
		stats.PeriodQuery{Period: "7d"},
		AggregateOptions{IncludeTrend: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Errors) != 0 || len(got.Sources) != 1 {
		t.Fatalf("overview should remain available: errors=%+v sources=%+v", got.Errors, got.Sources)
	}
	want := map[string]bool{"models": true, "projects": true, "trend": true}
	if len(got.PartialErrors) != len(want) {
		t.Fatalf("PartialErrors = %+v", got.PartialErrors)
	}
	for _, partial := range got.PartialErrors {
		if partial.SourceID != string(SourceOpenCode) || !want[partial.Dimension] {
			t.Errorf("unexpected partial failure: %+v", partial)
		}
		if partial.Dimension == "private model failure" {
			t.Fatal("raw adapter error escaped into partial failure metadata")
		}
	}
}

func TestAggregateOverviewPerSourceTimeout(t *testing.T) {
	a := newAggFake(SourceOpenCode)
	a.overview = stats.OverviewStats{Sessions: 7, Messages: 70}
	slow := newAggFake(SourceCodex)
	slow.block = 5 * time.Second

	reg := aggTestRegistry(t, a, slow)
	start := time.Now()
	got, err := AggregateOverview(context.Background(), reg, stats.PeriodQuery{Period: "all"}, AggregateOptions{PerSourceTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("AggregateOverview error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("aggregation took %v; per-source timeout should have bounded it", elapsed)
	}
	if got.Total.Sessions != 7 {
		t.Errorf("Total.Sessions = %d, want 7 (slow source timed out)", got.Total.Sessions)
	}
	if len(got.Errors) != 1 || got.Errors[0].SourceID != string(SourceCodex) {
		t.Fatalf("Errors = %+v; want one timeout for codex", got.Errors)
	}
}

func TestAggregateOverviewEmptyRegistry(t *testing.T) {
	got, err := AggregateOverview(context.Background(), nil, stats.PeriodQuery{Period: "all"}, AggregateOptions{})
	if err != nil {
		t.Fatalf("nil registry error: %v", err)
	}
	if got.Sources == nil || got.TopModels == nil || got.TopProjects == nil || got.TopTools == nil {
		t.Errorf("slices must be non-nil for JSON safety: %+v", got)
	}

	reg := NewRegistry(SourceOpenCode) // registered but empty
	got, err = AggregateOverview(context.Background(), reg, stats.PeriodQuery{Period: "all"}, AggregateOptions{})
	if err != nil {
		t.Fatalf("empty registry error: %v", err)
	}
	if len(got.Sources) != 0 {
		t.Errorf("Sources = %d, want 0", len(got.Sources))
	}
}

func TestRegistryAvailable(t *testing.T) {
	avail := newAggFake(SourceOpenCode)
	unavail := newAggFake(SourceCodex)
	unavail.info.Available = false

	reg := aggTestRegistry(t, avail, unavail)
	got := reg.Available(context.Background())
	if len(got) != 1 {
		t.Fatalf("Available len = %d, want 1", len(got))
	}
	if got[0].Info(context.Background()).ID != SourceOpenCode {
		t.Errorf("Available[0] = %q, want opencode", got[0].Info(context.Background()).ID)
	}

	if reg := (*Registry)(nil); reg.Available(context.Background()) != nil {
		t.Errorf("nil registry Available should be nil")
	}
}
