package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	claudeSourceID       = string(source.SourceClaudeCode)
	defaultSnapshotTTL   = 15 * time.Second
	defaultSourceTimeout = 10 * time.Second

	// scopedSnapshotTTL bounds how long one session's parse is kept for
	// follow-up lookups (its messages are usually opened one after another,
	// and a long session with subagents is hundreds of megabytes to re-read).
	// Staleness is not what it guards: the slot is keyed by each file's size
	// and mtime, so a session that is still being written is re-read anyway.
	scopedSnapshotTTL = 2 * time.Minute

	// boundedLoadMargin is subtracted from a bounded load's lower bound before
	// pruning files by mtime, absorbing filesystem timestamp coarseness and
	// small clock skew.
	boundedLoadMargin = 10 * time.Minute
)

// Options configures the passive Claude Code JSONL source.
type Options struct {
	ClaudeHome          string
	PathSource          string
	PricingSnapshotPath string
	PricingAliases      source.PricingAliasResolver
	PricingRates        source.PricingRateIndex
	SnapshotTTL         time.Duration
	ScanTimeout         time.Duration
}

// Source passively reads local Claude Code JSONL transcripts. It never writes to
// the Claude home and keeps only a short process-local snapshot cache.
type Source struct {
	opts Options

	mu             sync.Mutex
	snapshot       *snapshot
	loadedAt       time.Time
	lastDiag       source.SourceDiagnostics
	lastStatus     bool
	pricing        pricingSnapshot
	pricingErr     error
	pricingAliases source.PricingAliasResolver
	pricingRates   source.PricingRateIndex
	generation     uint64

	// Bounded (mtime-pruned) snapshot slot for recent-window queries. Kept
	// separate so a partial load never poisons the full-snapshot cache above.
	bounded         *snapshot
	boundedFrom     time.Time // prune threshold the bounded slot was built with
	boundedLoadedAt time.Time

	// Single-session slot for detail lookups (see sessionSnapshot). Like the
	// bounded slot it is partial, so it never stands in for the full snapshot.
	scoped         *snapshot
	scopedKey      string
	scopedLoadedAt time.Time
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
	return &Source{opts: opts, pricingAliases: opts.PricingAliases, pricingRates: opts.PricingRates}
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
	if s == nil {
		return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: "Claude Code source is not configured"}
	}
	if snap := s.freshSnapshot(); snap != nil {
		return snap.sessionByID(id), nil
	}
	// One session is stored under its own id, so read just those transcripts.
	snap, err := s.sessionSnapshot(ctx, func(pathSessionID string) bool { return pathSessionID == id })
	if err != nil {
		return nil, err
	}
	if snap != nil {
		if detail := snap.sessionByID(id); detail != nil {
			return detail, nil
		}
	}
	// Not stored under its own id (a record may name another session than its
	// file does): only the full history can answer.
	full, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return full.sessionByID(id), nil
}

func (s *Source) Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error) {
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return stats.MessageList{}, err
	}
	return snap.messages(pq, page, limit, sort)
}

func (s *Source) MessageByID(ctx context.Context, id string) (*stats.MessageDetail, error) {
	if !strings.HasPrefix(id, claudeSourceID+":") {
		return nil, nil // not an id this source issues (see synthesizeMessageID)
	}
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
	if snap := s.freshSnapshot(); snap != nil {
		return snap.messageByID(id), nil
	}
	// A message id names its session, so an older message is read from that
	// session's transcripts instead of the whole history.
	scoped, err := s.sessionSnapshot(ctx, func(pathSessionID string) bool {
		return strings.HasPrefix(id, messageIDPrefix(pathSessionID))
	})
	if err != nil {
		return nil, err
	}
	if scoped != nil {
		if detail := scoped.messageByID(id); detail != nil {
			return detail, nil
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
	view.Exists = true
	view.Format = stats.ConfigFormatJSON
	redacted, changed, err := redactJSONDocument(content)
	if err != nil {
		// No Raw on parse failure: without a structural redaction result
		// there is nothing guaranteed-safe to ship.
		view.ParseError = sanitizeConfigParseError(err)
		return view, nil
	}
	view.Content = redacted
	view.Redacted = changed
	if encoded, encodeErr := json.MarshalIndent(redacted, "", "  "); encodeErr == nil {
		view.Raw = string(encoded)
	}
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
	s.generation++
	s.snapshot = nil
	s.loadedAt = time.Time{}
	s.bounded = nil
	s.boundedFrom = time.Time{}
	s.boundedLoadedAt = time.Time{}
	s.scoped = nil
	s.scopedKey = ""
	s.scopedLoadedAt = time.Time{}
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
		// An interrupted scan says nothing about the source: keep what was known.
		if ctx.Err() != nil {
			if lastDiag.Status != "" {
				return lastDiag, lastAvailable
			}
			return disc.diagnostics, false
		}
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

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		s.mu.Lock()
		if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
			snap := s.snapshot
			s.mu.Unlock()
			return snap, nil
		}
		generation := s.generation
		s.mu.Unlock()

		disc := discoverTranscripts(ctx, s.opts.ClaudeHome)
		if !disc.available {
			s.mu.Lock()
			if s.generation != generation {
				s.mu.Unlock()
				continue
			}
			// An interrupted scan says nothing about the source itself.
			if err := ctx.Err(); err != nil {
				s.mu.Unlock()
				return nil, err
			}
			s.lastDiag = disc.diagnostics
			s.lastStatus = false
			s.mu.Unlock()
			return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: disc.diagnostics.Reason}
		}

		snap, err := s.parseFiles(ctx, disc.files, disc.diagnostics)
		if err != nil {
			s.mu.Lock()
			stale := s.generation != generation
			s.mu.Unlock()
			if stale {
				continue
			}
			return nil, err
		}

		s.mu.Lock()
		if s.generation != generation {
			s.mu.Unlock()
			continue
		}
		s.snapshot = snap
		s.loadedAt = time.Now()
		s.lastDiag = snap.diagnostics
		s.lastStatus = snap.diagnostics.Status != "unavailable"
		s.mu.Unlock()

		return snap, nil
	}
}

