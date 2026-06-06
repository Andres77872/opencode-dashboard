package opencode

import (
	"context"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
)

type Source struct {
	store      *store.Store
	path       string
	pathSource string
}

type Option func(*Source)

func WithPath(path, pathSource string) Option {
	return func(s *Source) {
		s.path = path
		s.pathSource = pathSource
	}
}

func New(st *store.Store, opts ...Option) *Source {
	s := &Source{store: st}
	if st != nil {
		s.path = st.Path()
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Source) Info(context.Context) source.SourceInfo {
	available := s != nil && s.store != nil && s.store.IsValidSchema()
	path := ""
	pathSource := ""
	if s != nil {
		path = s.path
		pathSource = s.pathSource
	}
	info := source.SourceInfo{
		ID:           source.SourceOpenCode,
		Label:        "OpenCode",
		Kind:         "sqlite",
		Available:    available,
		Path:         path,
		PathSource:   pathSource,
		ReadOnly:     true,
		LocalOnly:    true,
		Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"},
		CostPolicy: source.CostPolicy{
			Status:   string(stats.CostReported),
			Currency: "USD",
			Note:     "OpenCode stores reported costs in the SQLite database",
		},
		Privacy: source.PrivacyInfo{ReadOnly: true, LocalOnly: true, Redaction: true},
	}
	if !available {
		info.Diagnostics = source.SourceDiagnostics{Status: "unavailable", Reason: "OpenCode database is not available or schema is invalid"}
	}
	return info
}

func (s *Source) Overview(ctx context.Context, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	result, err := stats.Overview(ctx, s.store, pq)
	if err != nil {
		return result, err
	}
	annotateOverview(&result)
	return result, nil
}

func (s *Source) Daily(ctx context.Context, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	result, err := stats.Daily(ctx, s.store, pq, granularity...)
	if err != nil {
		return result, err
	}
	annotateDaily(&result)
	return result, nil
}

func (s *Source) DailyDimension(ctx context.Context, dimension string, pq stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	result, err := stats.DailyDimension(ctx, s.store, dimension, pq)
	if err != nil {
		return result, err
	}
	annotateDailyDimension(&result)
	return result, nil
}

func (s *Source) Models(ctx context.Context, pq stats.PeriodQuery) (stats.ModelStats, error) {
	result, err := stats.Models(ctx, s.store, pq)
	if err != nil {
		return result, err
	}
	annotateModels(&result)
	return result, nil
}

func (s *Source) Tools(ctx context.Context, pq stats.PeriodQuery) (stats.ToolStats, error) {
	result, err := stats.Tools(ctx, s.store, pq)
	if err != nil {
		return result, err
	}
	annotateTools(&result)
	return result, nil
}

func (s *Source) Projects(ctx context.Context, pq stats.PeriodQuery) (stats.ProjectStats, error) {
	result, err := stats.Projects(ctx, s.store, pq)
	if err != nil {
		return result, err
	}
	annotateProjects(&result)
	return result, nil
}

func (s *Source) ProjectByID(ctx context.Context, id string, pq stats.PeriodQuery, page, limit int) (*stats.ProjectDetail, error) {
	result, err := stats.ProjectByID(ctx, s.store, id, pq, page, limit)
	if err != nil {
		return result, err
	}
	annotateProjectDetail(result)
	return result, nil
}

func (s *Source) Sessions(ctx context.Context, query stats.SessionQuery) (stats.SessionList, error) {
	result, err := stats.SessionsWithQuery(ctx, s.store, query)
	if err != nil {
		return result, err
	}
	annotateSessions(&result)
	return result, nil
}

func (s *Source) SessionByID(ctx context.Context, id string) (*stats.SessionDetail, error) {
	result, err := stats.SessionByID(ctx, s.store, id)
	if err != nil {
		return result, err
	}
	annotateSessionDetail(result)
	return result, nil
}

func (s *Source) Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error) {
	result, err := stats.MessagesByPeriod(ctx, s.store, pq, page, limit, sort)
	if err != nil {
		return result, err
	}
	annotateMessages(&result)
	return result, nil
}

func (s *Source) MessageByID(ctx context.Context, id string) (*stats.MessageDetail, error) {
	result, err := stats.MessageByID(ctx, s.store, id)
	if err != nil {
		return result, err
	}
	annotateMessageDetail(result)
	return result, nil
}

func (s *Source) Config(ctx context.Context) (stats.ConfigView, error) {
	result, err := stats.Config(ctx, s.store)
	if err != nil {
		return result, err
	}
	annotateConfig(&result)
	return result, nil
}

func (s *Source) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}
