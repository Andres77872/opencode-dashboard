package qwencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	qwenSourceID         = string(source.SourceQwenCode)
	defaultSnapshotTTL   = 15 * time.Second
	defaultSourceTimeout = 10 * time.Second
	boundedLoadMargin    = 10 * time.Minute
	// usageMonthSlack absorbs the writer-local-timezone month boundaries of
	// token-usage-YYYY-MM.jsonl file names when pruning by window.
	usageMonthSlack = 48 * time.Hour
)

type Options struct {
	QwenHome            string
	PathSource          string
	PricingSnapshotPath string
	SnapshotTTL         time.Duration
	ScanTimeout         time.Duration
}

// Source passively reads Qwen Code's local chat transcripts and usage logs.
// It never writes to the Qwen home directory.
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
	if opts.QwenHome == "" {
		opts.QwenHome = defaultQwenHome()
	}
	if opts.PathSource == "" {
		opts.PathSource = "$HOME/.qwen"
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = defaultSnapshotTTL
	}
	if opts.ScanTimeout <= 0 {
		opts.ScanTimeout = defaultSourceTimeout
	}
	return &Source{opts: opts}
}

func defaultQwenHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".qwen")
	}
	return filepath.Join(home, ".qwen")
}

func (s *Source) Info(ctx context.Context) source.SourceInfo {
	if s == nil {
		return unavailableInfo("", "", "Qwen Code source is not configured")
	}
	diag, available := s.currentDiagnostics(ctx)
	return source.SourceInfo{
		ID:           source.SourceQwenCode,
		Label:        "Qwen Code",
		Kind:         "jsonl",
		Available:    available,
		Path:         s.opts.QwenHome,
		PathSource:   s.opts.PathSource,
		ReadOnly:     true,
		LocalOnly:    true,
		Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"},
		Warnings: []string{
			"Qwen Code chat transcripts are plaintext local files and may contain sensitive content",
			"Qwen Code support is passive, local-only, and read-only",
		},
		Diagnostics: diag,
		CostPolicy: source.CostPolicy{
			Status:            string(stats.CostEstimatedAPIEquivalent),
			Currency:          "USD",
			PricingSnapshotID: s.pricingSnapshotID(ctx),
			Note:              "Qwen Code costs are estimated API-equivalent values, not actual coding-plan or Token Plan spend; models without public list prices report cost as missing",
		},
		Privacy: source.PrivacyInfo{
			PlaintextTranscripts: true,
			ReadOnly:             true,
			LocalOnly:            true,
			Redaction:            true,
			Warnings: []string{
				"Local Qwen Code chat transcripts can contain prompts, reasoning, tool input/output, paths, patches, and secrets",
			},
		},
	}
}

func unavailableInfo(path, pathSource, reason string) source.SourceInfo {
	return source.SourceInfo{
		ID:          source.SourceQwenCode,
		Label:       "Qwen Code",
		Kind:        "jsonl",
		Available:   false,
		Path:        path,
		PathSource:  pathSource,
		ReadOnly:    true,
		LocalOnly:   true,
		Warnings:    []string{"Qwen Code chat transcripts are plaintext local files and may contain sensitive content"},
		Diagnostics: source.SourceDiagnostics{Status: "unavailable", Reason: reason},
		CostPolicy:  source.CostPolicy{Status: string(stats.CostMissing), Currency: "USD", Note: "Qwen Code source is unavailable"},
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

func (s *Source) DailyDimension(ctx context.Context, dimension string, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyDimensionStats, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	return snap.dailyDimension(dimension, pq, granularity...)
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
		return stats.ConfigView{}, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: "Qwen Code source is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return stats.ConfigView{}, err
	}
	path := filepath.Join(s.opts.QwenHome, "settings.json")
	view := stats.ConfigView{SourceID: qwenSourceID, Path: path, Format: stats.ConfigFormatJSON}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return view, nil
		}
		return view, fmt.Errorf("access Qwen Code settings: %w", err)
	}
	if info.IsDir() {
		return view, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return view, fmt.Errorf("read Qwen Code settings: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		view.Exists = true
		view.ParseError = sanitizeParseError(err)
		return view, nil
	}
	redacted, changed := redactConfigMap(parsed)
	view.Exists = true
	view.Content = redacted
	view.Redacted = changed
	if raw, err := encodeRedactedJSON(redacted); err == nil {
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

	disc := discoverData(ctx, s.opts.QwenHome)
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
		return nil, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: "Qwen Code source is not configured"}
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

	disc := discoverData(ctx, s.opts.QwenHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: disc.diagnostics.Reason}
	}
	snap, err := s.parseData(ctx, disc.chats, disc.usageFiles, disc.usageRecordPath, disc.diagnostics)
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
		return nil, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: "Qwen Code source is not configured"}
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

	disc := discoverData(ctx, s.opts.QwenHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: disc.diagnostics.Reason}
	}
	chats := make([]chatFile, 0, len(disc.chats))
	for _, item := range disc.chats {
		if !item.ModTime.Before(pruneT) {
			chats = append(chats, item)
		}
	}
	usageFiles := make([]usageFile, 0, len(disc.usageFiles))
	for _, item := range disc.usageFiles {
		if usageFileInWindow(item, pruneT) {
			usageFiles = append(usageFiles, item)
		}
	}
	snap, err := s.parseData(ctx, chats, usageFiles, disc.usageRecordPath, disc.diagnostics)
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

