// Package cache owns the dashboard SQLite cache.
//
// The cache is intentionally separate from every source database. It stores
// normalized metadata and aggregates only; raw transcript rows, message text,
// reasoning text, tool input, and tool output are not persisted here.
package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	// dataVersion is the version of the cache's data semantics: how sources are
	// normalized into rows, token accounting, message id synthesis. It
	// participates in source fingerprints, and Open resets the consolidation
	// state of any source cached under an older value so the next sync fully
	// re-collects (see resetOutdatedDataVersions). Schema (DDL) changes never
	// touch this constant — they bump schemaVersion in schema.go, which removes
	// and rebuilds the whole cache database.
	//
	// v4: finalized-only caches. v5: per-thread Codex token accounting with
	// fork/resume replay suppression (thread-scoped message ids). v6: Codex
	// requested service-tier and normalized processing-mode attribution. v7:
	// Codex API-equivalent USD estimates use the requested processing tier's
	// official Standard, Priority, or Flex per-token catalog. v8: OpenCode
	// model analytics use additive step-finish usage without changing Overview.
	dataVersion            = 8
	busyTimeout            = 5000 * time.Millisecond
	DefaultSyncSafetyDelay = 6 * time.Hour
	// collectCallTimeout bounds one bulk snapshot or one generic pagination
	// page (including that page's detail lookups), not a whole-job budget.
	// Initial JSONL scans can legitimately exceed their short interactive
	// timeout, while large database sources can take many pages and run for
	// substantially longer than this in aggregate.
	collectCallTimeout = 5 * time.Minute
	// syncPageSize sizes bulk page reads from live sources: the generic sync
	// fallback and the gap-merge live fetches. Sources accept up to
	// stats.MaxPageSize per page; larger pages mean fewer repeated
	// count/sort/scan passes per collection. Web-facing pagination keeps its
	// own tighter clamps and is unaffected.
	syncPageSize = 1000
)

var syncSort = stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc}

type SyncMode string

const (
	SyncModeIncremental SyncMode = "incremental"
	SyncModeRebuild     SyncMode = "rebuild"

	pricingSnapshotChangeReason = "pricing catalog changed; full historical repricing required"
)

type SyncOptions struct {
	Mode SyncMode
	// Cutoff is the hour-aligned finality boundary: only rows strictly older
	// than it are consolidated into the cache, and data before it is never
	// re-collected by later incremental syncs. Zero defaults to
	// DefaultSafeCutoff(now). The recorded value never regresses below the
	// source's current cutoff. Raw data mutating older than an already
	// recorded cutoff is only repaired by a manual rebuild.
	Cutoff time.Time
	// ReadTriggered marks an automatic consolidation from the read path: it
	// must never block behind another sync, wipe cached rows on transient
	// unavailability, or record a failed state.
	ReadTriggered bool
	// Progress, when set, is invoked at page granularity while collecting and
	// once before writing, so long consolidations can report live progress.
	Progress func(SyncProgress)
}

// SyncProgress describes how far a sync has advanced through one phase.
type SyncProgress struct {
	SourceID string
	Phase    string // "sessions" | "messages" | "write"
	Done     int64
	Total    int64
}

// FillState is the in-memory outcome of the latest read-triggered fill attempt
// for a source. It is not persisted; it exists so the status API can surface
// auto-refresh failures that would otherwise be invisible.
type FillState struct {
	AttemptMS int64
	ErrMsg    string
}

type SyncReport struct {
	SourceID      string
	Mode          SyncMode
	Since         time.Time
	Cutoff        time.Time
	FreshThrough  time.Time
	Messages      int
	Tools         int
	SkippedRecent int
	SkippedOld    int
	Changed       bool
}

type Store struct {
	db     *sql.DB
	path   string
	once   sync.Once
	logger *slog.Logger

	syncMu    sync.Mutex
	syncLocks map[string]*sync.Mutex

	fillMu     sync.Mutex
	fillStates map[string]FillState
	gapStates  map[string]FillState

	// memoMu guards stateMemo and memoGen (leaf lock; see merge_store.go).
	memoMu    sync.Mutex
	stateMemo map[string]sourceStateMemo
	memoGen   map[string]uint64

	// writeSem serializes cache write transactions across sources: SQLite
	// permits one writer, so in-process writers queue here instead of
	// colliding on SQLITE_BUSY (a rebuild's write can outlast any busy
	// timeout). Capacity-1 channel rather than a mutex so a queued writer
	// stays cancelable via context. Lock ordering: sourceLock -> writeSem,
	// never the reverse; syncMu and fillMu remain leaf locks.
	writeSem chan struct{}
}

// beginWrite acquires the global write slot, honoring ctx cancellation so a
// queued fill aborts promptly during shutdown instead of waiting out a long
// rebuild write transaction.
func (s *Store) beginWrite(ctx context.Context) error {
	select {
	case s.writeSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) endWrite() {
	<-s.writeSem
}

// commitState commits a source_state-writing transaction and drops the
// source's memoized watermark row so the next read re-seeds it.
func (s *Store) commitState(tx *sql.Tx, sourceID string) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateStateMemo(sourceID)
	return nil
}

// SetLogger directs sync/fill activity logs to l. A nil logger discards.
func (s *Store) SetLogger(l *slog.Logger) {
	if s == nil {
		return
	}
	if l == nil {
		l = slog.New(slog.DiscardHandler)
	}
	s.logger = l
}

func (s *Store) FillState(sourceID string) (FillState, bool) {
	if s == nil {
		return FillState{}, false
	}
	s.fillMu.Lock()
	defer s.fillMu.Unlock()
	state, ok := s.fillStates[sourceID]
	return state, ok
}

func (s *Store) recordFillState(sourceID string, err error) {
	if s == nil || sourceID == "" {
		return
	}
	state := FillState{AttemptMS: time.Now().UTC().UnixMilli()}
	if err != nil {
		state.ErrMsg = err.Error()
	}
	s.fillMu.Lock()
	defer s.fillMu.Unlock()
	if s.fillStates == nil {
		s.fillStates = make(map[string]FillState)
	}
	s.fillStates[sourceID] = state
}

// GapState is the outcome of the latest live read of the recent window for a
// source. A non-empty ErrMsg means the last request degraded to cache-only
// data (recent hours shown as zero); the web layer renders it as a warning.
func (s *Store) GapState(sourceID string) (FillState, bool) {
	if s == nil {
		return FillState{}, false
	}
	s.fillMu.Lock()
	defer s.fillMu.Unlock()
	state, ok := s.gapStates[sourceID]
	return state, ok
}

func (s *Store) recordGapState(sourceID string, err error) {
	if s == nil || sourceID == "" {
		return
	}
	state := FillState{AttemptMS: time.Now().UTC().UnixMilli()}
	if err != nil {
		state.ErrMsg = err.Error()
	}
	s.fillMu.Lock()
	defer s.fillMu.Unlock()
	if s.gapStates == nil {
		s.gapStates = make(map[string]FillState)
	}
	s.gapStates[sourceID] = state
}

