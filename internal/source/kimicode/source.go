package kimicode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	kimiSourceID         = string(source.SourceKimiCode)
	defaultSnapshotTTL   = 2 * time.Second
	defaultSourceTimeout = 10 * time.Second
	boundedLoadMargin    = 10 * time.Minute
)

type Options struct {
	KimiHome            string
	PathSource          string
	PricingSnapshotPath string
	SnapshotTTL         time.Duration
	ScanTimeout         time.Duration
}

// Source passively reads Kimi Code's local session state and agent wire logs.
// It never writes to KIMI_CODE_HOME.
type Source struct {
	opts Options

	mu         sync.Mutex
	snapshot   *snapshot
	loadedAt   time.Time
	lastDiag   source.SourceDiagnostics
	lastStatus bool
	pricing    pricingSnapshot
	pricingErr error

	bounded         *snapshot
	boundedFrom     time.Time
	boundedLoadedAt time.Time
}

func New(opts Options) *Source {
	if opts.KimiHome == "" {
		opts.KimiHome = defaultKimiHome()
	}
	if opts.PathSource == "" {
		opts.PathSource = "$HOME/.kimi-code"
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = defaultSnapshotTTL
	}
	if opts.ScanTimeout <= 0 {
		opts.ScanTimeout = defaultSourceTimeout
	}
	return &Source{opts: opts}
}

func defaultKimiHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".kimi-code")
	}
	return filepath.Join(home, ".kimi-code")
}

func (s *Source) Info(ctx context.Context) source.SourceInfo {
	if s == nil {
		return unavailableInfo("", "", "Kimi Code source is not configured")
	}
	diag, available := s.currentDiagnostics(ctx)
	return source.SourceInfo{
		ID:           source.SourceKimiCode,
		Label:        "Kimi Code",
		Kind:         "jsonl",
		Available:    available,
		Path:         s.opts.KimiHome,
		PathSource:   s.opts.PathSource,
		ReadOnly:     true,
		LocalOnly:    true,
		Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"},
		Warnings: []string{
			"Kimi Code wire logs are plaintext local files and may contain sensitive content",
			"Kimi Code support is passive, local-only, and read-only",
		},
		Diagnostics: diag,
		CostPolicy: source.CostPolicy{
			Status:            string(stats.CostEstimatedAPIEquivalent),
			Currency:          "USD",
			PricingSnapshotID: s.pricingSnapshotID(ctx),
			Note:              "Kimi Code costs are estimated API-equivalent values, not actual membership or coding-plan spend",
		},
		Privacy: source.PrivacyInfo{
			PlaintextTranscripts: true,
			ReadOnly:             true,
			LocalOnly:            true,
			Redaction:            true,
			Warnings: []string{
				"Local Kimi Code wire logs can contain prompts, reasoning, tool input/output, paths, patches, and secrets",
			},
		},
	}
}

func unavailableInfo(path, pathSource, reason string) source.SourceInfo {
	return source.SourceInfo{
		ID:          source.SourceKimiCode,
		Label:       "Kimi Code",
		Kind:        "jsonl",
		Available:   false,
		Path:        path,
		PathSource:  pathSource,
		ReadOnly:    true,
		LocalOnly:   true,
		Warnings:    []string{"Kimi Code wire logs are plaintext local files and may contain sensitive content"},
		Diagnostics: source.SourceDiagnostics{Status: "unavailable", Reason: reason},
		CostPolicy:  source.CostPolicy{Status: string(stats.CostMissing), Currency: "USD", Note: "Kimi Code source is unavailable"},
		Privacy:     source.PrivacyInfo{PlaintextTranscripts: true, ReadOnly: true, LocalOnly: true, Redaction: true},
	}
}

func (s *Source) Overview(ctx context.Context, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	return snap.overview(pq)
}

func (s *Source) Daily(ctx context.Context, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	return snap.daily(pq, granularity...)
}

