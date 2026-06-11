package codex

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
	codexSourceID        = string(source.SourceCodex)
	defaultSnapshotTTL   = 2 * time.Second
	defaultSourceTimeout = 10 * time.Second

	// boundedLoadMargin is subtracted from a bounded load's lower bound before
	// pruning files by mtime, absorbing filesystem timestamp coarseness and
	// small clock skew.
	boundedLoadMargin = 10 * time.Minute
)

type Options struct {
	CodexHome           string
	PathSource          string
	PricingSnapshotPath string
	SnapshotTTL         time.Duration
	ScanTimeout         time.Duration
}

type Source struct {
	opts Options

	mu         sync.Mutex
	snapshot   *snapshot
	loadedAt   time.Time
	lastDiag   source.SourceDiagnostics
	lastStatus bool
	pricing    pricingSnapshot
	pricingErr error

	// Bounded (mtime-pruned) snapshot slot for recent-window queries. Kept
	// separate so a partial load never poisons the full-snapshot cache above.
	bounded         *snapshot
	boundedFrom     time.Time // prune threshold the bounded slot was built with
	boundedLoadedAt time.Time
}

func New(opts Options) *Source {
	if opts.CodexHome == "" {
		opts.CodexHome = defaultCodexHome()
	}
	if opts.PathSource == "" {
		opts.PathSource = "$HOME/.codex"
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = defaultSnapshotTTL
	}
	if opts.ScanTimeout <= 0 {
		opts.ScanTimeout = defaultSourceTimeout
	}
	return &Source{opts: opts}
}

func defaultCodexHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".codex")
	}
	return filepath.Join(home, ".codex")
}

func (s *Source) Info(ctx context.Context) source.SourceInfo {
	if s == nil {
		return unavailableInfo("", "", "Codex source is not configured")
	}
	diag, available := s.currentDiagnostics(ctx)
	return source.SourceInfo{
		ID:           source.SourceCodex,
		Label:        "Codex",
		Kind:         "jsonl",
		Available:    available,
		Path:         s.opts.CodexHome,
		PathSource:   s.opts.PathSource,
		ReadOnly:     true,
		LocalOnly:    true,
		Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"},
		Warnings: []string{
			"Codex transcripts are plaintext local files and may contain sensitive content",
			"Codex support is passive, local-only, and read-only",
		},
		Diagnostics: diag,
		CostPolicy: source.CostPolicy{
			Status:            string(stats.CostEstimatedAPIEquivalent),
			Currency:          "USD",
			PricingSnapshotID: s.pricingSnapshotID(ctx),
			Note:              "Codex costs are estimated API-equivalent values, not actual subscription spend",
		},
		Privacy: source.PrivacyInfo{
			PlaintextTranscripts: true,
			ReadOnly:             true,
			LocalOnly:            true,
			Redaction:            true,
			Warnings: []string{
				"Local Codex transcripts can contain prompts, tool output, paths, patches, and secrets",
			},
		},
	}
}

