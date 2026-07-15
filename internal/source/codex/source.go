package codex

import (
	"bufio"
	"context"
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
	// A session id maps to one or more rollout files (root plus any resumes);
	// parse only those instead of the whole corpus. Fall back to the full
	// snapshot only when no file matches the id.
	if snap, matched, err := s.loadThreadSnapshot(ctx, id, true); err != nil {
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

	// Resolve by the message's own thread: parse just that thread's file(s)
	// rather than the entire corpus, which is what keeps this lookup under the
	// scan timeout on large Codex homes. A matched-but-absent id is a genuine
	// miss (nil detail -> 404), not a reason to fall back to a full parse.
	if threadID, ok := threadIDFromMessageID(id); ok {
		snap, matched, err := s.loadThreadSnapshot(ctx, threadID, false)
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
		return stats.ConfigView{}, source.UnavailableSourceError{ID: source.SourceCodex, Reason: "Codex source is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return stats.ConfigView{}, err
	}
	path, format, exists, err := detectConfigFile(s.opts.CodexHome)
	view := stats.ConfigView{SourceID: codexSourceID, Path: path, Format: format}
	if err != nil {
		return view, fmt.Errorf("access Codex config: %w", err)
	}
	if !exists {
		return view, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return view, fmt.Errorf("read Codex config: %w", err)
	}
	raw, rawChanged := redactConfigLines(format, content)
	view.Exists = true
	view.Raw = raw
	parsed, parseErr := parseConfigDocument(format, content)
	if parseErr != nil {
		view.ParseError = sanitizeParseError(parseErr)
		view.Redacted = rawChanged
		return view, nil
	}
	redactedMap, mapChanged := redactConfigMap(parsed)
	view.Content = redactedMap
	view.Redacted = rawChanged || mapChanged
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
	records, diag, err := s.parseFileRecords(ctx, files, diag)
	if err != nil {
		return nil, err
	}
	return normalizeRecords(s.opts.CodexHome, records, pricing, diag), nil
}

func (s *Source) parseFileRecords(ctx context.Context, files []transcriptFile, diag source.SourceDiagnostics) ([]codexRecord, source.SourceDiagnostics, error) {
	records := make([]codexRecord, 0)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, diag, err
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
			return nil, diag, err
		}
		diag.MalformedLines += parseDiag.MalformedLines
		diag.UnsupportedEvents += parseDiag.UnsupportedEvents
		records = append(records, parsed...)
	}
	return records, diag, nil
}

// wantedParentThreadIDs collects the thread/session ids that derived
// (forked/resumed) rollout files replay history from. Their transcripts must be
// parsed alongside the derived files so the replayed token ladder is
// recognizable as already-counted usage.
func wantedParentThreadIDs(records []codexRecord) map[string]bool {
	wanted := make(map[string]bool)
	for _, record := range records {
		meta := record.SessionMeta
		if meta == nil {
			continue
		}
		if meta.ForkedFromID != "" {
			wanted[meta.ForkedFromID] = true
		}
		if meta.SessionID != "" && meta.ID != "" && meta.SessionID != meta.ID {
			wanted[meta.SessionID] = true
		}
	}
	return wanted
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
	included := make(map[string]bool, len(disc.files))
	for _, file := range disc.files {
		if !file.ModTime.Before(pruneT) {
			files = append(files, file)
			included[file.Path] = true
		}
	}
	records, diag, err := s.parseFileRecords(ctx, files, disc.diagnostics)
	if err != nil {
		return nil, err
	}
	// Forked/resumed threads replay their parent thread's whole token ladder
	// with fresh timestamps. If the (possibly long-inactive, mtime-pruned)
	// parent transcript is missing from the parse set, that replay would be
	// indistinguishable from new usage inside the window. Pull parent files
	// back in by their filename thread id, transitively for chained forks.
	byFilenameID := make(map[string][]transcriptFile, len(disc.files))
	for _, file := range disc.files {
		if file.SessionID != "" {
			byFilenameID[file.SessionID] = append(byFilenameID[file.SessionID], file)
		}
	}
	for {
		added := make([]transcriptFile, 0)
		for id := range wantedParentThreadIDs(records) {
			for _, file := range byFilenameID[id] {
				if !included[file.Path] {
					included[file.Path] = true
					added = append(added, file)
				}
			}
		}
		if len(added) == 0 {
			break
		}
		var parentRecords []codexRecord
		parentRecords, diag, err = s.parseFileRecords(ctx, added, diag)
		if err != nil {
			return nil, err
		}
		records = append(records, parentRecords...)
	}
	snap := normalizeRecords(s.opts.CodexHome, records, s.loadPricing(ctx), diag)

	s.mu.Lock()
	s.bounded = snap
	s.boundedFrom = pruneT
	s.boundedLoadedAt = time.Now()
	s.mu.Unlock()
	return snap, nil
}

// fileThreadMeta is the lightweight identity of one rollout file, recovered from
// its leading session_meta record without parsing the whole transcript.
type fileThreadMeta struct {
	file         transcriptFile
	threadID     string // session_meta.id — the file's own thread id
	sessionID    string // logical session id (session_id, falling back to id)
	forkedFromID string
	parentID     string // parent_thread_id
}

