package cache

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
)

// schemaVersion is the version of the cache database's schema, stored in
// PRAGMA user_version. Bump it ONLY when schemaSQL changes (a table, column,
// or index is added, removed, or altered) — never for app releases or data-
// semantics changes (those bump dataVersion in store.go).
//
// The cache holds only data derived from the sources, so there are no in-place
// migrations: when Open finds a dashboard cache stamped with any other version
// — older or newer — it removes the database files and rebuilds the schema
// from scratch, and the normal consolidation flow re-collects everything in
// the background while reads fall back to the live sources.
//
// v5 matches the last version written by the retired migration ladder, so
// existing up-to-date caches are adopted without a rebuild.
const schemaVersion = 5

// schemaSQL is the complete current schema, applied in one transaction to a
// fresh (or just-rebuilt) database. It must always describe the exact shape
// this binary's queries expect; any edit here requires a schemaVersion bump.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS source_state (
	source_id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	kind TEXT NOT NULL,
	path TEXT,
	path_source TEXT,
	available INTEGER NOT NULL DEFAULT 0,
	diagnostics_json TEXT,
	cost_policy_json TEXT,
	privacy_json TEXT,
	source_info_json TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	reason TEXT,
	last_synced_ms INTEGER NOT NULL,
	last_safe_cutoff_ms INTEGER NOT NULL DEFAULT 0,
	fresh_through_ms INTEGER NOT NULL DEFAULT 0,
	data_version INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS projects (
	source_id TEXT NOT NULL,
	project_id TEXT NOT NULL,
	project_name TEXT NOT NULL,
	worktree TEXT,
	PRIMARY KEY (source_id, project_id)
);

CREATE TABLE IF NOT EXISTS sessions (
	source_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	title TEXT NOT NULL,
	project_id TEXT,
	project_name TEXT,
	time_created_ms INTEGER NOT NULL,
	time_updated_ms INTEGER NOT NULL,
	message_count INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	cost_status TEXT,
	cost_provenance_json TEXT,
	PRIMARY KEY (source_id, session_id)
);

CREATE TABLE IF NOT EXISTS message_index (
	source_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	session_title TEXT NOT NULL,
	role TEXT NOT NULL,
	time_created_ms INTEGER NOT NULL,
	cost REAL NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	model_id TEXT,
	provider_id TEXT,
	agent TEXT,
	is_subagent INTEGER NOT NULL DEFAULT 0,
	folded_assistant_calls INTEGER NOT NULL DEFAULT 0,
	folded_tool_calls INTEGER NOT NULL DEFAULT 0,
	folded_token_updates INTEGER NOT NULL DEFAULT 0,
	cost_status TEXT,
	cost_provenance_json TEXT,
	project_id TEXT,
	project_name TEXT,
	service_tier TEXT,
	processing_mode TEXT,
	model_input_tokens INTEGER NOT NULL DEFAULT 0,
	model_output_tokens INTEGER NOT NULL DEFAULT 0,
	model_reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	model_cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	model_cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, message_id)
);

CREATE TABLE IF NOT EXISTS tool_index (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	project_id TEXT,
	project_name TEXT,
	time_created_ms INTEGER NOT NULL,
	tool_name TEXT NOT NULL,
	status TEXT
);

CREATE TABLE IF NOT EXISTS hourly_usage (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	project_id TEXT NOT NULL,
	project_name TEXT NOT NULL,
	model_id TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	role TEXT NOT NULL,
	sessions INTEGER NOT NULL DEFAULT 0,
	messages INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms, project_id, model_id, provider_id, role)
);

CREATE TABLE IF NOT EXISTS hourly_tool_usage (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	tool_name TEXT NOT NULL,
	invocations INTEGER NOT NULL DEFAULT 0,
	successes INTEGER NOT NULL DEFAULT 0,
	failures INTEGER NOT NULL DEFAULT 0,
	sessions INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms, tool_name)
);

CREATE TABLE IF NOT EXISTS overview_hourly (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	messages INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms)
);

