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
	// schemaVersion participates in the source fingerprint, so bumping it
	// forces every existing cache through a full collect on first sync after
	// upgrade. v4: finalized-only caches — rows at/after the hour-aligned
	// cutoff are no longer mirrored; the first v4 sync prunes them.
	schemaVersion          = 4
	busyTimeout            = 5000 * time.Millisecond
	DefaultSyncSafetyDelay = 6 * time.Hour
)

var syncSort = stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc}

type SyncMode string

const (
	SyncModeIncremental SyncMode = "incremental"
	SyncModeRebuild     SyncMode = "rebuild"
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

	// memoMu guards stateMemo (leaf lock; see merge_store.go).
	memoMu    sync.Mutex
	stateMemo map[string]sourceStateMemo

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

	db, err := sql.Open("sqlite", buildDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect cache database: %w", err)
	}

	store := &Store{db: db, path: path, logger: slog.New(slog.DiscardHandler), writeSem: make(chan struct{}, 1)}
	if err := store.ensureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
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
		"_pragma=foreign_keys(1)",
	}
	return path + "?" + strings.Join(params, "&")
}

func (s *Store) ensureSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create cache schema: %w", err)
	}
	if err := ensureSourceStateColumn(ctx, tx, "last_safe_cutoff_ms", "last_safe_cutoff_ms INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSourceStateColumn(ctx, tx, "fresh_through_ms", "fresh_through_ms INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO schema_migrations(version, applied_at_ms) VALUES(?, ?)`, schemaVersion, time.Now().UTC().UnixMilli()); err != nil {
		return fmt.Errorf("record cache schema version: %w", err)
	}
	return tx.Commit()
}

func ensureSourceStateColumn(ctx context.Context, tx *sql.Tx, column, ddl string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(source_state)`)
	if err != nil {
		return fmt.Errorf("inspect source_state schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan source_state schema: %w", err)
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source_state schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE source_state ADD COLUMN `+ddl); err != nil {
		return fmt.Errorf("add source_state.%s: %w", column, err)
	}
	return nil
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
			// Another sync is running; serve the current cache as-is.
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
			s.logger.Debug("cache fill skipped: source unavailable", "source", info.ID, "reason", info.Diagnostics.Reason)
			return report, nil
		}
		s.logger.Warn("cache sync: source unavailable, clearing cached rows", "source", info.ID, "reason", info.Diagnostics.Reason)
		return report, s.replaceUnavailable(ctx, info, fp)
	}

	// The finality boundary never regresses, except down to the start of its
	// own hour: the cache only ever holds complete clock-hour buckets, so a
	// non-aligned cutoff inherited from a pre-v4 cache snaps back once (the
	// partial hour is re-collected by the next consolidation; reads serve it
	// live meanwhile).
	cutoff := maxTime(millisToTime(current.LastSafeCutoff), opts.Cutoff).Truncate(time.Hour)
	report.Cutoff = cutoff
	report.FreshThrough = cutoff

	if opts.Mode == SyncModeIncremental {
		report.Since = millisToTime(current.LastSafeCutoff)
		if ok && current.Status == "ready" && fpReliable && current.Fingerprint == fp && current.FreshThrough > 0 {
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
			_ = s.replaceFailed(ctx, info, fp, current, err)
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
}

type toolRow struct {
	MessageID   string
	SessionID   string
	ProjectID   string
	ProjectName string
	TimeCreated time.Time
	Name        string
	Status      string
}

func (s *Store) replaceUnavailable(ctx context.Context, info source.SourceInfo, fp string) error {
	if err := s.beginWrite(ctx); err != nil {
		return err
	}
	defer s.endWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := deleteSourceRows(ctx, tx, string(info.ID)); err != nil {
		return err
	}
	if err := insertSourceState(ctx, tx, info, fp, "unavailable", info.Diagnostics.Reason, time.Time{}, time.Time{}); err != nil {
		return err
	}
	return s.commitState(tx, string(info.ID))
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
	if err := rebuildHourlyToolUsage(ctx, tx, sourceID); err != nil {
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
	if err := refreshSessionRollups(ctx, tx, sourceID); err != nil {
		return err
	}
	if err := rebuildHourlyUsage(ctx, tx, sourceID); err != nil {
		return err
	}
	if err := rebuildHourlyToolUsage(ctx, tx, sourceID); err != nil {
		return err
	}
	return s.commitState(tx, sourceID)
}

func deleteSourceRows(ctx context.Context, tx *sql.Tx, sourceID string) error {
	tables := []string{
		"hourly_tool_usage",
		"hourly_usage",
		"tool_index",
		"message_index",
		"sessions",
		"projects",
		"source_files",
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
			reason, last_synced_ms, last_safe_cutoff_ms, fresh_through_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, string(info.ID), info.Label, info.Kind, info.Path, info.PathSource, boolInt(info.Available),
		string(diagJSON), string(costJSON), string(privacyJSON), string(infoJSON), fp, status, nullEmpty(reason), time.Now().UTC().UnixMilli(), timeToMillis(safeCutoff), timeToMillis(freshThrough))
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
			source_id, message_id, session_id, session_title, role, time_created_ms,
			cost, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens,
			cache_write_tokens, model_id, provider_id, agent, is_subagent,
			folded_assistant_calls, folded_tool_calls, folded_token_updates,
			cost_status, cost_provenance_json, project_id, project_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, message_id) DO UPDATE SET
			session_id = excluded.session_id,
			session_title = excluded.session_title,
			role = excluded.role,
			time_created_ms = excluded.time_created_ms,
			cost = excluded.cost,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			reasoning_tokens = excluded.reasoning_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			cache_write_tokens = excluded.cache_write_tokens,
			model_id = excluded.model_id,
			provider_id = excluded.provider_id,
			agent = excluded.agent,
			is_subagent = excluded.is_subagent,
			folded_assistant_calls = excluded.folded_assistant_calls,
			folded_tool_calls = excluded.folded_tool_calls,
			folded_token_updates = excluded.folded_token_updates,
			cost_status = excluded.cost_status,
			cost_provenance_json = excluded.cost_provenance_json,
			project_id = excluded.project_id,
			project_name = excluded.project_name
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
		prov, err := marshalProvenance(entry.CostProvenance)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			sourceID, entry.ID, entry.SessionID, entry.SessionTitle, entry.Role, entry.TimeCreated.UTC().UnixMilli(),
			entry.Cost, tokens.Input, tokens.Output, tokens.Reasoning, tokens.Cache.Read, tokens.Cache.Write,
			nullEmpty(entry.ModelID), nullEmpty(entry.ProviderID), nullEmpty(entry.Agent), boolInt(entry.IsSubagent),
			entry.FoldedAssistantCalls, entry.FoldedToolCalls, entry.FoldedTokenUpdates,
			string(entry.CostStatus), prov, row.ProjectID, row.ProjectName,
		); err != nil {
			return fmt.Errorf("insert message %s: %w", entry.ID, err)
		}
	}
	return nil
}

func insertTools(ctx context.Context, tx *sql.Tx, sourceID string, rows []toolRow) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tool_index(source_id, message_id, session_id, project_id, project_name, time_created_ms, tool_name, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, sourceID, row.MessageID, row.SessionID, row.ProjectID, row.ProjectName, row.TimeCreated.UTC().UnixMilli(), row.Name, nullEmpty(row.Status)); err != nil {
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
			source_id, bucket_start_ms, project_id, project_name, model_id, provider_id, role,
			sessions, messages, cost, input_tokens, output_tokens, reasoning_tokens,
			cache_read_tokens, cache_write_tokens
		)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000 AS bucket_start_ms,
			COALESCE(project_id, ''),
			COALESCE(project_name, ''),
			COALESCE(model_id, ''),
			COALESCE(provider_id, ''),
			role,
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ?
		GROUP BY source_id, bucket_start_ms, project_id, project_name, model_id, provider_id, role
	`, sourceID)
	if err != nil {
		return fmt.Errorf("rebuild hourly usage: %w", err)
	}
	return nil
}