type SourceStatus struct {
	SourceID       string `json:"source_id"`
	Fingerprint    string `json:"fingerprint"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	LastSynced     int64  `json:"last_synced_ms,omitempty"`
	LastSafeCutoff int64  `json:"last_safe_cutoff_ms,omitempty"`
	FreshThrough   int64  `json:"fresh_through_ms,omitempty"`
}

type SyncNeed struct {
	Needed bool
	Reason string
	Status SourceStatus
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("cache database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	db, err := openDB(ctx, path)
	if err != nil {
		return nil, err
	}
	// A schema-version mismatch removes and rebuilds the database here; the
	// returned handle may therefore be a different connection than db.
	db, err = ensureSchemaVersion(ctx, db, path)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db, path: path, logger: slog.New(slog.DiscardHandler), writeSem: make(chan struct{}, 1)}
	if err := store.resetOutdatedDataVersions(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// openDB opens and probes one cache database connection pool.
func openDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", buildDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect cache database: %w", err)
	}
	return db, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.once.Do(func() {
		if s.db != nil {
			err = s.db.Close()
		}
	})
	return err
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// buildDSN uses modernc.org/sqlite parameter syntax: each _pragma is applied
// to every new pooled connection (the driver orders busy_timeout first), so
// per-connection settings survive pool growth. The parentheses must not be
// percent-encoded and the path must not be a file: URI (the driver would
// URI-parse the filesystem path).
func buildDSN(path string) string {
	params := []string{
		"_txlock=immediate",
		fmt.Sprintf("_pragma=busy_timeout(%d)", busyTimeout.Milliseconds()),
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(1)",
		"_pragma=temp_store(MEMORY)",
		"_pragma=cache_size(-16384)",
		"_pragma=mmap_size(268435456)",
	}
	return path + "?" + strings.Join(params, "&")
}

func (s *Store) SourceStatus(ctx context.Context, sourceID string) (SourceStatus, bool, error) {
	var status SourceStatus
	err := s.db.QueryRowContext(ctx, `
		SELECT source_id, fingerprint, status, COALESCE(reason, ''), last_synced_ms, last_safe_cutoff_ms, fresh_through_ms
		FROM source_state
		WHERE source_id = ?
	`, sourceID).Scan(&status.SourceID, &status.Fingerprint, &status.Status, &status.Reason, &status.LastSynced, &status.LastSafeCutoff, &status.FreshThrough)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SourceStatus{}, false, nil
		}
		return SourceStatus{}, false, err
	}
	return status, true, nil
}

func (s *Store) NeedsSync(ctx context.Context, src source.Source) (SyncNeed, error) {
	if s == nil || src == nil {
		return SyncNeed{}, nil
	}
	info := src.Info(ctx)
	if info.ID == "" || !info.Available {
		return SyncNeed{}, nil
	}
	fp, err := sourceFingerprint(ctx, info)
	if err != nil {
		fp = fallbackFingerprint(info)
	}
	current, ok, err := s.SourceStatus(ctx, string(info.ID))
	if err != nil {
		return SyncNeed{}, fmt.Errorf("read cache source state: %w", err)
	}
	if !ok {
		return SyncNeed{Needed: true, Reason: "cache has no consolidated data for this source"}, nil
	}
	if pricingIdentityChanged(current.Fingerprint, fp, info.CostPolicy.PricingSnapshotID) {
		return SyncNeed{Needed: true, Reason: pricingSnapshotChangeReason, Status: current}, nil
	}
	if current.Status != "ready" {
		reason := "cache is not ready"
		if current.Reason != "" {
			reason = current.Reason
		}
		return SyncNeed{Needed: true, Reason: reason, Status: current}, nil
	}
	if current.Fingerprint != fp {
		return SyncNeed{Needed: true, Reason: "source data changed since the last consolidation", Status: current}, nil
	}
	return SyncNeed{Status: current}, nil
}

func (s *Store) SyncSource(ctx context.Context, src source.Source) error {
	_, err := s.SyncSourceWithOptions(ctx, src, SyncOptions{Mode: SyncModeRebuild})
	return err
}

func (s *Store) SyncSourceWithOptions(ctx context.Context, src source.Source, opts SyncOptions) (report SyncReport, retErr error) {
	opts = normalizeSyncOptions(opts)
	report = SyncReport{Mode: opts.Mode, Cutoff: opts.Cutoff}
	if s == nil || src == nil {
		return report, nil
	}
	info := src.Info(ctx)
	report.SourceID = string(info.ID)
	if info.ID == "" {
		return report, nil
	}

	lock := s.sourceLock(string(info.ID))
	if opts.ReadTriggered {
		if !lock.TryLock() {
			// Another sync is running; serve the current cache as-is. The skip
			// is deliberately invisible in the fill states: it is not an attempt
			// (recording success would fake freshness, recording an error would
			// flag a failure while a healthy sync runs) — the running job is the
			// visible artifact, and the 1-minute fill backoff retries shortly.
			s.logger.Debug("cache fill skipped: sync already running", "source", info.ID)
			return report, nil
		}
	} else {
		lock.Lock()
	}
	defer lock.Unlock()
	if opts.ReadTriggered {
		defer func() { s.recordFillState(string(info.ID), retErr) }()
	}

	fp, fpErr := sourceFingerprint(ctx, info)
	fpReliable := fpErr == nil
	if fpErr != nil {
		fp = fallbackFingerprint(info)
	}
	current, ok, err := s.SourceStatus(ctx, string(info.ID))
	if err != nil {
		retErr = fmt.Errorf("read cache source state: %w", err)
		return report, retErr
	}
	if !info.Available {
		if opts.ReadTriggered {
			// Return an error so the deferred recordFillState surfaces the
			// condition ("auto-refresh failed") instead of faking a healthy fill.
			// Nothing persisted changes; the read itself is unaffected.
			s.logger.Debug("cache fill skipped: source unavailable", "source", info.ID, "reason", info.Diagnostics.Reason)
			retErr = fmt.Errorf("source unavailable: %s", info.Diagnostics.Reason)
			return report, retErr
		}
		s.logger.Warn("cache sync: source unavailable, keeping cached rows", "source", info.ID, "reason", info.Diagnostics.Reason)
		return report, s.markUnavailable(ctx, info, fp, current, ok)
	}

	pricingChanged := ok && pricingIdentityChanged(current.Fingerprint, fp, info.CostPolicy.PricingSnapshotID)
	if pricingChanged && opts.Mode == SyncModeIncremental {
		// Incremental consolidation only re-collects rows at/after the previous
		// cutoff. Catalog changes alter the cost of every historical token, so
		// retaining that window would leave older rows priced with stale rates.
		opts.Mode = SyncModeRebuild
		report.Mode = SyncModeRebuild
	}

	// The finality boundary never regresses across incremental syncs, except
	// down to the start of its own hour: the cache only ever holds complete
	// clock-hour buckets, so a non-aligned cutoff inherited from a pre-v4
	// cache snaps back once (the partial hour is re-collected by the next
	// consolidation; reads serve it live meanwhile). A rebuild instead uses
	// the requested cutoff directly: it re-collects every row from scratch
	// strictly before it, so a stuck-ahead stored cutoff (clock skew, a
	// hand-edited row) is repaired rather than inherited forever.
	cutoff := maxTime(millisToTime(current.LastSafeCutoff), opts.Cutoff).Truncate(time.Hour)
	if opts.Mode == SyncModeRebuild {
		cutoff = opts.Cutoff.Truncate(time.Hour)
	}
	report.Cutoff = cutoff
	report.FreshThrough = cutoff

	if opts.Mode == SyncModeIncremental {
		report.Since = millisToTime(current.LastSafeCutoff)
		// An unchanged fingerprint only proves no *new* raw data arrived — it says
		// nothing about rows that arrived before the last sync and fell inside its
		// recent window (>= that run's cutoff), which were skipped as un-finalized
		// and never written. Advancing the watermark past them would move them
		// behind the cache's finality boundary while they are absent from the
		// cache, permanently hiding them from every view. So only short-circuit
		// while the cutoff has not moved past what was actually consolidated;
		// otherwise fall through and collect [LastSafeCutoff, cutoff).
		cutoffAdvanced := cutoff.After(millisToTime(current.LastSafeCutoff))
		if ok && current.Status == "ready" && fpReliable && current.Fingerprint == fp && current.FreshThrough > 0 && !cutoffAdvanced {
			s.logger.Debug("cache sync: raw data unchanged, advancing watermarks", "source", info.ID, "cutoff", logTime(cutoff))
			retErr = s.advanceWatermarks(ctx, info, fp, cutoff, cutoff)
			return report, retErr
		}
	}

	start := time.Now()
	startLog := s.logger.Info
	if opts.ReadTriggered {
		startLog = s.logger.Debug
	}
	startLog("cache sync: collecting from source",
		"source", info.ID, "mode", opts.Mode, "read_triggered", opts.ReadTriggered,
		"since", logTime(report.Since), "cutoff", logTime(cutoff))

	prog := s.progressFunc(opts.Progress)
	payload, summary, err := collectSource(ctx, src, info, syncWindow{Since: report.Since, Cutoff: cutoff}, prog)
	if err != nil {
		s.logger.Warn("cache sync failed while collecting", "source", info.ID, "mode", opts.Mode, "read_triggered", opts.ReadTriggered, "error", err)
		if !opts.ReadTriggered {
			failedFingerprint := fp
			if pricingChanged {
				// No rows were replaced, so retain the pricing identity that the
				// cached rows actually use. A later incremental retry will again
				// promote itself to a full historical rebuild.
				failedFingerprint = current.Fingerprint
			}
			_ = s.replaceFailed(ctx, info, failedFingerprint, current, err)
		}
		retErr = err
		return report, retErr
	}
	payload.Fingerprint = fp
	report.Messages = len(payload.Messages)
	report.Tools = len(payload.Tools)
	report.SkippedRecent = summary.SkippedRecent
	report.SkippedOld = summary.SkippedOld
	report.Changed = report.Messages > 0 || report.Tools > 0 || !ok || current.Fingerprint != fp || current.Status != "ready"

	prog(SyncProgress{SourceID: string(info.ID), Phase: "write", Done: int64(report.Messages), Total: int64(report.Messages)})
	if opts.Mode == SyncModeRebuild {
		retErr = s.replaceSource(ctx, payload, cutoff)
	} else {
		retErr = s.fillSource(ctx, payload, report.Since, cutoff)
	}
	if retErr != nil {
		s.logger.Warn("cache sync failed while writing", "source", info.ID, "mode", opts.Mode, "error", retErr)
		return report, retErr
	}
	doneLog := s.logger.Info
	if opts.ReadTriggered && !report.Changed {
		doneLog = s.logger.Debug
	}
	doneLog("cache sync done",
		"source", info.ID, "mode", opts.Mode, "read_triggered", opts.ReadTriggered,
		"messages", report.Messages, "tools", report.Tools,
		"skipped_old", report.SkippedOld, "skipped_recent", report.SkippedRecent,
		"consolidated_through", logTime(cutoff), "duration", time.Since(start).Round(time.Millisecond))
	return report, nil
}

func logTime(t time.Time) string {
	if t.IsZero() {
		return "beginning"
	}
	return t.UTC().Format("2006-01-02 15:04:05Z")
}

// progressFunc forwards progress to next (when set) and logs a throttled
// console line so long consolidations are visible even without a job consumer.
func (s *Store) progressFunc(next func(SyncProgress)) func(SyncProgress) {
	lastLog := time.Now() // quick syncs finish without a progress line
	return func(p SyncProgress) {
		if next != nil {
			next(p)
		}
		now := time.Now()
		if now.Sub(lastLog) < 2*time.Second {
			return
		}
		lastLog = now
		s.logger.Info("cache sync progress", "source", p.SourceID, "phase", p.Phase, "done", p.Done, "total", p.Total)
	}
}

func (s *Store) sourceLock(sourceID string) *sync.Mutex {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncLocks == nil {
		s.syncLocks = make(map[string]*sync.Mutex)
	}
	lock, ok := s.syncLocks[sourceID]
	if !ok {
		lock = &sync.Mutex{}
		s.syncLocks[sourceID] = lock
	}
	return lock
}

func normalizeSyncOptions(opts SyncOptions) SyncOptions {
	if opts.Mode != SyncModeRebuild {
		opts.Mode = SyncModeIncremental
	}
	if opts.Cutoff.IsZero() {
		opts.Cutoff = DefaultSafeCutoff(time.Now())
	}
	opts.Cutoff = opts.Cutoff.UTC()
	return opts
}

// DefaultSafeCutoff is the hour-aligned finality boundary: the start of the
// UTC hour at least DefaultSyncSafetyDelay before now. The cache consolidates
// only rows strictly before it, so it always holds complete clock-hour
// buckets (minutes 00-59); the remaining recent window is read live and
// merged at query time.
func DefaultSafeCutoff(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC().Add(-DefaultSyncSafetyDelay).Truncate(time.Hour)
}

// advanceWatermarks records a no-collect verification: the fingerprint was
// unchanged and the cutoff had not advanced past what is consolidated. It
// still stamps last_synced_ms = now — that field means "last time the cache
// was verified fresh through the cutoff", so deferring the next read-triggered
// staleness check by consolidationStaleness is correct; the periodic auto-sync
// re-verifies on its own cadence regardless.
func (s *Store) advanceWatermarks(ctx context.Context, info source.SourceInfo, fp string, safeCutoff, freshThrough time.Time) error {
	if err := s.beginWrite(ctx); err != nil {
		return err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := insertSourceState(ctx, tx, info, fp, "ready", "", safeCutoff, freshThrough); err != nil {
		return err
	}
	return s.commitState(tx, string(info.ID))
}

type sourcePayload struct {
	Info        source.SourceInfo
	Fingerprint string
	Projects    []projectRow
	Sessions    []sessionRow
	Messages    []messageRow
	Tools       []toolRow
}

type projectRow struct {
	ProjectID   string
	ProjectName string
	Worktree    string
}

type sessionRow struct {
	SessionID    string
	Title        string
	ProjectID    string
	ProjectName  string
	TimeCreated  time.Time
	TimeUpdated  time.Time
	MessageCount int64
	Cost         float64
	Status       stats.CostStatus
	Provenance   *stats.CostProvenance
}

type messageRow struct {
	Entry       stats.MessageEntry
	ProjectID   string
	ProjectName string
	ModelTokens *stats.TokenStats
}

type toolRow struct {
	MessageID   string
	SessionID   string
	TimeCreated time.Time
	Name        string
	Status      string
}

// markUnavailable records that the source's raw data is currently missing
// WITHOUT deleting the cached rows. Unavailability is routinely transient (a
// request-scoped discovery timeout, a stat hiccup, an unmounted disk), and
// unavailable sources are never served from the cache anyway, so wiping here
// would only destroy data. When prior state exists, the fingerprint the
// cached rows actually correspond to and both watermarks are preserved, so
// recovery is a cheap incremental collect and a junk fallback fingerprint
// computed while unavailable cannot later force a spurious full rebuild.
// Rows are only ever removed by boundary deletes, an explicit rebuild, or
// PruneUnknownSources.
func (s *Store) markUnavailable(ctx context.Context, info source.SourceInfo, fp string, current SourceStatus, hasState bool) error {
	if err := s.beginWrite(ctx); err != nil {
		return err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	stateFp, cutoff, fresh := fp, time.Time{}, time.Time{}
	if hasState {
		stateFp = current.Fingerprint
		cutoff = millisToTime(current.LastSafeCutoff)
		fresh = millisToTime(current.FreshThrough)
	}
	if err := insertSourceState(ctx, tx, info, stateFp, "unavailable", info.Diagnostics.Reason, cutoff, fresh); err != nil {
		return err
	}
	return s.commitState(tx, string(info.ID))
}

// PruneUnknownSources deletes every cached row whose source_id is not in
// keep. Rebuilds only re-collect currently-registered sources, so rows for a
// renamed or de-configured source would otherwise survive "rebuild the entire
// database" indefinitely. Returns the pruned source ids. No sourceLock is
// taken: pruned ids have no registered live source, hence no concurrent sync;
// writeSem alone serializes the write (respecting sourceLock -> writeSem).
func (s *Store) PruneUnknownSources(ctx context.Context, keep []string) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if err := s.beginWrite(ctx); err != nil {
		return nil, err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	keepWhere := ""
	keepArgs := make([]any, 0, len(keep))
	if len(keep) > 0 {
		keepWhere = " WHERE source_id NOT IN (" + inPlaceholders(len(keep)) + ")"
		for _, id := range keep {
			keepArgs = append(keepArgs, id)
		}
	}
	pruned := make([]string, 0)
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT source_id FROM source_state`+keepWhere, keepArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		pruned = append(pruned, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Sweep each data table independently so orphan rows without a state row
	// are caught too.
	tables := []string{
		"hourly_model_cost",
		"hourly_model_sessions",
		"overview_hourly_cost",
		"overview_hourly_sessions",
		"overview_hourly",
		"hourly_usage",
		"tool_index",
		"message_index",
		"sessions",
		"projects",
		"source_state",
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+keepWhere, keepArgs...); err != nil {
			return nil, fmt.Errorf("prune %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, id := range pruned {
		s.invalidateStateMemo(id)
	}
	return pruned, nil
}

// replaceFailed records the error but preserves the existing watermarks so a
// transient failure neither resets finality nor forces a full re-collection.
func (s *Store) replaceFailed(ctx context.Context, info source.SourceInfo, fp string, current SourceStatus, syncErr error) error {
	if err := s.beginWrite(ctx); err != nil {
		return err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := insertSourceState(ctx, tx, info, fp, "error", syncErr.Error(), millisToTime(current.LastSafeCutoff), millisToTime(current.FreshThrough)); err != nil {
		return err
	}
	return s.commitState(tx, string(info.ID))
}

func (s *Store) replaceSource(ctx context.Context, payload sourcePayload, cutoff time.Time) error {
	sourceID := string(payload.Info.ID)
	if err := s.beginWrite(ctx); err != nil {
		return err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	if err := deleteSourceRows(ctx, tx, sourceID); err != nil {
		return err
	}
	if err := insertSourceState(ctx, tx, payload.Info, payload.Fingerprint, "ready", "", cutoff, cutoff); err != nil {
		return err
	}
	if err := insertProjects(ctx, tx, sourceID, payload.Projects); err != nil {
		return err
	}
	if err := insertSessions(ctx, tx, sourceID, payload.Sessions); err != nil {
		return err
	}
	if err := insertMessages(ctx, tx, sourceID, payload.Messages); err != nil {
		return err
	}
	if err := insertTools(ctx, tx, sourceID, payload.Tools); err != nil {
		return err
	}
	if err := refreshSessionRollups(ctx, tx, sourceID); err != nil {
		return err
	}
	if err := rebuildHourlyUsage(ctx, tx, sourceID); err != nil {
		return err
	}
	if err := rebuildOverviewHourly(ctx, tx, sourceID); err != nil {
		return err
	}
	return s.commitState(tx, sourceID)
}

// fillSource consolidates the newly finalized window [since, cutoff) into the
// cache: every row at/after since is deleted and replaced by the freshly
// collected payload. The delete also prunes any rows at/after the cutoff left
// behind by pre-v4 caches that mirrored the un-finalized recent window.
func (s *Store) fillSource(ctx context.Context, payload sourcePayload, since, cutoff time.Time) error {
	sourceID := string(payload.Info.ID)
	if err := s.beginWrite(ctx); err != nil {
		return err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	if err := insertSourceState(ctx, tx, payload.Info, payload.Fingerprint, "ready", "", cutoff, cutoff); err != nil {
		return err
	}
	// When a pre-v4 cutoff snaps back to its hour start, cutoff < since: the
	// delete boundary follows so the partial hour's rows leave the cache too
	// and the strict rows-strictly-before-cutoff invariant holds immediately.
	boundary := since
	if cutoff.Before(boundary) {
		boundary = cutoff
	}
	sinceMs := timeToMillis(boundary)
	if err := prepareChangedSessions(ctx, tx, sourceID, sinceMs, payload); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tool_index WHERE source_id = ? AND time_created_ms >= ?`, sourceID, sinceMs); err != nil {
		return fmt.Errorf("clear gap tool rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_index WHERE source_id = ? AND time_created_ms >= ?`, sourceID, sinceMs); err != nil {
		return fmt.Errorf("clear gap message rows: %w", err)
	}
	if err := insertProjects(ctx, tx, sourceID, payload.Projects); err != nil {
		return err
	}
	if err := insertSessions(ctx, tx, sourceID, payload.Sessions); err != nil {
		return err
	}
	if err := insertMessages(ctx, tx, sourceID, payload.Messages); err != nil {
		return err
	}
	if err := deleteToolsForMessages(ctx, tx, sourceID, payload.Messages); err != nil {
		return err
	}
	if err := insertTools(ctx, tx, sourceID, payload.Tools); err != nil {
		return err
	}
	if err := refreshChangedSessionRollups(ctx, tx, sourceID); err != nil {
		return err
	}
	if err := refreshHourlyUsage(ctx, tx, sourceID, sinceMs); err != nil {
		return err
	}
	if err := refreshOverviewHourly(ctx, tx, sourceID, sinceMs); err != nil {
		return err
	}
	return s.commitState(tx, sourceID)
}

func deleteSourceRows(ctx context.Context, tx *sql.Tx, sourceID string) error {
	tables := []string{
		"hourly_model_cost",
		"hourly_model_sessions",
		"overview_hourly_cost",
		"overview_hourly_sessions",
		"overview_hourly",
		"hourly_usage",
		"tool_index",
		"message_index",
		"sessions",
		"projects",
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE source_id = ?", sourceID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}

func insertSourceState(ctx context.Context, tx *sql.Tx, info source.SourceInfo, fp, status, reason string, safeCutoff, freshThrough time.Time) error {
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return err
	}
	diagJSON, err := json.Marshal(info.Diagnostics)
	if err != nil {
		return err
	}
	costJSON, err := json.Marshal(info.CostPolicy)
	if err != nil {
		return err
	}
	privacyJSON, err := json.Marshal(info.Privacy)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO source_state (
			source_id, label, kind, path, path_source, available, diagnostics_json,
			cost_policy_json, privacy_json, source_info_json, fingerprint, status,
			reason, last_synced_ms, last_safe_cutoff_ms, fresh_through_ms, data_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, string(info.ID), info.Label, info.Kind, info.Path, info.PathSource, boolInt(info.Available),
		string(diagJSON), string(costJSON), string(privacyJSON), string(infoJSON), fp, status, nullEmpty(reason), time.Now().UTC().UnixMilli(), timeToMillis(safeCutoff), timeToMillis(freshThrough), dataVersion)
	if err != nil {
		return fmt.Errorf("insert source state: %w", err)
	}
	return nil
}

func insertProjects(ctx context.Context, tx *sql.Tx, sourceID string, rows []projectRow) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO projects(source_id, project_id, project_name, worktree)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(source_id, project_id) DO UPDATE SET
			project_name = excluded.project_name,
			worktree = excluded.worktree
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.ProjectID == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, sourceID, row.ProjectID, row.ProjectName, nullEmpty(row.Worktree)); err != nil {
			return fmt.Errorf("insert project %s: %w", row.ProjectID, err)
		}
	}
	return nil
}

func insertSessions(ctx context.Context, tx *sql.Tx, sourceID string, rows []sessionRow) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO sessions(
			source_id, session_id, title, project_id, project_name, time_created_ms,
			time_updated_ms, message_count, cost, cost_status, cost_provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, session_id) DO UPDATE SET
			title = excluded.title,
			project_id = excluded.project_id,
			project_name = excluded.project_name,
			time_created_ms = excluded.time_created_ms,
			time_updated_ms = excluded.time_updated_ms,
			message_count = excluded.message_count,
			cost = excluded.cost,
			cost_status = excluded.cost_status,
			cost_provenance_json = excluded.cost_provenance_json
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.SessionID == "" {
			continue
		}
		prov, err := marshalProvenance(row.Provenance)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, sourceID, row.SessionID, row.Title, row.ProjectID, row.ProjectName, row.TimeCreated.UTC().UnixMilli(), row.TimeUpdated.UTC().UnixMilli(), row.MessageCount, row.Cost, string(row.Status), prov); err != nil {
			return fmt.Errorf("insert session %s: %w", row.SessionID, err)
		}
	}
	return nil
}