func unavailableInfo(path, pathSource, reason string) source.SourceInfo {
	return source.SourceInfo{
		ID:          source.SourceCodex,
		Label:       "Codex",
		Kind:        "jsonl",
		Available:   false,
		Path:        path,
		PathSource:  pathSource,
		ReadOnly:    true,
		LocalOnly:   true,
		Warnings:    []string{"Codex transcripts are plaintext local files and may contain sensitive content"},
		Diagnostics: source.SourceDiagnostics{Status: "unavailable", Reason: reason},
		CostPolicy:  source.CostPolicy{Status: string(stats.CostMissing), Currency: "USD", Note: "Codex source is unavailable"},
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
	// A fresh bounded snapshot usually already holds recent messages (the
	// consolidation collect lists with FromTime and then fetches details);
	// fall back to the full snapshot only on a miss.
	s.mu.Lock()
	if s.bounded != nil && time.Since(s.boundedLoadedAt) <= s.opts.SnapshotTTL {
		if detail := s.bounded.messageByID(id); detail != nil {
			s.mu.Unlock()
			return detail, nil
		}
	}
	s.mu.Unlock()
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.messageByID(id), nil
}

func (s *Source) Config(ctx context.Context) (stats.ConfigView, error) {
	if s == nil {
		return stats.ConfigView{}, source.UnavailableSourceError{ID: source.SourceCodex, Reason: "Codex source is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return stats.ConfigView{}, err
	}
	path := filepath.Join(s.opts.CodexHome, "config.toml")
	view := stats.ConfigView{SourceID: codexSourceID, Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return view, nil
		}
		return view, fmt.Errorf("access Codex config: %w", err)
	}
	if info.IsDir() {
		return view, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return view, fmt.Errorf("read Codex config: %w", err)
	}
	redacted, changed := redactConfigTOML(content)
	view.Exists = true
	view.Content = redacted
	view.Redacted = changed
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

	disc := discoverTranscripts(ctx, s.opts.CodexHome)
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
		return nil, source.UnavailableSourceError{ID: source.SourceCodex, Reason: "Codex source is not configured"}
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

	disc := discoverTranscripts(ctx, s.opts.CodexHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceCodex, Reason: disc.diagnostics.Reason}
	}
	snap, err := s.parseFiles(ctx, disc.files, disc.diagnostics)
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

func (s *Source) parseFiles(ctx context.Context, files []transcriptFile, diag source.SourceDiagnostics) (*snapshot, error) {
	pricing := s.loadPricing(ctx)
	records := make([]codexRecord, 0)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parsed, parseDiag, err := parseTranscriptFile(ctx, file)
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "some Codex transcript files disappeared during scan")
				continue
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "some Codex transcript files could not be read due to permissions")
				continue
			}
			return nil, err
		}
		diag.MalformedLines += parseDiag.MalformedLines
		diag.UnsupportedEvents += parseDiag.UnsupportedEvents
		records = append(records, parsed...)
	}
	return normalizeRecords(s.opts.CodexHome, records, pricing, diag), nil
}

// snapshotFor picks the snapshot for a query: a bounded (mtime-pruned) load
// when the query carries a time-precision lower bound, else the full snapshot.
func (s *Source) snapshotFor(ctx context.Context, pq stats.PeriodQuery) (*snapshot, error) {
	if pq.FromTime.IsZero() {
		return s.loadSnapshot(ctx)
	}
	return s.loadBoundedSnapshot(ctx, pq.FromTime)
}

// loadBoundedSnapshot parses only rollout files whose mtime is at/after from
// minus a safety margin. Rollouts are append-only, so a file whose mtime
// predates the threshold cannot contain a record created after from; included
// files are parsed whole, so in-window records keep full session context. The
// result lives in its own cache slot and never replaces the full snapshot or
// its diagnostics.
func (s *Source) loadBoundedSnapshot(ctx context.Context, from time.Time) (*snapshot, error) {
	if s == nil {
		return nil, source.UnavailableSourceError{ID: source.SourceCodex, Reason: "Codex source is not configured"}
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()
	pruneT := from.UTC().Add(-boundedLoadMargin)

	s.mu.Lock()
	if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
		snap := s.snapshot
		s.mu.Unlock()
		return snap, nil // a fresh full snapshot is a superset
	}
	if s.bounded != nil && time.Since(s.boundedLoadedAt) <= s.opts.SnapshotTTL && !s.boundedFrom.After(pruneT) {
		snap := s.bounded
		s.mu.Unlock()
		return snap, nil // cached bounded superset
	}
	s.mu.Unlock()

	disc := discoverTranscripts(ctx, s.opts.CodexHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceCodex, Reason: disc.diagnostics.Reason}
	}
	files := make([]transcriptFile, 0, len(disc.files))
	for _, file := range disc.files {
		if !file.ModTime.Before(pruneT) {
			files = append(files, file)
		}
	}
	snap, err := s.parseFiles(ctx, files, disc.diagnostics)
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
	pricing := s.loadPricing(ctx)
	return pricing.ID
}
