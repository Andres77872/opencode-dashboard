package cache

import (
	"context"
	"database/sql"
	"fmt"
)

// migration is one structural (DDL) upgrade step of the cache database.
// Structural history is tracked in PRAGMA user_version, which commits
// atomically with the DDL of each step. Data-semantics changes (normalizer
// fixes that require re-collecting from sources) do not belong here — they
// bump dataVersion instead.
type migration struct {
	version     int
	description string
	apply       func(ctx context.Context, tx *sql.Tx) error
}

// migrations is the ordered, append-only structural history of the cache
// database. Versions must be dense starting at 1: the runner applies
// migrations[v] to a database at user_version v. Never edit or reorder past
// entries; add new ones at the end.
var migrations = []migration{
	{1, "baseline: v4-era schema (idempotent create + source_state columns)", applyBaseline},
	{2, "drop dead source_files/schema_migrations tables, add source_state.data_version", applyDropDeadTablesAddDataVersion},
	{3, "add requested service tier and processing mode to cached messages", applyAddMessageProcessingMode},
}

func latestStructuralVersion() int {
	return migrations[len(migrations)-1].version
}

// migrate brings the cache database to the latest structural version, one
// transaction per migration so a partial failure resumes at the last committed
// step. The DSN's _txlock=immediate takes SQLite's write lock at BeginTx and
// the version is read inside the transaction, so a concurrent opener applies
// each step at most once.
func (s *Store) migrate(ctx context.Context) error {
	return s.migrateWith(ctx, migrations)
}

func (s *Store) migrateWith(ctx context.Context, list []migration) error {
	for i, m := range list {
		if m.version != i+1 {
			return fmt.Errorf("cache migration list is not dense: entry %d has version %d", i, m.version)
		}
	}
	latest := len(list)
	for {
		done, err := s.applyNextMigration(ctx, list, latest)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (s *Store) applyNextMigration(ctx context.Context, list []migration, latest int) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin cache migration: %w", err)
	}
	defer rollback(tx)

	version, err := userVersion(ctx, tx)
	if err != nil {
		return false, err
	}
	if version >= latest {
		if version > latest {
			return false, fmt.Errorf("cache database %s is at schema version %d, newer than this binary supports (%d)", s.path, version, latest)
		}
		return true, nil
	}
	if version == 0 {
		if err := guardForeignDB(ctx, tx, s.path); err != nil {
			return false, err
		}
	}
	next := list[version]
	if err := next.apply(ctx, tx); err != nil {
		return false, fmt.Errorf("apply cache migration %d (%s): %w", next.version, next.description, err)
	}
	if err := setUserVersion(ctx, tx, next.version); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit cache migration %d: %w", next.version, err)
	}
	return false, nil
}

func userVersion(ctx context.Context, tx *sql.Tx) (int, error) {
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read cache schema version: %w", err)
	}
	return version, nil
}

func setUserVersion(ctx context.Context, tx *sql.Tx, version int) error {
	// PRAGMA cannot take bind parameters; version is an internal constant.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("record cache schema version %d: %w", version, err)
	}
	return nil
}

// guardForeignDB refuses to migrate a non-empty database that is not a
// dashboard cache (no source_state table, present since the cache's first
// release). The --cache-db path is user-suppliable, so a mistake here must
// never mutate or destroy someone's unrelated SQLite file.
func guardForeignDB(ctx context.Context, tx *sql.Tx, path string) error {
	var tables int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect cache database: %w", err)
	}
	if tables == 0 {
		return nil // fresh database
	}
	var sourceState int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'source_state'`).Scan(&sourceState); err != nil {
		return fmt.Errorf("inspect cache database: %w", err)
	}
	if sourceState == 0 {
		return fmt.Errorf("%s is not a dashboard cache database (no source_state table); refusing to migrate it", path)
	}
	return nil
}

// applyBaseline normalizes fresh and every legacy pre-ledger database to the
// exact v4-era shape: the frozen schemaSQL is fully idempotent, and the two
// ALTERs cover source_state variants that predate those columns.
func applyBaseline(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create cache schema: %w", err)
	}
	if err := ensureSourceStateColumn(ctx, tx, "last_safe_cutoff_ms", "last_safe_cutoff_ms INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return ensureSourceStateColumn(ctx, tx, "fresh_through_ms", "fresh_through_ms INTEGER NOT NULL DEFAULT 0")
}

// applyDropDeadTablesAddDataVersion removes two write-only vestiges —
// source_files (an abandoned per-file scan design) and schema_migrations
// (superseded by PRAGMA user_version; its rows were unread stamps of whichever
// binary last opened the DB) — and adds the per-source data_version column
// that drives full re-collection when data semantics change (see dataVersion).
func applyDropDeadTablesAddDataVersion(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS source_files; DROP TABLE IF EXISTS schema_migrations;`); err != nil {
		return fmt.Errorf("drop dead cache tables: %w", err)
	}
	return ensureSourceStateColumn(ctx, tx, "data_version", "data_version INTEGER NOT NULL DEFAULT 0")
}

// applyAddMessageProcessingMode stores both the raw service tier selected by
// the Codex client and its normalized dashboard processing mode. The columns
// intentionally remain nullable: empty metadata from non-Codex sources is not
// rewritten as a Codex-specific "unknown" mode. Query-time aggregation applies
// that fallback only when the processing-mode dimension is requested.
func applyAddMessageProcessingMode(ctx context.Context, tx *sql.Tx) error {
	if err := ensureTableColumn(ctx, tx, "message_index", "service_tier", "service_tier TEXT"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, tx, "message_index", "processing_mode", "processing_mode TEXT"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_message_index_source_processing_mode
		ON message_index(source_id, role, processing_mode, time_created_ms)
	`); err != nil {
		return fmt.Errorf("create message processing-mode index: %w", err)
	}
	return nil
}

func ensureSourceStateColumn(ctx context.Context, tx *sql.Tx, column, ddl string) error {
	return ensureTableColumn(ctx, tx, "source_state", column, ddl)
}

func ensureTableColumn(ctx context.Context, tx *sql.Tx, table, column, ddl string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan %s schema: %w", table, err)
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s schema: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+ddl); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// resetOutdatedDataVersions invalidates the consolidation state of every source
// cached under an older dataVersion: clearing the fingerprint makes NeedsSync
// fire, zeroing last_safe_cutoff_ms makes the next sync collect from the
// beginning of time — fillSource's boundary delete then replaces every stale
// row (including rows whose message ids no longer exist upstream) — and
// zeroing last_synced_ms makes the read path's staleness check spawn that sync
// on the first request instead of waiting out the staleness window. A bump of
// dataVersion alone would only trigger an incremental sync that leaves
// previously consolidated rows untouched.
//
// Until the re-collection completes, reads stay correct: a zero cutoff means
// the cache serves nothing and the live source covers the whole window.
func (s *Store) resetOutdatedDataVersions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE source_state
		SET fingerprint = '', last_safe_cutoff_ms = 0, fresh_through_ms = 0, last_synced_ms = 0, data_version = ?
		WHERE data_version != ?
	`, dataVersion, dataVersion)
	if err != nil {
		return fmt.Errorf("reset outdated cache data versions: %w", err)
	}
	return nil
}