CREATE TABLE IF NOT EXISTS overview_hourly_sessions (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	PRIMARY KEY (source_id, bucket_start_ms, session_id)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS overview_hourly_cost (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	cost_status TEXT NOT NULL,
	messages INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms, cost_status)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS hourly_model_sessions (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	model_id TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	PRIMARY KEY (source_id, bucket_start_ms, model_id, provider_id, session_id)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS hourly_model_cost (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	model_id TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	cost_status TEXT NOT NULL,
	messages INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms, model_id, provider_id, cost_status)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_message_index_source_time ON message_index(source_id, time_created_ms);
CREATE INDEX IF NOT EXISTS idx_message_index_source_session ON message_index(source_id, session_id);
CREATE INDEX IF NOT EXISTS idx_message_index_source_project ON message_index(source_id, project_id);
CREATE INDEX IF NOT EXISTS idx_message_index_source_model ON message_index(source_id, model_id, provider_id);
CREATE INDEX IF NOT EXISTS idx_message_index_source_processing_mode ON message_index(source_id, role, processing_mode, time_created_ms);
CREATE INDEX IF NOT EXISTS idx_sessions_source_project ON sessions(source_id, project_id);
CREATE INDEX IF NOT EXISTS idx_tool_index_source_time ON tool_index(source_id, time_created_ms);
CREATE INDEX IF NOT EXISTS idx_tool_index_source_name ON tool_index(source_id, tool_name);
CREATE INDEX IF NOT EXISTS idx_hourly_usage_source_bucket ON hourly_usage(source_id, bucket_start_ms);
CREATE INDEX IF NOT EXISTS idx_hourly_tool_usage_source_bucket ON hourly_tool_usage(source_id, bucket_start_ms);
`

// schemaState is what ensureSchemaVersion inspects before deciding between
// create-in-place, adopt, rebuild, and refuse.
type schemaState struct {
	version int
	tables  int
	isCache bool
}

// ensureSchemaVersion makes the database at path match schemaVersion exactly.
// An empty database gets the schema created in place. A dashboard cache
// stamped with any other version — older or newer — is removed and rebuilt
// from scratch (the returned handle replaces db, which is closed). A non-empty
// database that is not a dashboard cache is refused and never touched: the
// path is user-suppliable and this function must not destroy unrelated files.
func ensureSchemaVersion(ctx context.Context, db *sql.DB, path string) (*sql.DB, error) {
	state, err := inspectSchema(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	switch {
	case state.tables == 0:
		if err := createSchema(ctx, db); err != nil {
			_ = db.Close()
			return nil, err
		}
		return db, nil
	case state.version == schemaVersion && state.isCache:
		return db, nil
	case !state.isCache:
		_ = db.Close()
		return nil, fmt.Errorf("%s is not a dashboard cache database (no source_state table); refusing to rebuild it", path)
	}

	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close outdated cache database: %w", err)
	}
	slog.Default().Info("cache: database schema version does not match this binary; removing and rebuilding",
		"path", path, "have", state.version, "want", schemaVersion)
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove outdated cache database %q: %w", candidate, err)
		}
	}
	db, err = openDB(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := createSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// inspectSchema reads the version stamp and table inventory in one immediate
// transaction so a concurrent opener sees a consistent snapshot.
func inspectSchema(ctx context.Context, db *sql.DB) (schemaState, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return schemaState{}, fmt.Errorf("begin cache schema inspection: %w", err)
	}
	defer rollback(tx)

	var state schemaState
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&state.version); err != nil {
		return schemaState{}, fmt.Errorf("read cache schema version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&state.tables); err != nil {
		return schemaState{}, fmt.Errorf("inspect cache database: %w", err)
	}
	var sourceState int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'source_state'`).Scan(&sourceState); err != nil {
		return schemaState{}, fmt.Errorf("inspect cache database: %w", err)
	}
	state.isCache = sourceState > 0
	return state, nil
}

// createSchema applies schemaSQL and stamps schemaVersion in one transaction.
// The DDL is fully idempotent, so concurrent creators serialized by
// _txlock=immediate each apply it harmlessly.
func createSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cache schema creation: %w", err)
	}
	defer rollback(tx)

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create cache schema: %w", err)
	}
	// PRAGMA cannot take bind parameters; the version is an internal constant.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("record cache schema version %d: %w", schemaVersion, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cache schema creation: %w", err)
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