func (s *Source) parseFiles(ctx context.Context, files []transcriptFile, diag source.SourceDiagnostics) (*snapshot, error) {
	pricing := s.loadPricing(ctx)
	records := make([]parsedRecord, 0)
	for _, file := range files {
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
	return normalizeRecords(s.opts.ClaudeHome, records, pricing, diag), nil
}

// snapshotFor picks the snapshot for a query: a bounded (mtime-pruned) load
// when the query carries a time-precision lower bound, else the full snapshot.
func (s *Source) snapshotFor(ctx context.Context, pq stats.PeriodQuery) (*snapshot, error) {
	if pq.FromTime.IsZero() {
		return s.loadSnapshot(ctx)
	}
	return s.loadBoundedSnapshot(ctx, pq.FromTime)
}

// loadBoundedSnapshot parses only transcript files whose mtime is at/after
// from minus a safety margin. Transcripts are append-only, so a file whose
// mtime predates the threshold cannot contain a record created after from;
// included files are parsed whole, so in-window records keep full session
// context. The result lives in its own cache slot and never replaces the full
// snapshot or its diagnostics.
func (s *Source) loadBoundedSnapshot(ctx context.Context, from time.Time) (*snapshot, error) {
	if s == nil {
		return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: "Claude Code source is not configured"}
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()
	pruneT := from.UTC().Add(-boundedLoadMargin)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

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
		generation := s.generation
		s.mu.Unlock()

		disc := discoverTranscripts(ctx, s.opts.ClaudeHome)
		if !disc.available {
			s.mu.Lock()
			if s.generation != generation {
				s.mu.Unlock()
				continue
			}
			// An interrupted scan says nothing about the source itself.
			if err := ctx.Err(); err != nil {
				s.mu.Unlock()
				return nil, err
			}
			s.lastDiag = disc.diagnostics
			s.lastStatus = false
			s.mu.Unlock()
			return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: disc.diagnostics.Reason}
		}
		files := make([]transcriptFile, 0, len(disc.files))
		for _, file := range disc.files {
			if !file.ModTime.Before(pruneT) {
				files = append(files, file)
			}
		}
		snap, err := s.parseFiles(ctx, files, disc.diagnostics)
		if err != nil {
			s.mu.Lock()
			stale := s.generation != generation
			s.mu.Unlock()
			if stale {
				continue
			}
			return nil, err
		}

		s.mu.Lock()
		if s.generation != generation {
			s.mu.Unlock()
			continue
		}
		s.bounded = snap
		s.boundedFrom = pruneT
		s.boundedLoadedAt = time.Now()
		s.mu.Unlock()
		return snap, nil
	}
}

// freshSnapshot returns the full snapshot while it is within its TTL, or nil.
func (s *Source) freshSnapshot() *snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot != nil && time.Since(s.loadedAt) <= s.opts.SnapshotTTL {
		return s.snapshot
	}
	return nil
}

// sessionSnapshot parses only the transcripts whose path stores them under a
// session id accepted by belongs: a session's own file plus its subagent
// files. It returns nil when no transcript matches.
//
// Detail lookups need one session, and the full snapshot they used to load
// grows with the whole history: on a few gigabytes of transcripts it cannot
// finish inside a request, so every lookup failed. Claude Code writes a
// session to <project>/<session>.jsonl (subagents below <session>/subagents),
// which makes the matching files a complete view of that session. The result
// is partial, so it is kept in its own slot and never replaces the full
// snapshot or its diagnostics.
func (s *Source) sessionSnapshot(ctx context.Context, belongs func(pathSessionID string) bool) (*snapshot, error) {
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()

	s.mu.Lock()
	generation := s.generation
	s.mu.Unlock()

	disc := discoverTranscripts(ctx, s.opts.ClaudeHome)
	if !disc.available {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, source.UnavailableSourceError{ID: source.SourceClaudeCode, Reason: disc.diagnostics.Reason}
	}
	files := make([]transcriptFile, 0, 4)
	stamps := make([]string, 0, 4)
	for _, file := range disc.files {
		if file.SessionID != "" && belongs(file.SessionID) {
			files = append(files, file)
			// Transcripts are append-only: an unchanged size and mtime means
			// an unchanged file, so the key also says when a parse is stale.
			stamps = append(stamps, fmt.Sprintf("%s|%d|%d", file.Path, file.Size, file.ModTime.UnixNano()))
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Strings(stamps)
	key := strings.Join(stamps, "\x00")

	s.mu.Lock()
	if s.scoped != nil && s.scopedKey == key && time.Since(s.scopedLoadedAt) <= scopedSnapshotTTL {
		snap := s.scoped
		s.mu.Unlock()
		return snap, nil
	}
	s.mu.Unlock()

	snap, err := s.parseFiles(ctx, files, disc.diagnostics)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.generation == generation {
		s.scoped, s.scopedKey, s.scopedLoadedAt = snap, key, time.Now()
	}
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