func rebuildHourlyToolUsage(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM hourly_tool_usage WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO hourly_tool_usage(
			source_id, bucket_start_ms, tool_name, invocations, successes, failures, sessions
		)
		SELECT
			source_id,
			(time_created_ms / 3600000) * 3600000 AS bucket_start_ms,
			tool_name,
			COUNT(*),
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),
			COUNT(DISTINCT session_id)
		FROM tool_index
		WHERE source_id = ?
		GROUP BY source_id, bucket_start_ms, tool_name
	`, sourceID)
	if err != nil {
		return fmt.Errorf("rebuild hourly tool usage: %w", err)
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

func collectSessions(ctx context.Context, src source.Source, sourceID string, window syncWindow, progress func(SyncProgress)) (map[string]cachedSession, error) {
	result := make(map[string]cachedSession)
	query := stats.SessionQuery{PageSize: 100, Sort: stats.SessionSortOldest, Period: "all"}
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
		list, err := src.Sessions(ctx, query)
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
	pq := stats.PeriodQuery{Period: "all"}
	if from := windowFromHint(window); from != "" {
		// Time-precision hint engages bounded loads in the JSONL sources.
		pq = stats.PeriodQuery{From: from, FromTime: window.Since.UTC()}
	}
	if !window.Cutoff.IsZero() {
		pq.ToTime = window.Cutoff.UTC()
	}
	for page := 1; ; page++ {
		list, err := src.Messages(ctx, pq, page, 100, syncSort)
		if err != nil {
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

			detail, err := src.MessageByID(ctx, entry.ID)
			if err != nil {
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
					ProjectID:   session.ProjectID,
					ProjectName: session.ProjectName,
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