func insertMessages(ctx context.Context, tx *sql.Tx, sourceID string, rows []messageRow) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO message_index(
			source_id, message_id, session_id, role, time_created_ms,
			cost, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens,
			cache_write_tokens, model_input_tokens, model_output_tokens,
			model_reasoning_tokens, model_cache_read_tokens, model_cache_write_tokens,
			model_id, provider_id, service_tier, processing_mode, request_trace, usage_status,
			agent, is_subagent,
			folded_assistant_calls, folded_tool_calls, folded_token_updates,
			cost_status, cost_provenance_json, project_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, message_id) DO UPDATE SET
			session_id = excluded.session_id,
			role = excluded.role,
			time_created_ms = excluded.time_created_ms,
			cost = excluded.cost,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			reasoning_tokens = excluded.reasoning_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			cache_write_tokens = excluded.cache_write_tokens,
			model_input_tokens = excluded.model_input_tokens,
			model_output_tokens = excluded.model_output_tokens,
			model_reasoning_tokens = excluded.model_reasoning_tokens,
			model_cache_read_tokens = excluded.model_cache_read_tokens,
			model_cache_write_tokens = excluded.model_cache_write_tokens,
			model_id = excluded.model_id,
			provider_id = excluded.provider_id,
			service_tier = excluded.service_tier,
			processing_mode = excluded.processing_mode,
			request_trace = excluded.request_trace,
			usage_status = excluded.usage_status,
			agent = excluded.agent,
			is_subagent = excluded.is_subagent,
			folded_assistant_calls = excluded.folded_assistant_calls,
			folded_tool_calls = excluded.folded_tool_calls,
			folded_token_updates = excluded.folded_token_updates,
			cost_status = excluded.cost_status,
			cost_provenance_json = excluded.cost_provenance_json,
			project_id = excluded.project_id
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		entry := row.Entry
		if entry.ID == "" {
			continue
		}
		var tokens stats.TokenStats
		if entry.Tokens != nil {
			tokens = *entry.Tokens
		}
		modelTokens := tokens
		if row.ModelTokens != nil {
			modelTokens = *row.ModelTokens
		}
		prov, err := marshalProvenance(entry.CostProvenance)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			sourceID, entry.ID, entry.SessionID, entry.Role, entry.TimeCreated.UTC().UnixMilli(),
			entry.Cost, tokens.Input, tokens.Output, tokens.Reasoning, tokens.Cache.Read, tokens.Cache.Write,
			modelTokens.Input, modelTokens.Output, modelTokens.Reasoning, modelTokens.Cache.Read, modelTokens.Cache.Write,
			nullEmpty(entry.ModelID), nullEmpty(entry.ProviderID), nullEmpty(entry.ServiceTier), nullEmpty(string(entry.ProcessingMode)),
			nullEmpty(string(entry.RequestTrace)), nullEmpty(string(entry.UsageStatus)),
			nullEmpty(entry.Agent), boolInt(entry.IsSubagent),
			entry.FoldedAssistantCalls, entry.FoldedToolCalls, entry.FoldedTokenUpdates,
			string(entry.CostStatus), prov, row.ProjectID,
		); err != nil {
			return fmt.Errorf("insert message %s: %w", entry.ID, err)
		}
	}
	return nil
}

