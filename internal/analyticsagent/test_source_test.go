package analyticsagent

import (
	"context"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

type analyticsTestSource struct {
	info      source.SourceInfo
	overview  stats.OverviewStats
	daily     stats.DailyStats
	dimension stats.DailyDimensionStats
	models    stats.ModelStats
	tools     stats.ToolStats
	projects  stats.ProjectStats
	sessions  stats.SessionList
	session   *stats.SessionDetail
	messages  stats.MessageList
	message   *stats.MessageDetail
	config    stats.ConfigView
}

func newAnalyticsTestSource(id source.SourceID, sessions int64) *analyticsTestSource {
	provenance := &stats.CostProvenance{Status: stats.CostReported, Currency: "USD", ReportedCount: 1}
	return &analyticsTestSource{
		info: source.SourceInfo{
			ID: id, Label: string(id), Available: true, ReadOnly: true, LocalOnly: true,
			Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"},
			CostPolicy:   source.CostPolicy{Status: string(stats.CostReported), Currency: "USD"},
		},
		overview:  stats.OverviewStats{SourceID: string(id), Sessions: sessions, Messages: sessions * 2, Cost: float64(sessions), Days: 7, CostStatus: stats.CostReported, CostProvenance: provenance},
		daily:     stats.DailyStats{SourceID: string(id), Granularity: stats.GranularityDay, Days: []stats.DayStats{{SourceID: string(id), Date: "2026-07-14", Sessions: sessions, Messages: sessions * 2, Cost: float64(sessions), CostStatus: stats.CostReported, CostProvenance: provenance}}, CostStatus: stats.CostReported, CostProvenance: provenance},
		dimension: stats.DailyDimensionStats{SourceID: string(id), Dimension: "project", Period: "7d", Days: []stats.DimensionDayStats{{SourceID: string(id), Date: "2026-07-14", Dimension: "project-id", Sessions: sessions, Messages: sessions * 2, Cost: float64(sessions), CostStatus: stats.CostReported, CostProvenance: provenance}}, CostStatus: stats.CostReported, CostProvenance: provenance},
		models:    stats.ModelStats{SourceID: string(id), Models: []stats.ModelEntry{{SourceID: string(id), ModelID: "model-safe", ProviderID: "provider-safe", Sessions: sessions, Messages: sessions * 2, Cost: float64(sessions), CostStatus: stats.CostReported, CostProvenance: provenance}}, CostStatus: stats.CostReported, CostProvenance: provenance},
		tools:     stats.ToolStats{SourceID: string(id), Tools: []stats.ToolEntry{{SourceID: string(id), Name: "shell", Invocations: sessions, Successes: sessions, Sessions: sessions}}},
		projects:  stats.ProjectStats{SourceID: string(id), Projects: []stats.ProjectEntry{{SourceID: string(id), ProjectID: "project-id", ProjectName: "project-name", Sessions: sessions, Messages: sessions * 2, Cost: float64(sessions), CostStatus: stats.CostReported, CostProvenance: provenance}}, CostStatus: stats.CostReported, CostProvenance: provenance},
		sessions:  stats.SessionList{SourceID: string(id)},
		messages:  stats.MessageList{SourceID: string(id)},
	}
}

func (s *analyticsTestSource) Info(context.Context) source.SourceInfo { return s.info }
func (s *analyticsTestSource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	return s.overview, nil
}
func (s *analyticsTestSource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return s.daily, nil
}
func (s *analyticsTestSource) DailyDimension(context.Context, string, stats.PeriodQuery, ...stats.Granularity) (stats.DailyDimensionStats, error) {
	return s.dimension, nil
}
func (s *analyticsTestSource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return s.models, nil
}
func (s *analyticsTestSource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return s.tools, nil
}
func (s *analyticsTestSource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return s.projects, nil
}
func (s *analyticsTestSource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}
func (s *analyticsTestSource) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return s.sessions, nil
}
func (s *analyticsTestSource) SessionByID(context.Context, string) (*stats.SessionDetail, error) {
	return s.session, nil
}
func (s *analyticsTestSource) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	return s.messages, nil
}
func (s *analyticsTestSource) MessageByID(context.Context, string) (*stats.MessageDetail, error) {
	return s.message, nil
}
func (s *analyticsTestSource) Config(context.Context) (stats.ConfigView, error) { return s.config, nil }