func (s *Source) DailyDimension(ctx context.Context, dimension string, pq stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	return snap.dailyDimension(dimension, pq)
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
	snap, err := s.snapshotFor(ctx, stats.PeriodQuery{FromTime: query.FromTime})
	if err != nil {
		return stats.SessionList{}, err
	}
	return snap.sessions(query)
}

func (s *Source) SessionByID(ctx context.Context, id string) (*stats.SessionDetail, error) {
	if snap, matched, err := s.loadSessionSnapshot(ctx, id); err != nil {
		return nil, err
	} else if matched {
		return snap.sessionByID(id), nil
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.sessionByID(id), nil
}

func (s *Source) Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.MessageList{}, err
	}
	return snap.messages(pq, page, limit, sort)
}

func (s *Source) MessageByID(ctx context.Context, id string) (*stats.MessageDetail, error) {
	s.mu.Lock()
	if s.bounded != nil && time.Since(s.boundedLoadedAt) <= s.opts.SnapshotTTL {
		if detail := s.bounded.messageByID(id); detail != nil {
			s.mu.Unlock()
			return detail, nil
		}
	}
	s.mu.Unlock()

	if sessionID, ok := sessionIDFromMessageID(id); ok {
		snap, matched, err := s.loadSessionSnapshot(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if matched {
			return snap.messageByID(id), nil
		}
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.messageByID(id), nil
}

func (s *Source) Config(ctx context.Context) (stats.ConfigView, error) {
	if s == nil {
		return stats.ConfigView{}, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: "Kimi Code source is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return stats.ConfigView{}, err
	}
	path := filepath.Join(s.opts.KimiHome, "config.toml")
	view := stats.ConfigView{SourceID: kimiSourceID, Path: path, Format: stats.ConfigFormatTOML}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return view, nil
		}
		return view, fmt.Errorf("access Kimi Code config: %w", err)
	}
	if info.IsDir() {
		return view, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return view, fmt.Errorf("read Kimi Code config: %w", err)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(content, &parsed); err != nil {
		view.Exists = true
		view.ParseError = sanitizeParseError(err)
		return view, nil
	}
	redacted, changed := redactConfigMap(parsed)
	view.Exists = true
	view.Content = redacted
	view.Redacted = changed
	if raw, err := encodeRedactedTOML(redacted); err == nil {
		view.Raw = raw
	}
	return view, nil
}

func (s *Source) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = nil
	s.loadedAt = time.Time{}
	s.bounded = nil
	s.boundedLoadedAt = time.Time{}
}

func (s *Source) currentDiagnostics(ctx context.Context) (source.SourceDiagnostics, bool) {
	s.mu.Lock()
	if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
		diag := s.snapshot.diagnostics
		available := diag.Status != "unavailable" && diag.Status != "empty"
		s.mu.Unlock()
		return diag, available
	}
	lastDiag := s.lastDiag
	lastAvailable := s.lastStatus
	s.mu.Unlock()

	disc := discoverSessions(ctx, s.opts.KimiHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return disc.diagnostics, false
	}
	if lastDiag.Status != "" && lastAvailable {
		lastDiag.ScannedFiles = disc.diagnostics.ScannedFiles
		return lastDiag, true
	}
	s.setLastDiagnostics(disc.diagnostics, true)
	return disc.diagnostics, true
}

func (s *Source) setLastDiagnostics(diag source.SourceDiagnostics, available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastDiag = diag
	s.lastStatus = available
}

func (s *Source) snapshotFor(ctx context.Context, pq stats.PeriodQuery) (*snapshot, error) {
	if pq.FromTime.IsZero() {
		return s.loadSnapshot(ctx)
	}
	return s.loadBoundedSnapshot(ctx, pq.FromTime)
}