// usageFileInWindow keeps a monthly usage log when any instant of its month
// (with timezone slack) can fall inside the load window.
func usageFileInWindow(file usageFile, pruneT time.Time) bool {
	if file.Month == "" {
		return true
	}
	monthStart, err := time.Parse("2006-01", file.Month)
	if err != nil {
		return true
	}
	monthEnd := monthStart.AddDate(0, 1, 0).Add(usageMonthSlack)
	return monthEnd.After(pruneT)
}

func (s *Source) loadSessionSnapshot(ctx context.Context, id string) (*snapshot, bool, error) {
	if s == nil {
		return nil, false, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: "Qwen Code source is not configured"}
	}
	if id == "" {
		return nil, false, nil
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()
	disc := discoverData(ctx, s.opts.QwenHome)
	if !disc.available {
		return nil, false, source.UnavailableSourceError{ID: source.SourceQwenCode, Reason: disc.diagnostics.Reason}
	}
	for _, item := range disc.chats {
		if item.SessionID != id {
			continue
		}
		snap, err := s.parseData(ctx, []chatFile{item}, disc.usageFiles, disc.usageRecordPath, disc.diagnostics)
		return snap, true, err
	}
	return nil, false, nil
}

func (s *Source) parseData(ctx context.Context, chats []chatFile, usageFiles []usageFile, usageRecordPath string, diag source.SourceDiagnostics) (*snapshot, error) {
	parsed := make([]parsedChatSession, 0, len(chats))
	for _, item := range chats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chat, parseDiag, err := parseChatFile(ctx, item)
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "some Qwen Code chat transcripts disappeared during scan")
				continue
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "some Qwen Code chat transcripts could not be read due to permissions")
				continue
			}
			return nil, err
		}
		diag.MalformedLines += parseDiag.MalformedLines
		diag.UnsupportedEvents += parseDiag.UnsupportedEvents
		parsed = append(parsed, chat)
	}

	usageLog := make([]usageLogRecord, 0)
	for _, item := range usageFiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		records, parseDiag, err := parseUsageLogFile(ctx, item.Path)
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "some Qwen Code usage logs could not be read")
				continue
			}
			return nil, err
		}
		diag.MalformedLines += parseDiag.MalformedLines
		usageLog = append(usageLog, records...)
	}

	rollups, rollupDiag, err := parseSessionRollups(ctx, usageRecordPath)
	if err != nil {
		if os.IsPermission(err) {
			diag.Reason = appendReason(diag.Reason, "the Qwen Code usage record file could not be read")
			rollups = map[string]sessionRollup{}
		} else {
			return nil, err
		}
	}
	diag.MalformedLines += rollupDiag.MalformedLines

	return normalizeData(s.opts.QwenHome, parsed, usageLog, rollups, s.loadPricing(ctx), finalizeDiagnostics(diag)), nil
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
			diag.Reason = "no Qwen Code chat transcripts or usage logs found"
		}
		return diag
	}
	if diag.MalformedLines > 0 || diag.UnsupportedEvents > 0 || diag.Reason != "" {
		diag.Status = "partial"
		if diag.Reason == "" {
			diag.Reason = "some Qwen Code JSONL records were skipped"
		}
		return diag
	}
	diag.Status = "ok"
	return diag
}