// readThreadMeta reads only the first session_meta record of a rollout file.
// It is always at (or very near) the head of the file, so this is a cheap read
// even for multi-hundred-MB transcripts. Returns false if no usable meta is
// found before the scan cap or the file cannot be read.
func readThreadMeta(ctx context.Context, file transcriptFile) (fileThreadMeta, bool) {
	fh, err := os.Open(file.Path)
	if err != nil {
		return fileThreadMeta{}, false
	}
	defer fh.Close()

	reader := bufio.NewReaderSize(fh, 128*1024)
	// session_meta is the first record in practice; cap the scan so a malformed
	// or unexpected file never turns this into a full-file read.
	for i := 0; i < 128; i++ {
		if err := ctx.Err(); err != nil {
			return fileThreadMeta{}, false
		}
		line, readErr := reader.ReadString('\n')
		if line != "" && strings.Contains(line, "session_meta") {
			record, ok, _ := parseLine(file, i+1, line)
			if ok && record.SessionMeta != nil && record.SessionMeta.ID != "" {
				meta := record.SessionMeta
				sessionID := meta.SessionID
				if sessionID == "" {
					sessionID = meta.ID
				}
				return fileThreadMeta{
					file:         file,
					threadID:     meta.ID,
					sessionID:    sessionID,
					forkedFromID: meta.ForkedFromID,
					parentID:     meta.ParentThreadID,
				}, true
			}
		}
		if readErr != nil {
			break
		}
	}
	return fileThreadMeta{}, false
}

// scanThreadMetas builds the lightweight thread index by head-reading every
// discovered file.
func scanThreadMetas(ctx context.Context, files []transcriptFile) []fileThreadMeta {
	metas := make([]fileThreadMeta, 0, len(files))
	for _, file := range files {
		if ctx.Err() != nil {
			return metas
		}
		if meta, ok := readThreadMeta(ctx, file); ok {
			metas = append(metas, meta)
		}
	}
	return metas
}

// selectThreadFiles resolves the minimal set of transcript files needed to
// answer a by-thread (message) or by-session lookup: the seed file(s) plus all
// transitive ancestors (fork parents / resume roots) so replayed token ladders
// and user prompts are recognized exactly as they would be in a full snapshot.
// It never pulls in unrelated files, so the parse stays O(one conversation).
func selectThreadFiles(metas []fileThreadMeta, allFiles []transcriptFile, id string, bySession bool) []transcriptFile {
	byThread := make(map[string][]fileThreadMeta, len(metas))
	bySessionID := make(map[string][]fileThreadMeta, len(metas))
	for _, meta := range metas {
		byThread[meta.threadID] = append(byThread[meta.threadID], meta)
		bySessionID[meta.sessionID] = append(bySessionID[meta.sessionID], meta)
	}

	selected := make(map[string]transcriptFile)
	queue := make([]string, 0)
	addFile := func(meta fileThreadMeta) {
		if _, ok := selected[meta.file.Path]; ok {
			return
		}
		selected[meta.file.Path] = meta.file
		if meta.forkedFromID != "" {
			queue = append(queue, meta.forkedFromID)
		}
		if meta.parentID != "" {
			queue = append(queue, meta.parentID)
		}
		if meta.sessionID != "" && meta.sessionID != meta.threadID {
			queue = append(queue, meta.sessionID)
		}
	}

	// Seed files: the thread that owns the message, or every thread (root +
	// resumes) that shares the requested session id.
	for _, meta := range byThread[id] {
		addFile(meta)
	}
	if bySession {
		for _, meta := range bySessionID[id] {
			addFile(meta)
		}
	}

	// Fallback: if the head-scan could not identify the file (unreadable meta),
	// match the id as a filename substring. rolloutSessionID does not reliably
	// yield the bare thread UUID for every filename format, but the UUID is
	// present in the filename, so a substring match still finds it.
	if len(selected) == 0 {
		for _, file := range allFiles {
			if strings.Contains(filepath.Base(file.Path), id) {
				selected[file.Path] = file
			}
		}
	}

	// Transitively pull in ancestors. Terminates: each file is added at most
	// once, and only a newly added file enqueues more parent ids.
	for len(queue) > 0 {
		parentID := queue[0]
		queue = queue[1:]
		for _, meta := range byThread[parentID] {
			addFile(meta)
		}
		for _, meta := range bySessionID[parentID] {
			addFile(meta)
		}
	}

	files := make([]transcriptFile, 0, len(selected))
	for _, file := range selected {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// loadThreadSnapshot parses only the transcript files belonging to one thread
// (bySession=false, for a message lookup) or one logical session including its
// resumes (bySession=true, for a session lookup), plus their ancestors. The
// returned bool reports whether any matching file was found; callers fall back
// to the full snapshot only when it is false (an id whose file is genuinely
// unknown). This keeps single-message/single-session detail lookups bounded to
// one conversation instead of the whole corpus, which is what makes them
// survive the scan timeout on large Codex homes.
func (s *Source) loadThreadSnapshot(ctx context.Context, id string, bySession bool) (*snapshot, bool, error) {
	if s == nil {
		return nil, false, source.UnavailableSourceError{ID: source.SourceCodex, Reason: "Codex source is not configured"}
	}
	if id == "" {
		return nil, false, nil
	}
	ctx, cancel := s.contextWithTimeout(ctx)
	defer cancel()

	disc := discoverTranscripts(ctx, s.opts.CodexHome)
	if !disc.available {
		s.setLastDiagnostics(disc.diagnostics, false)
		return nil, false, source.UnavailableSourceError{ID: source.SourceCodex, Reason: disc.diagnostics.Reason}
	}

	metas := scanThreadMetas(ctx, disc.files)
	files := selectThreadFiles(metas, disc.files, id, bySession)
	if len(files) == 0 {
		return nil, false, nil
	}
	records, diag, err := s.parseFileRecords(ctx, files, disc.diagnostics)
	if err != nil {
		return nil, false, err
	}
	return normalizeRecords(s.opts.CodexHome, records, s.loadPricing(ctx), diag), true, nil
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