func (s *Source) loadSnapshot(ctx context.Context) (*snapshot, error) {
	if s == nil {
		return nil, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: "Kimi Code source is not configured"}
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()

	s.mu.Lock()
	if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
		snap := s.snapshot
		s.mu.Unlock()
		return snap, nil
	}
	s.mu.Unlock()

	disc := discoverSessions(ctx, s.opts.KimiHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: disc.diagnostics.Reason}
	}
	snap, err := s.parseSessions(ctx, disc.sessions, disc.diagnostics)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.snapshot = snap
	s.loadedAt = time.Now()
	s.lastDiag = snap.diagnostics
	s.lastStatus = snap.diagnostics.Status != "unavailable" && snap.diagnostics.Status != "empty"
	s.mu.Unlock()
	return snap, nil
}

func (s *Source) loadBoundedSnapshot(ctx context.Context, from time.Time) (*snapshot, error) {
	if s == nil {
		return nil, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: "Kimi Code source is not configured"}
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()
	pruneT := from.UTC().Add(-boundedLoadMargin)

	s.mu.Lock()
	if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
		snap := s.snapshot
		s.mu.Unlock()
		return snap, nil
	}
	if s.bounded != nil && time.Since(s.boundedLoadedAt) <= s.opts.SnapshotTTL && !s.boundedFrom.After(pruneT) {
		snap := s.bounded
		s.mu.Unlock()
		return snap, nil
	}
	s.mu.Unlock()

	disc := discoverSessions(ctx, s.opts.KimiHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: disc.diagnostics.Reason}
	}
	selected := make([]sessionFiles, 0, len(disc.sessions))
	for _, item := range disc.sessions {
		if !item.ModTime.Before(pruneT) {
			selected = append(selected, item)
		}
	}
	snap, err := s.parseSessions(ctx, selected, disc.diagnostics)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.bounded = snap
	s.boundedFrom = pruneT
	s.boundedLoadedAt = time.Now()
	s.mu.Unlock()
	return snap, nil
}

func (s *Source) loadSessionSnapshot(ctx context.Context, id string) (*snapshot, bool, error) {
	if s == nil {
		return nil, false, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: "Kimi Code source is not configured"}
	}
	if id == "" {
		return nil, false, nil
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()
	disc := discoverSessions(ctx, s.opts.KimiHome)
	if !disc.available {
		return nil, false, source.UnavailableSourceError{ID: source.SourceKimiCode, Reason: disc.diagnostics.Reason}
	}
	for _, item := range disc.sessions {
		if item.ID != id {
			continue
		}
		snap, err := s.parseSessions(ctx, []sessionFiles{item}, disc.diagnostics)
		return snap, true, err
	}
	return nil, false, nil
}

func (s *Source) parseSessions(ctx context.Context, sessions []sessionFiles, diag source.SourceDiagnostics) (*snapshot, error) {
	parsed := make([]parsedSession, 0, len(sessions))
	for _, item := range sessions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		session, parseDiag, err := parseSession(ctx, item)
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "some Kimi Code session files disappeared during scan")
				continue
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "some Kimi Code session files could not be read due to permissions")
				continue
			}
			return nil, err
		}
		diag.MalformedLines += parseDiag.MalformedLines
		diag.UnsupportedEvents += parseDiag.UnsupportedEvents
		parsed = append(parsed, session)
	}
	return normalizeSessions(s.opts.KimiHome, parsed, s.loadPricing(ctx), finalizeDiagnostics(diag)), nil
}

func (s *Source) contextWithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, s.opts.ScanTimeout)
}

func (s *Source) pricingSnapshotID(ctx context.Context) string {
	return s.loadPricing(ctx).ID
}

func finalizeDiagnostics(diag source.SourceDiagnostics) source.SourceDiagnostics {
	if diag.Status == "unavailable" || diag.Status == "empty" {
		return diag
	}
	if diag.ScannedFiles == 0 {
		diag.Status = "empty"
		if diag.Reason == "" {
			diag.Reason = "no Kimi Code agent wire JSONL transcripts found"
		}
		return diag
	}
	if diag.MalformedLines > 0 || diag.UnsupportedEvents > 0 || diag.Reason != "" {
		diag.Status = "partial"
		if diag.Reason == "" {
			diag.Reason = "some Kimi Code JSONL records were skipped"
		}
		return diag
	}
	diag.Status = "ok"
	return diag
}
