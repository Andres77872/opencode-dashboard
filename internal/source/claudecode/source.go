package claudecode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	claudeSourceID       = string(source.SourceClaudeCode)
	defaultSnapshotTTL   = 2 * time.Second
	defaultSourceTimeout = 10 * time.Second
)

// Options configures the passive Claude Code JSONL source.
type Options struct {
	ClaudeHome          string
	PathSource          string
	PricingSnapshotPath string
	SnapshotTTL         time.Duration
	ScanTimeout         time.Duration
}

// Source passively reads local Claude Code JSONL transcripts. It never writes to
// the Claude home and keeps only a short process-local snapshot cache.
type Source struct {
	opts Options

	mu         sync.Mutex
	snapshot   *snapshot
	loadedAt   time.Time
	lastDiag   source.SourceDiagnostics
	lastStatus bool
	pricing    pricingSnapshot
	pricingErr error
}

func New(opts Options) *Source {
	if opts.ClaudeHome == "" {
		opts.ClaudeHome = defaultClaudeHome()
	}
	if opts.PathSource == "" {
		opts.PathSource = "$HOME/.claude"
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = defaultSnapshotTTL
	}
	if opts.ScanTimeout <= 0 {
		opts.ScanTimeout = defaultSourceTimeout
	}
	return &Source{opts: opts}
}

func defaultClaudeHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".claude")
	}
	return filepath.Join(home, ".claude")
}

func (s *Source) Info(ctx context.Context) source.SourceInfo {
	if s == nil {
		return unavailableInfo("", "", "Claude Code source is not configured")
	}

	diag, available := s.currentDiagnostics(ctx)
	info := source.SourceInfo{
		ID:           source.SourceClaudeCode,
		Label:        "Claude Code",
		Kind:         "jsonl",
		Available:    available,
		Path:         s.opts.ClaudeHome,
		PathSource:   s.opts.PathSource,
		ReadOnly:     true,
		LocalOnly:    true,
		Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"},
		Warnings: []string{
			"Claude Code transcripts are plaintext local files and may contain sensitive content",
			"Claude Code support is passive, local-only, and read-only",
		},
		Diagnostics: diag,
		CostPolicy: source.CostPolicy{
			Status:            string(stats.CostMixed),
			Currency:          "USD",
			PricingSnapshotID: s.pricingSnapshotID(ctx),
			Note:              "Claude costs are reported when present, otherwise computed from a bundled pricing snapshot or marked missing",
		},
		Privacy: source.PrivacyInfo{
			PlaintextTranscripts: true,
			ReadOnly:             true,
			LocalOnly:            true,
			Redaction:            true,
			Warnings: []string{
				"Local transcripts are plaintext and may contain file contents, shell output, or secrets",
			},
		},
	}
	return info
}

func unavailableInfo(path, pathSource, reason string) source.SourceInfo {
	return source.SourceInfo{
		ID:         source.SourceClaudeCode,
		Label:      "Claude Code",
		Kind:       "jsonl",
		Available:  false,
		Path:       path,
		PathSource: pathSource,
		ReadOnly:   true,
		LocalOnly:  true,
		Warnings: []string{
			"Claude Code transcripts are plaintext local files and may contain sensitive content",
		},
		Diagnostics: source.SourceDiagnostics{Status: "unavailable", Reason: reason},
		CostPolicy:  source.CostPolicy{Status: string(stats.CostMissing), Currency: "USD", Note: "Claude Code source is unavailable"},
		Privacy: source.PrivacyInfo{
			PlaintextTranscripts: true,
			ReadOnly:             true,
			LocalOnly:            true,
			Redaction:            true,
		},
	}
}

func (s *Source) Overview(ctx context.Context, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	return snap.overview(pq)
}

func (s *Source) Daily(ctx context.Context, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.DailyStats{}, err
	}
	return snap.daily(pq, granularity...)
}

func (s *Source) DailyDimension(ctx context.Context, dimension string, pq stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	return snap.dailyDimension(dimension, pq)
}

func (s *Source) Models(ctx context.Context, pq stats.PeriodQuery) (stats.ModelStats, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.ModelStats{}, err
	}
	return snap.models(pq)
}