func insertTools(ctx context.Context, tx *sql.Tx, sourceID string, rows []toolRow) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tool_index(source_id, message_id, session_id, time_created_ms, tool_name, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, sourceID, row.MessageID, row.SessionID, row.TimeCreated.UTC().UnixMilli(), row.Name, nullEmpty(row.Status)); err != nil {
			return fmt.Errorf("insert tool %s: %w", row.Name, err)
		}
	}
	return nil
}

func deleteToolsForMessages(ctx context.Context, tx *sql.Tx, sourceID string, rows []messageRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM tool_index WHERE source_id = ? AND message_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.Entry.ID == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, sourceID, row.Entry.ID); err != nil {
			return fmt.Errorf("delete tools for message %s: %w", row.Entry.ID, err)
		}
	}
	return nil
}

// prepareChangedSessions snapshots every session whose cached messages may be
// replaced by an incremental fill. The temporary table lives on the
// transaction's SQLite connection and is cleared before each use. Capturing
// the ids before the suffix delete is important: an upstream deletion can
// remove the last message of a session, so the incoming payload alone is not
// sufficient to identify every rollup that must be repaired.
func prepareChangedSessions(ctx context.Context, tx *sql.Tx, sourceID string, sinceMs int64, payload sourcePayload) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS cache_changed_sessions (
			session_id TEXT PRIMARY KEY
		) WITHOUT ROWID;
		DELETE FROM cache_changed_sessions;
	`); err != nil {
		return fmt.Errorf("prepare changed-session set: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO cache_changed_sessions(session_id)
		SELECT DISTINCT session_id
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ?
	`, sourceID, sinceMs); err != nil {
		return fmt.Errorf("snapshot changed sessions: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO cache_changed_sessions(session_id) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare changed-session insert: %w", err)
	}
	defer stmt.Close()
	for _, row := range payload.Sessions {
		if row.SessionID != "" {
			if _, err := stmt.ExecContext(ctx, row.SessionID); err != nil {
				return fmt.Errorf("mark changed session %s: %w", row.SessionID, err)
			}
		}
	}
	for _, row := range payload.Messages {
		if row.Entry.SessionID != "" {
			if _, err := stmt.ExecContext(ctx, row.Entry.SessionID); err != nil {
				return fmt.Errorf("mark changed message session %s: %w", row.Entry.SessionID, err)
			}
		}
	}
	return nil
}

