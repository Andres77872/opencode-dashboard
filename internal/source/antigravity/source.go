// Package antigravity passively reads Antigravity's SQLite conversation store.
package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const antigravitySourceID = string(source.SourceAntigravity)

type Options struct {
	Home, PathSource string
	PricingAliases   source.PricingAliasResolver
	PricingRates     source.PricingRateIndex
	SnapshotTTL      time.Duration
}
type Source struct {
	opts         Options
	mu           sync.Mutex
	snapshot     *snapshot
	refs         map[string]stepRef
	loadedAt     time.Time
	pricingID    string
	basePricing  pricingSnapshot
	diagnostics  source.SourceDiagnostics
	diagnosticAt time.Time
}

func New(opts Options) *Source {
	if opts.Home == "" {
		home, _ := os.UserHomeDir()
		opts.Home = filepath.Join(home, ".gemini", "antigravity")
	}
	if opts.PathSource == "" {
		opts.PathSource = "$HOME/.gemini/antigravity"
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = 15 * time.Second
	}
	return &Source{opts: opts, basePricing: readPricing()}
}

func (s *Source) Info(ctx context.Context) source.SourceInfo {
	files, _ := filepath.Glob(filepath.Join(s.opts.Home, "conversations", "*.db"))
	s.mu.Lock()
	diag := s.diagnostics
	if diag.Status == "unavailable" && time.Since(s.diagnosticAt) >= s.opts.SnapshotTTL {
		diag = source.SourceDiagnostics{}
	}
	s.mu.Unlock()
	available := len(files) > 0
	if diag.Status == "" {
		diag.Status = "ok"
		diag.ScannedFiles = int64(len(files))
	}
	if !available {
		diag.Status = "unavailable"
		diag.Reason = "no Antigravity conversation databases found"
	}
	if diag.Status == "unavailable" {
		available = false
	}
	return source.SourceInfo{ID: source.SourceAntigravity, Label: "Antigravity", Kind: "sqlite", Enabled: true, Available: available, Path: s.opts.Home, PathSource: s.opts.PathSource, ReadOnly: true, LocalOnly: true, DataLayout: "antigravity-sqlite-pb-v1", Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"}, Diagnostics: diag,
		Warnings:   []string{"Persisted generations do not expose every outbound retry or failed attempt. Local transcripts may contain sensitive content."},
		CostPolicy: source.CostPolicy{Status: string(stats.CostEstimatedAPIEquivalent), Currency: "USD", PricingSnapshotID: s.loadPricing(ctx).ID, PricingSource: s.basePricing.Source, Note: apiEquivalentNote}, Privacy: source.PrivacyInfo{PlaintextTranscripts: true, ReadOnly: true, LocalOnly: true, Redaction: true}}
}

func (s *Source) snapshotFor(ctx context.Context, _ stats.PeriodQuery) (*snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pricing := s.loadPricing(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot != nil && time.Since(s.loadedAt) < s.opts.SnapshotTTL && s.pricingID == pricing.ID {
		return s.snapshot, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snap, refs, err := s.loadSnapshot(ctx, pricing)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if snap != nil {
		s.diagnostics = snap.diagnostics
	}
	s.diagnosticAt = time.Now()
	if err != nil {
		s.diagnostics.Status = "unavailable"
		s.diagnostics.Reason = "Antigravity history could not be read"
		return nil, err
	}
	s.snapshot, s.refs, s.loadedAt, s.pricingID = snap, refs, time.Now(), pricing.ID
	return snap, nil
}
func (s *Source) Invalidate() {
	s.mu.Lock()
	s.snapshot = nil
	s.loadedAt = time.Time{}
	s.diagnostics = source.SourceDiagnostics{}
	s.mu.Unlock()
}

func (s *Source) SessionByID(ctx context.Context, id string) (*stats.SessionDetail, error) {
	snap, err := s.snapshotFor(ctx, stats.PeriodQuery{})
	if err != nil {
		return nil, err
	}
	return snap.sessionByID(id), nil
}
func (s *Source) MessageByID(ctx context.Context, id string) (*stats.MessageDetail, error) {
	snap, err := s.snapshotFor(ctx, stats.PeriodQuery{})
	if err != nil {
		return nil, err
	}
	detail := snap.messageByID(id)
	if detail == nil {
		return nil, nil
	}
	s.mu.Lock()
	refs := s.refs
	s.mu.Unlock()
	return s.loadDetail(ctx, detail, refs)
}

func (s *Source) Config(ctx context.Context) (stats.ConfigView, error) {
	path := filepath.Join(s.opts.Home, "mcp_config.json")
	v := stats.ConfigView{SourceID: antigravitySourceID, Path: path, Format: stats.ConfigFormatJSON}
	if err := ctx.Err(); err != nil {
		return v, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if info.Size() > maxBlobBytes {
		return v, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	v.Exists = true
	var content map[string]any
	if err := json.Unmarshal(data, &content); err != nil {
		v.ParseError = sanitizeParseError(err)
		return v, nil
	}
	v.Content, v.Redacted = redactConfigMap(content)
	v.Raw, _ = encodeRedactedJSON(v.Content)
	return v, nil
}

func accounting(messages []*messageRecord) *stats.RequestAccounting {
	a := &stats.RequestAccounting{TraceCoverage: stats.TraceCoverageSuccessfulOnly}
	for _, msg := range messages {
		if msg.Entry.RequestTrace == "" {
			continue
		}
		switch msg.Entry.UsageStatus {
		case stats.UsageStatusRecorded:
			a.UsageRecorded++
		case stats.UsageStatusRecovered:
			a.UsageRecovered++
		default:
			a.UsageUnavailable++
			a.UsageUnavailableReasons.Unknown++
		}
	}
	return a
}

func (s *Source) Overview(ctx context.Context, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	return snap.overview(pq)
}

func (s *Source) Daily(ctx context.Context, pq stats.PeriodQuery, gran ...stats.Granularity) (stats.DailyStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	return snap.daily(pq, gran...)
}

func (s *Source) DailyDimension(ctx context.Context, dimension string, pq stats.PeriodQuery, gran ...stats.Granularity) (stats.DailyDimensionStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	return snap.dailyDimension(dimension, pq, gran...)
}

func (s *Source) Models(ctx context.Context, pq stats.PeriodQuery) (stats.ModelStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.ModelStats{}, err
	}
	return snap.models(pq)
}

func (s *Source) Tools(ctx context.Context, pq stats.PeriodQuery) (stats.ToolStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.ToolStats{}, err
	}
	return snap.tools(pq)
}

func (s *Source) Projects(ctx context.Context, pq stats.PeriodQuery) (stats.ProjectStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	return snap.projects(pq)
}

func (s *Source) ProjectByID(ctx context.Context, id string, pq stats.PeriodQuery, page, limit int) (*stats.ProjectDetail, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return nil, err
	}
	return snap.projectByID(id, pq, page, limit)
}

func (s *Source) Sessions(ctx context.Context, query stats.SessionQuery) (stats.SessionList, error) {
	snap, err := s.snapshotFor(ctx, stats.PeriodQuery{})
	if err != nil {
		return stats.SessionList{}, err
	}
	return snap.sessions(query)
}

func (s *Source) Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.MessageList{}, err
	}
	return snap.messages(pq, page, limit, sort)
}