func (s *Source) Tools(ctx context.Context, pq stats.PeriodQuery) (stats.ToolStats, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.ToolStats{}, err
	}
	return snap.tools(pq)
}

func (s *Source) Projects(ctx context.Context, pq stats.PeriodQuery) (stats.ProjectStats, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	return snap.projects(pq)
}

func (s *Source) ProjectByID(ctx context.Context, id string, pq stats.PeriodQuery, page, limit int) (*stats.ProjectDetail, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.projectByID(id, pq, page, limit)
}

func (s *Source) Sessions(ctx context.Context, query stats.SessionQuery) (stats.SessionList, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.SessionList{}, err
	}
	return snap.sessions(query)
}

func (s *Source) SessionByID(ctx context.Context, id string) (*stats.SessionDetail, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.sessionByID(id), nil
}

func (s *Source) Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return stats.MessageList{}, err
	}
	return snap.messages(pq, page, limit, sort)
}

func (s *Source) MessageByID(ctx context.Context, id string) (*stats.MessageDetail, error) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.messageByID(id), nil
}

func (s *Source) Config(ctx context.Context) (stats.ConfigView, error) {
	if s == nil {
		return stats.ConfigView{}, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: "Claude Code source is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return stats.ConfigView{}, err
	}
	path := filepath.Join(s.opts.ClaudeHome, "settings.json")
	view := stats.ConfigView{SourceID: claudeSourceID, Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return view, nil
		}
		return view, fmt.Errorf("access Claude settings: %w", err)
	}
	if info.IsDir() {
		return view, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return view, fmt.Errorf("read Claude settings: %w", err)
	}
	redacted, changed, err := redactJSONDocument(content)
	if err != nil {
		return view, err
	}
	view.Exists = true
	view.Content = redacted
	view.Redacted = changed
	return view, nil
}

// Invalidate drops the process-local snapshot so the next request re-scans the
// passive transcript tree. It does not write any files or maintain a persistent
// index.
func (s *Source) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = nil
	s.loadedAt = time.Time{}
}

func (s *Source) currentDiagnostics(ctx context.Context) (source.SourceDiagnostics, bool) {
	s.mu.Lock()
	if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
		diag := s.snapshot.diagnostics
		available := diag.Status != "unavailable"
		s.mu.Unlock()
		return diag, available
	}
	lastDiag := s.lastDiag
	lastAvailable := s.lastStatus
	s.mu.Unlock()

	disc := discoverTranscripts(ctx, s.opts.ClaudeHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return disc.diagnostics, false
	}
	if lastDiag.Status != "" && lastAvailable {
		lastDiag.ScannedFiles = int64(len(disc.files))
		return lastDiag, true
	}
	diag := disc.diagnostics
	s.setLastDiagnostics(diag, true)
	return diag, true
}

func (s *Source) setLastDiagnostics(diag source.SourceDiagnostics, available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastDiag = diag
	s.lastStatus = available
}

func (s *Source) loadSnapshot(ctx context.Context) (*snapshot, error) {
	if s == nil {
		return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: "Claude Code source is not configured"}
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

	disc := discoverTranscripts(ctx, s.opts.ClaudeHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: disc.diagnostics.Reason}
	}

	pricing := s.loadPricing(ctx)
	records := make([]parsedRecord, 0)
	diag := disc.diagnostics
	for _, file := range disc.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parsed, parseDiag, err := parseTranscriptFile(ctx, file)
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "some transcript files disappeared during scan")
				continue
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "some transcript files could not be read due to permissions")
				continue
			}
			return nil, err
		}
		diag.MalformedLines += parseDiag.MalformedLines
		diag.UnsupportedEvents += parseDiag.UnsupportedEvents
		records = append(records, parsed...)
	}

	snap := normalizeRecords(s.opts.ClaudeHome, records, pricing, diag)

	s.mu.Lock()
	s.snapshot = snap
	s.loadedAt = time.Now()
	s.lastDiag = snap.diagnostics
	s.lastStatus = snap.diagnostics.Status != "unavailable"
	s.mu.Unlock()

	return snap, nil
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

func appendReason(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	return current + "; " + next
}

func (s *Source) pricingSnapshotID(ctx context.Context) string {
	pricing := s.loadPricing(ctx)
	return pricing.ID
}