// refreshChangedSessionRollups updates only sessions touched by the suffix
// replacement. The previous implementation recomputed four correlated
// aggregates for every historical session after every hourly fill, which made
// small cache updates grow linearly with the lifetime size of the cache.
func refreshChangedSessionRollups(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE source_id = ?
		  AND session_id IN (SELECT session_id FROM cache_changed_sessions)
		  AND NOT EXISTS (
			SELECT 1
			FROM message_index m
			WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
		  )
	`, sourceID); err != nil {
		return fmt.Errorf("remove changed empty sessions: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET
			time_created_ms = (
				SELECT MIN(m.time_created_ms)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			),
			time_updated_ms = (
				SELECT MAX(m.time_created_ms)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			),
			message_count = (
				SELECT COUNT(*)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			),
			cost = COALESCE((
				SELECT SUM(m.cost)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			), 0)
		WHERE source_id = ?
		  AND session_id IN (SELECT session_id FROM cache_changed_sessions)
	`, sourceID)
	if err != nil {
		return fmt.Errorf("refresh changed session rollups: %w", err)
	}
	return nil
}

func refreshSessionRollups(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE source_id = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM message_index m
			WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
		  )
	`, sourceID); err != nil {
		return fmt.Errorf("remove empty sessions: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET
			time_created_ms = (
				SELECT MIN(m.time_created_ms)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			),
			time_updated_ms = (
				SELECT MAX(m.time_created_ms)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			),
			message_count = (
				SELECT COUNT(*)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			),
			cost = COALESCE((
				SELECT SUM(m.cost)
				FROM message_index m
				WHERE m.source_id = sessions.source_id AND m.session_id = sessions.session_id
			), 0)
		WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return fmt.Errorf("refresh session rollups: %w", err)
	}
	return nil
}

func rebuildHourlyUsage(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM hourly_usage WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO hourly_usage(
			source_id, bucket_start_ms, model_id, provider_id, role,
			messages, requests, usage_recorded, usage_recovered, usage_unavailable,
			trace_observed, trace_inferred, cost, input_tokens, output_tokens, reasoning_tokens,
			cache_read_tokens, cache_write_tokens
		)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000 AS bucket_start_ms,
			COALESCE(model_id, ''),
			COALESCE(provider_id, ''),
			role,
			COUNT(*),
			SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END),
			SUM(CASE WHEN usage_status = 'recorded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN usage_status = 'recovered' THEN 1 ELSE 0 END),
			SUM(CASE WHEN usage_status = 'unavailable' THEN 1 ELSE 0 END),
			SUM(CASE WHEN request_trace = 'observed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN request_trace = 'inferred' THEN 1 ELSE 0 END),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(model_input_tokens), 0),
			COALESCE(SUM(model_output_tokens), 0),
			COALESCE(SUM(model_reasoning_tokens), 0),
			COALESCE(SUM(model_cache_read_tokens), 0),
			COALESCE(SUM(model_cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND COALESCE(model_id, '') != ''
		GROUP BY source_id, bucket_start_ms, model_id, provider_id, role
	`, sourceID)
	if err != nil {
		return fmt.Errorf("rebuild hourly usage: %w", err)
	}
	return nil
}

func hourBucketStartMs(ms int64) int64 {
	const hourMs = int64(time.Hour / time.Millisecond)
	if ms >= 0 {
		return (ms / hourMs) * hourMs
	}
	// Go integer division truncates toward zero; SQLite's historical timestamps
	// are normally positive, but floor negative values correctly as well.
	return ((ms - hourMs + 1) / hourMs) * hourMs
}

// refreshHourlyUsage rebuilds only buckets that can have changed after the
// suffix replacement. The first bucket is intentionally rounded down because
// legacy caches can carry a non-hour-aligned watermark.
func refreshHourlyUsage(ctx context.Context, tx *sql.Tx, sourceID string, sinceMs int64) error {
	bucketMs := hourBucketStartMs(sinceMs)
	if _, err := tx.ExecContext(ctx, `DELETE FROM hourly_usage WHERE source_id = ? AND bucket_start_ms >= ?`, sourceID, bucketMs); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO hourly_usage(
			source_id, bucket_start_ms, model_id, provider_id, role,
			messages, requests, usage_recorded, usage_recovered, usage_unavailable,
			trace_observed, trace_inferred, cost, input_tokens, output_tokens, reasoning_tokens,
			cache_read_tokens, cache_write_tokens
		)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000 AS bucket_start_ms,
			COALESCE(model_id, ''),
			COALESCE(provider_id, ''),
			role,
			COUNT(*),
			SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END),
			SUM(CASE WHEN usage_status = 'recorded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN usage_status = 'recovered' THEN 1 ELSE 0 END),
			SUM(CASE WHEN usage_status = 'unavailable' THEN 1 ELSE 0 END),
			SUM(CASE WHEN request_trace = 'observed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN request_trace = 'inferred' THEN 1 ELSE 0 END),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(model_input_tokens), 0),
			COALESCE(SUM(model_output_tokens), 0),
			COALESCE(SUM(model_reasoning_tokens), 0),
			COALESCE(SUM(model_cache_read_tokens), 0),
			COALESCE(SUM(model_cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND role = 'assistant' AND COALESCE(model_id, '') != ''
		GROUP BY source_id, bucket_start_ms, model_id, provider_id, role
	`, sourceID, bucketMs)
	if err != nil {
		return fmt.Errorf("refresh hourly usage: %w", err)
	}
	return nil
}

func rebuildOverviewHourly(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if err := deleteOverviewHourly(ctx, tx, sourceID, 0, false); err != nil {
		return err
	}
	return insertOverviewHourly(ctx, tx, sourceID, 0, false)
}

func refreshOverviewHourly(ctx context.Context, tx *sql.Tx, sourceID string, sinceMs int64) error {
	bucketMs := hourBucketStartMs(sinceMs)
	if err := deleteOverviewHourly(ctx, tx, sourceID, bucketMs, true); err != nil {
		return err
	}
	return insertOverviewHourly(ctx, tx, sourceID, bucketMs, true)
}

func deleteOverviewHourly(ctx context.Context, tx *sql.Tx, sourceID string, bucketMs int64, bounded bool) error {
	where := `source_id = ?`
	args := []any{sourceID}
	if bounded {
		where += ` AND bucket_start_ms >= ?`
		args = append(args, bucketMs)
	}
	for _, table := range []string{"hourly_model_cost", "hourly_model_sessions", "overview_hourly_cost", "overview_hourly_sessions", "overview_hourly"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+where, args...); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}

func insertOverviewHourly(ctx context.Context, tx *sql.Tx, sourceID string, bucketMs int64, bounded bool) error {
	where := `source_id = ?`
	args := []any{sourceID}
	if bounded {
		where += ` AND time_created_ms >= ?`
		args = append(args, bucketMs)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO overview_hourly(
			source_id, bucket_start_ms, messages, requests,
			usage_recorded, usage_recovered, usage_unavailable, trace_observed, trace_inferred,
			cost, input_tokens, output_tokens,
			reasoning_tokens, cache_read_tokens, cache_write_tokens
		)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000,
			COUNT(*),
			SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END),
			SUM(CASE WHEN role = 'assistant' AND usage_status = 'recorded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN role = 'assistant' AND usage_status = 'recovered' THEN 1 ELSE 0 END),
			SUM(CASE WHEN role = 'assistant' AND usage_status = 'unavailable' THEN 1 ELSE 0 END),
			SUM(CASE WHEN role = 'assistant' AND request_trace = 'observed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN role = 'assistant' AND request_trace = 'inferred' THEN 1 ELSE 0 END),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE `+where+`
		GROUP BY source_id, (time_created_ms / 3600000) * 3600000
	`, args...); err != nil {
		return fmt.Errorf("refresh overview hourly totals: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO overview_hourly_sessions(source_id, bucket_start_ms, session_id)
		SELECT DISTINCT source_id, (time_created_ms / 3600000) * 3600000, session_id
		FROM message_index
		WHERE `+where+`
	`, args...); err != nil {
		return fmt.Errorf("refresh overview hourly sessions: %w", err)
	}
	costWhere := where + ` AND role = 'assistant'`
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO overview_hourly_cost(source_id, bucket_start_ms, cost_status, messages)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000,
			COALESCE(cost_status, ''),
			COUNT(*)
		FROM message_index
		WHERE `+costWhere+`
		GROUP BY source_id, (time_created_ms / 3600000) * 3600000, COALESCE(cost_status, '')
	`, args...); err != nil {
		return fmt.Errorf("refresh overview hourly cost status: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hourly_model_sessions(source_id, bucket_start_ms, model_id, provider_id, session_id)
		SELECT DISTINCT
			source_id,
			(time_created_ms / 3600000) * 3600000,
			COALESCE(model_id, ''),
			COALESCE(provider_id, ''),
			session_id
		FROM message_index
		WHERE `+where+` AND role = 'assistant' AND COALESCE(model_id, '') != ''
	`, args...); err != nil {
		return fmt.Errorf("refresh hourly model sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hourly_model_cost(source_id, bucket_start_ms, model_id, provider_id, cost_status, messages)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000,
			COALESCE(model_id, ''),
			COALESCE(provider_id, ''),
			COALESCE(cost_status, ''),
			COUNT(*)
		FROM message_index
		WHERE `+where+` AND role = 'assistant' AND COALESCE(model_id, '') != ''
		GROUP BY
			source_id, (time_created_ms / 3600000) * 3600000,
			COALESCE(model_id, ''), COALESCE(provider_id, ''), COALESCE(cost_status, '')
	`, args...); err != nil {
		return fmt.Errorf("refresh hourly model cost status: %w", err)
	}
	return nil
}

type syncWindow struct {
	Since  time.Time
	Cutoff time.Time
}

type collectSummary struct {
	SkippedRecent int
	SkippedOld    int
}

func collectSource(ctx context.Context, src source.Source, info source.SourceInfo, window syncWindow, progress func(SyncProgress)) (sourcePayload, collectSummary, error) {
	payload := sourcePayload{Info: info}
	sourceID := string(info.ID)
	if bulk, ok := src.(source.ConsolidationSource); ok {
		return collectConsolidationSource(ctx, bulk, payload, sourceID, window, progress)
	}

	sessionMap, err := collectSessions(ctx, src, sourceID, window, progress)
	if err != nil {
		return payload, collectSummary{}, err
	}

	messages, tools, summary, err := collectMessagesAndTools(ctx, src, sourceID, sessionMap, window, progress)
	if err != nil {
		return payload, summary, err
	}
	payload.Messages = messages
	payload.Tools = tools
	payload.Sessions = sessionsFromMessages(sourceID, sessionMap, messages)
	payload.Projects = projectsFromMessages(messages)

	ensureProjectsFromMessages(&payload)
	ensureSessionsFromMessages(&payload)
	return payload, summary, nil
}

type cachedSession struct {
	SessionID    string
	Title        string
	ProjectID    string
	ProjectName  string
	Created      time.Time
	Updated      time.Time
	MessageCount int64
	Cost         float64
	Status       stats.CostStatus
	Provenance   *stats.CostProvenance
}

// windowFromHint narrows live-source pagination to days that can contain new
// rows. From is day-granular, so same-day rows before Since still come back
// and are filtered by the in-Go window checks.
func windowFromHint(window syncWindow) string {
	if window.Since.IsZero() {
		return ""
	}
	return window.Since.UTC().Format("2006-01-02")
}

func periodForWindow(window syncWindow) stats.PeriodQuery {
	pq := stats.PeriodQuery{Period: "all"}
	if from := windowFromHint(window); from != "" {
		// FromTime both selects a bounded JSONL snapshot and applies the exact
		// lower bound; From is retained for sources that only understand dates.
		pq = stats.PeriodQuery{From: from, FromTime: window.Since.UTC()}
	}
	if !window.Cutoff.IsZero() {
		pq.ToTime = window.Cutoff.UTC()
	}
	return pq
}

// collectConsolidationSource consumes a single stable, metadata-only snapshot
// from JSONL sources. Besides avoiding repeated filtering/sorting, this is
// essential for detail metadata: the generic fallback calls MessageByID once
// per row, which would otherwise rediscover and reparse transcript files tens
// of thousands of times during an initial sync.
func collectConsolidationSource(ctx context.Context, bulk source.ConsolidationSource, payload sourcePayload, sourceID string, window syncWindow, progress func(SyncProgress)) (sourcePayload, collectSummary, error) {
	callCtx, cancel := context.WithTimeout(ctx, collectCallTimeout)
	data, err := bulk.ConsolidationData(callCtx, periodForWindow(window))
	cancel()
	if err != nil {
		return payload, collectSummary{}, fmt.Errorf("cache collect consolidation data: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return payload, collectSummary{}, fmt.Errorf("cache collect consolidation data: %w", err)
	}

	sessions := make(map[string]cachedSession, len(data.Sessions))
	for _, entry := range data.Sessions {
		if err := ctx.Err(); err != nil {
			return payload, collectSummary{}, fmt.Errorf("cache collect consolidation data: %w", err)
		}
		sessions[entry.ID] = cachedSession{
			SessionID:    entry.ID,
			Title:        safeSessionTitle(sourceID, entry.ID),
			ProjectID:    entry.ProjectID,
			ProjectName:  entry.ProjectName,
			Created:      entry.TimeCreated,
			Updated:      entry.TimeUpdated,
			MessageCount: entry.MessageCount,
			Cost:         entry.Cost,
			Status:       entry.CostStatus,
			Provenance:   entry.CostProvenance,
		}
	}
	if progress != nil {
		count := int64(len(data.Sessions))
		progress(SyncProgress{SourceID: sourceID, Phase: "sessions", Done: count, Total: count})
	}

	messages := make([]messageRow, 0, len(data.Messages))
	tools := make([]toolRow, 0)
	var summary collectSummary
	total := int64(len(data.Messages))
	if progress != nil && total == 0 {
		progress(SyncProgress{SourceID: sourceID, Phase: "messages", Done: 0, Total: 0})
	}
	reportMessageProgress := func(done int) {
		if progress != nil && (done%100 == 0 || done == len(data.Messages)) {
			progress(SyncProgress{SourceID: sourceID, Phase: "messages", Done: int64(done), Total: total})
		}
	}
	for i, item := range data.Messages {
		if err := ctx.Err(); err != nil {
			return payload, summary, fmt.Errorf("cache collect consolidation data: %w", err)
		}
		entry := item.Entry
		// Keep this guard even though snapshot-aware sources apply the exact
		// query. It protects the cache boundary from an optional implementation
		// that treats From as day-granular or ignores ToTime.
		switch {
		case !window.Since.IsZero() && entry.TimeCreated.Before(window.Since):
			summary.SkippedOld++
			reportMessageProgress(i + 1)
			continue
		case !window.Cutoff.IsZero() && !entry.TimeCreated.Before(window.Cutoff):
			summary.SkippedRecent++
			reportMessageProgress(i + 1)
			continue
		}

		session := sessions[entry.SessionID]
		entry.SourceID = sourceID
		entry.SessionTitle = safeSessionTitle(sourceID, entry.SessionID)
		if entry.CostStatus == "" && entry.Role == "assistant" {
			entry.CostStatus = data.CostStatus
			entry.CostProvenance = data.CostProvenance
		}
		messages = append(messages, messageRow{
			Entry: entry, ProjectID: session.ProjectID, ProjectName: session.ProjectName,
			ModelTokens: item.ModelTokens,
		})
		for _, tool := range item.Tools {
			if tool.Name == "" {
				continue
			}
			tools = append(tools, toolRow{
				MessageID:   entry.ID,
				SessionID:   entry.SessionID,
				TimeCreated: entry.TimeCreated,
				Name:        tool.Name,
				Status:      tool.Status,
			})
		}
		reportMessageProgress(i + 1)
	}

	payload.Messages = messages
	payload.Tools = tools
	payload.Sessions = sessionsFromMessages(sourceID, sessions, messages)
	payload.Projects = projectsFromMessages(messages)
	ensureProjectsFromMessages(&payload)
	ensureSessionsFromMessages(&payload)
	return payload, summary, nil
}

func collectSessions(ctx context.Context, src source.Source, sourceID string, window syncWindow, progress func(SyncProgress)) (map[string]cachedSession, error) {
	result := make(map[string]cachedSession)
	query := stats.SessionQuery{PageSize: syncPageSize, Sort: stats.SessionSortOldest, Period: "all"}
	if !window.Cutoff.IsZero() {
		query.ToTime = window.Cutoff.UTC()
	}
	if from := windowFromHint(window); from != "" {
		query.Period = ""
		query.From = from
		// Time-precision hint engages bounded loads in the JSONL sources.
		query.FromTime = window.Since.UTC()
	}
	for page := 1; ; page++ {
		query.Page = page
		pageCtx, cancel := context.WithTimeout(ctx, collectCallTimeout)
		list, err := src.Sessions(pageCtx, query)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("cache collect sessions: %w", err)
		}
		if progress != nil {
			progress(SyncProgress{SourceID: sourceID, Phase: "sessions", Done: int64(len(result) + len(list.Sessions)), Total: list.Total})
		}
		for _, entry := range list.Sessions {
			title := safeSessionTitle(sourceID, entry.ID)
			result[entry.ID] = cachedSession{
				SessionID:    entry.ID,
				Title:        title,
				ProjectID:    entry.ProjectID,
				ProjectName:  entry.ProjectName,
				Created:      entry.TimeCreated,
				Updated:      entry.TimeUpdated,
				MessageCount: entry.MessageCount,
				Cost:         entry.Cost,
				Status:       entry.CostStatus,
				Provenance:   entry.CostProvenance,
			}
		}
		if len(list.Sessions) == 0 || int64(page*list.PageSize) >= list.Total {
			break
		}
	}
	return result, nil
}

func collectProjects(ctx context.Context, src source.Source, sessions map[string]cachedSession) ([]projectRow, error) {
	seen := make(map[string]projectRow)
	projects, err := src.Projects(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		return nil, fmt.Errorf("cache collect projects: %w", err)
	}
	for _, entry := range projects.Projects {
		if entry.ProjectID == "" {
			continue
		}
		seen[entry.ProjectID] = projectRow{ProjectID: entry.ProjectID, ProjectName: entry.ProjectName}
	}
	for _, session := range sessions {
		if session.ProjectID == "" {
			continue
		}
		if _, ok := seen[session.ProjectID]; !ok {
			seen[session.ProjectID] = projectRow{ProjectID: session.ProjectID, ProjectName: session.ProjectName}
		}
	}
	rows := make([]projectRow, 0, len(seen))
	for _, row := range seen {
		rows = append(rows, row)
	}
	return rows, nil
}

func collectMessagesAndTools(ctx context.Context, src source.Source, sourceID string, sessions map[string]cachedSession, window syncWindow, progress func(SyncProgress)) ([]messageRow, []toolRow, collectSummary, error) {
	messages := make([]messageRow, 0)
	tools := make([]toolRow, 0)
	var summary collectSummary
	var seen int64
	pq := periodForWindow(window)
	for page := 1; ; page++ {
		// One deadline covers the page query and its at-most-syncPageSize
		// detail lookups. The next page receives a fresh budget.
		pageCtx, cancel := context.WithTimeout(ctx, collectCallTimeout)
		list, err := src.Messages(pageCtx, pq, page, syncPageSize, syncSort)
		if err != nil {
			cancel()
			return nil, nil, summary, fmt.Errorf("cache collect messages: %w", err)
		}
		seen += int64(len(list.Messages))
		for _, entry := range list.Messages {
			// Consolidation keeps the half-open window [Since, Cutoff): rows
			// at/after the cutoff stay un-finalized and are served live by the
			// gap-merge layer until the cutoff passes them.
			switch {
			case !window.Since.IsZero() && entry.TimeCreated.Before(window.Since):
				summary.SkippedOld++
				continue
			case !window.Cutoff.IsZero() && !entry.TimeCreated.Before(window.Cutoff):
				summary.SkippedRecent++
				continue
			}
			session := sessions[entry.SessionID]
			entry.SourceID = sourceID
			entry.SessionTitle = safeSessionTitle(sourceID, entry.SessionID)
			if entry.CostStatus == "" && entry.Role == "assistant" {
				entry.CostStatus = list.CostStatus
				entry.CostProvenance = list.CostProvenance
			}
			row := messageRow{Entry: entry, ProjectID: session.ProjectID, ProjectName: session.ProjectName}
			messages = append(messages, row)

			detail, err := src.MessageByID(pageCtx, entry.ID)
			if err != nil {
				cancel()
				return nil, nil, summary, fmt.Errorf("cache collect message tools %s: %w", entry.ID, err)
			}
			if detail == nil {
				continue
			}
			for _, part := range detail.Content.ToolParts {
				if part.Tool == "" {
					continue
				}
				tools = append(tools, toolRow{
					MessageID:   entry.ID,
					SessionID:   entry.SessionID,
					TimeCreated: entry.TimeCreated,
					Name:        part.Tool,
					Status:      part.State.Status,
				})
			}
		}
		if progress != nil {
			// Reported after the page's per-message detail fetches, the slow part.
			progress(SyncProgress{SourceID: sourceID, Phase: "messages", Done: seen, Total: list.Total})
		}
		cancel()
		if len(list.Messages) == 0 || int64(page*list.PageSize) >= list.Total {
			break
		}
	}
	return messages, tools, summary, nil
}

func sessionsFromMessages(sourceID string, sessions map[string]cachedSession, messages []messageRow) []sessionRow {
	seen := make(map[string]bool)
	rows := make([]sessionRow, 0)
	for _, msg := range messages {
		sessionID := msg.Entry.SessionID
		if sessionID == "" || seen[sessionID] {
			continue
		}
		session := sessions[sessionID]
		row := sessionRow{
			SessionID:    sessionID,
			Title:        session.Title,
			ProjectID:    session.ProjectID,
			ProjectName:  session.ProjectName,
			TimeCreated:  session.Created,
			TimeUpdated:  session.Updated,
			MessageCount: session.MessageCount,
			Cost:         session.Cost,
			Status:       session.Status,
			Provenance:   session.Provenance,
		}
		if row.Title == "" {
			row.Title = safeSessionTitle(sourceID, sessionID)
		}
		if row.ProjectID == "" {
			row.ProjectID = msg.ProjectID
			row.ProjectName = msg.ProjectName
		}
		if row.TimeCreated.IsZero() {
			row.TimeCreated = msg.Entry.TimeCreated
		}
		if row.TimeUpdated.IsZero() {
			row.TimeUpdated = msg.Entry.TimeCreated
		}
		rows = append(rows, row)
		seen[sessionID] = true
	}
	return rows
}

func projectsFromMessages(messages []messageRow) []projectRow {
	seen := make(map[string]projectRow)
	for _, msg := range messages {
		if msg.ProjectID == "" {
			continue
		}
		seen[msg.ProjectID] = projectRow{ProjectID: msg.ProjectID, ProjectName: msg.ProjectName}
	}
	rows := make([]projectRow, 0, len(seen))
	for _, row := range seen {
		rows = append(rows, row)
	}
	return rows
}

func ensureProjectsFromMessages(payload *sourcePayload) {
	seen := make(map[string]bool)
	for _, row := range payload.Projects {
		seen[row.ProjectID] = true
	}
	for _, msg := range payload.Messages {
		if msg.ProjectID == "" || seen[msg.ProjectID] {
			continue
		}
		payload.Projects = append(payload.Projects, projectRow{ProjectID: msg.ProjectID, ProjectName: msg.ProjectName})
		seen[msg.ProjectID] = true
	}
}

func ensureSessionsFromMessages(payload *sourcePayload) {
	seen := make(map[string]bool)
	for _, row := range payload.Sessions {
		seen[row.SessionID] = true
	}
	for _, msg := range payload.Messages {
		if msg.Entry.SessionID == "" || seen[msg.Entry.SessionID] {
			continue
		}
		payload.Sessions = append(payload.Sessions, sessionRow{
			SessionID:    msg.Entry.SessionID,
			Title:        safeSessionTitle(string(payload.Info.ID), msg.Entry.SessionID),
			ProjectID:    msg.ProjectID,
			ProjectName:  msg.ProjectName,
			TimeCreated:  msg.Entry.TimeCreated,
			TimeUpdated:  msg.Entry.TimeCreated,
			MessageCount: 1,
		})
		seen[msg.Entry.SessionID] = true
	}
}

func safeSessionTitle(sourceID, sessionID string) string {
	if sessionID == "" {
		return "Session"
	}
	short := sessionID
	if len(short) > 12 {
		short = short[:12]
	}
	return "Session " + short
}

func marshalProvenance(prov *stats.CostProvenance) (any, error) {
	if prov == nil {
		return nil, nil
	}
	b, err := json.Marshal(prov)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func nullEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func timeToMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}

func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
