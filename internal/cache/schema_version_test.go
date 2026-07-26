package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openRawSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	return queryInt(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name) > 0
}

func TestOpenFreshDatabaseStampsSchemaVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fresh.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if got := queryInt(t, store.db, `PRAGMA user_version`); got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
	for _, dead := range []string{"source_files", "schema_migrations"} {
		if tableExists(t, store.db, dead) {
			t.Errorf("dead table %s exists in a fresh database", dead)
		}
	}
	for _, dead := range []string{"hourly_tool_usage"} {
		if tableExists(t, store.db, dead) {
			t.Errorf("dead table %s exists in a fresh database", dead)
		}
	}
	for _, live := range []string{
		"source_state", "projects", "sessions", "message_index", "tool_index",
		"hourly_usage", "overview_hourly",
		"overview_hourly_sessions", "overview_hourly_cost",
		"hourly_model_sessions", "hourly_model_cost",
	} {
		if !tableExists(t, store.db, live) {
			t.Errorf("live table %s missing in a fresh database", live)
		}
	}
	if got := queryInt(t, store.db, `SELECT COUNT(*) FROM pragma_table_info('source_state') WHERE name='data_version'`); got != 1 {
		t.Errorf("source_state.data_version column missing")
	}
	for _, column := range []string{
		"service_tier", "processing_mode", "request_trace", "usage_status", "usage_unavailable_reason",
		"model_input_tokens", "model_output_tokens", "model_reasoning_tokens",
		"model_cache_read_tokens", "model_cache_write_tokens",
	} {
		if got := queryInt(t, store.db, `SELECT COUNT(*) FROM pragma_table_info('message_index') WHERE name = ?`, column); got != 1 {
			t.Errorf("message_index.%s column count = %d, want 1", column, got)
		}
	}
	for _, table := range []string{"hourly_usage", "overview_hourly"} {
		for _, column := range []string{
			"requests", "usage_recorded", "usage_recovered", "usage_unavailable",
			"usage_unavailable_cancelled", "usage_unavailable_interrupted",
			"usage_unavailable_failed", "usage_unavailable_unknown",
			"trace_observed", "trace_inferred",
		} {
			if got := queryInt(t, store.db, fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, table), column); got != 1 {
				t.Errorf("%s.%s column count = %d, want 1", table, column, got)
			}
		}
	}
	if got := queryInt(t, store.db, `SELECT COUNT(*) FROM pragma_index_info('idx_message_index_source_processing_mode') WHERE name = 'processing_mode'`); got != 1 {
		t.Errorf("processing-mode index contains processing_mode %d times, want 1", got)
	}
}

func TestOpenIsIdempotentAcrossReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reopen.sqlite")

	for i := 0; i < 3; i++ {
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("Open() #%d failed: %v", i+1, err)
		}
		if got := queryInt(t, store.db, `PRAGMA user_version`); got != schemaVersion {
			t.Errorf("user_version after open #%d = %d, want %d", i+1, got, schemaVersion)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() #%d failed: %v", i+1, err)
		}
	}
}

// seedConsolidatedRows stamps a ready source with one cached message so a test
// can tell whether a reopen adopted the database or rebuilt it.
func seedConsolidatedRows(t *testing.T, store *Store) {
	t.Helper()
	if _, err := store.db.Exec(fmt.Sprintf(`
		INSERT INTO source_state (source_id, label, kind, source_info_json, fingerprint, status, last_synced_ms, last_safe_cutoff_ms, fresh_through_ms, data_version)
		VALUES ('codex', 'Codex', 'jsonl', '{}', 'current-fp', 'ready', %d, 1700000000000, 1700000000000, %d);
		INSERT INTO message_index (source_id, message_id, session_id, role, time_created_ms, input_tokens)
		VALUES ('codex', 'codex:s1:t1:r0', 's1', 'assistant', 1690000000000, 42);
	`, time.Now().UnixMilli(), dataVersion)); err != nil {
		t.Fatalf("seed consolidated rows: %v", err)
	}
}

func TestOpenAdoptsMatchingSchemaVersionPreservingRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "adopt.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	seedConsolidatedRows(t, store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("re-Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if got := queryInt(t, store.db, `SELECT input_tokens FROM message_index WHERE message_id = 'codex:s1:t1:r0'`); got != 42 {
		t.Errorf("cached message input_tokens = %d, want 42 preserved across matching-version reopen", got)
	}
	status, ok, err := store.SourceStatus(ctx, "codex")
	if err != nil || !ok {
		t.Fatalf("SourceStatus(codex) = %v %v", ok, err)
	}
	if status.Fingerprint != "current-fp" || status.LastSafeCutoff != 1700000000000 {
		t.Errorf("current-version consolidation state was reset: %+v", status)
	}
}

// rebuildCase drives Open against a cache stamped with the given user_version
// and asserts the file was removed and rebuilt: current version, empty tables.
func rebuildCase(t *testing.T, name string, version int) {
	t.Run(name, func(t *testing.T) {
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "rebuild.sqlite")

		store, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("Open() failed: %v", err)
		}
		seedConsolidatedRows(t, store)
		if _, err := store.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
			t.Fatalf("stamp version %d: %v", version, err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() failed: %v", err)
		}

		store, err = Open(ctx, path)
		if err != nil {
			t.Fatalf("re-Open() on version-%d cache failed: %v", version, err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if got := queryInt(t, store.db, `PRAGMA user_version`); got != schemaVersion {
			t.Errorf("user_version after rebuild = %d, want %d", got, schemaVersion)
		}
		if got := queryInt(t, store.db, `SELECT COUNT(*) FROM message_index`); got != 0 {
			t.Errorf("message_index rows after rebuild = %d, want 0 (database removed and recreated)", got)
		}
		if got := queryInt(t, store.db, `SELECT COUNT(*) FROM source_state`); got != 0 {
			t.Errorf("source_state rows after rebuild = %d, want 0 (sources re-consolidate from scratch)", got)
		}
	})
}

func TestOpenRebuildsMismatchedSchemaVersion(t *testing.T) {
	rebuildCase(t, "older version", schemaVersion-1)
	rebuildCase(t, "newer version", schemaVersion+1)
	rebuildCase(t, "far future version", 999)
}

// TestOpenRebuildsLegacyUnversionedCache covers caches written before the
// version stamp existed: recognizably a dashboard cache (source_state present)
// but user_version 0 — a mismatch, so it is rebuilt rather than upgraded.
func TestOpenRebuildsLegacyUnversionedCache(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")

	db := openRawSQLite(t, path)
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO source_state (source_id, label, kind, source_info_json, fingerprint, status, last_synced_ms)
		VALUES ('codex', 'Codex', 'jsonl', '{}', 'old-fp', 'ready', 1000)
	`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() on legacy unversioned cache failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if got := queryInt(t, store.db, `PRAGMA user_version`); got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
	if got := queryInt(t, store.db, `SELECT COUNT(*) FROM source_state`); got != 0 {
		t.Errorf("source_state rows = %d, want 0 after rebuild", got)
	}
}

func TestOpenRefusesForeignDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreign.sqlite")
	db := openRawSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE something_else (x INTEGER); INSERT INTO something_else VALUES (7);`); err != nil {
		t.Fatalf("seed foreign db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close foreign db: %v", err)
	}

	_, err := Open(ctx, path)
	if err == nil {
		t.Fatalf("Open() on a foreign database succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "not a dashboard cache database") {
		t.Errorf("error = %q, want foreign-database refusal", err)
	}

	// The file must be untouched: still only the foreign table, data intact.
	db = openRawSQLite(t, path)
	if got := queryInt(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name != 'something_else'`); got != 0 {
		t.Errorf("foreign database gained %d tables", got)
	}
	if got := queryInt(t, db, `SELECT x FROM something_else`); got != 7 {
		t.Errorf("foreign data = %d, want 7 untouched", got)
	}
}

// TestOpenRefusesForeignDatabaseWithMatchingVersion pins the deletion guard on
// the nastiest input: a non-cache file whose user_version happens to equal
// schemaVersion. Version agreement alone must never qualify a file for
// adoption — or, on the next bump, deletion.
func TestOpenRefusesForeignDatabaseWithMatchingVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreign-versioned.sqlite")
	db := openRawSQLite(t, path)
	if _, err := db.Exec(fmt.Sprintf(`
		CREATE TABLE something_else (x INTEGER);
		INSERT INTO something_else VALUES (7);
		PRAGMA user_version = %d;
	`, schemaVersion)); err != nil {
		t.Fatalf("seed foreign db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close foreign db: %v", err)
	}

	_, err := Open(ctx, path)
	if err == nil {
		t.Fatalf("Open() on a version-matching foreign database succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "not a dashboard cache database") {
		t.Errorf("error = %q, want foreign-database refusal", err)
	}
	db = openRawSQLite(t, path)
	if got := queryInt(t, db, `SELECT x FROM something_else`); got != 7 {
		t.Errorf("foreign data = %d, want 7 untouched", got)
	}
}

func TestConcurrentOpenCreatesSchemaOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent.sqlite")

	// Opening one brand-new cache file from several goroutines can lose the
	// WAL-setup race at connect time (pre-existing Open behavior, not a real
	// deployment scenario). The creation path's guarantee is narrower and is
	// what this test pins: whoever gets through creates the schema exactly
	// once and nobody corrupts the database.
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			store, err := Open(ctx, path)
			if err != nil {
				errs[slot] = err
				return
			}
			errs[slot] = store.Close()
		}(i)
	}
	wg.Wait()
	succeeded := 0
	for i, err := range errs {
		if err == nil {
			succeeded++
		} else if !strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(err.Error(), "database is locked") {
			t.Errorf("concurrent Open #%d failed with a non-lock error: %v", i, err)
		}
	}
	if succeeded == 0 {
		t.Fatalf("no concurrent Open succeeded")
	}

	db := openRawSQLite(t, path)
	if got := queryInt(t, db, `PRAGMA user_version`); got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
}

func TestCurrentDataVersionRowsAreNotReset(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "stable.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	if _, err := store.db.Exec(fmt.Sprintf(`
		INSERT INTO source_state (source_id, label, kind, source_info_json, fingerprint, status, last_synced_ms, last_safe_cutoff_ms, fresh_through_ms, data_version)
		VALUES ('codex', 'Codex', 'jsonl', '{}', 'current-fp', 'ready', %d, 1700000000000, 1700000000000, %d)
	`, time.Now().UnixMilli(), dataVersion)); err != nil {
		t.Fatalf("seed current-version row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("re-Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	status, ok, err := store.SourceStatus(ctx, "codex")
	if err != nil || !ok {
		t.Fatalf("SourceStatus(codex) = %v %v", ok, err)
	}
	if status.Fingerprint != "current-fp" || status.LastSafeCutoff != 1700000000000 {
		t.Errorf("current-version consolidation state was reset: %+v", status)
	}
}

func TestOutdatedDataVersionRowsAreReset(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outdated-data.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	if _, err := store.db.Exec(fmt.Sprintf(`
		INSERT INTO source_state (source_id, label, kind, source_info_json, fingerprint, status, last_synced_ms, last_safe_cutoff_ms, fresh_through_ms, data_version)
		VALUES ('codex', 'Codex', 'jsonl', '{}', 'old-fp', 'ready', %d, 1700000000000, 1700000000000, %d)
	`, time.Now().UnixMilli(), dataVersion-1)); err != nil {
		t.Fatalf("seed outdated-version row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("re-Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	status, ok, err := store.SourceStatus(ctx, "codex")
	if err != nil || !ok {
		t.Fatalf("SourceStatus(codex) = %v %v", ok, err)
	}
	if status.Fingerprint != "" || status.LastSafeCutoff != 0 || status.FreshThrough != 0 || status.LastSynced != 0 {
		t.Errorf("outdated consolidation state = %+v, want fully reset for re-collection", status)
	}
	if got := queryInt(t, store.db, `SELECT data_version FROM source_state WHERE source_id='codex'`); got != dataVersion {
		t.Errorf("data_version = %d, want %d stamped", got, dataVersion)
	}
}

// TestSchemaSQLChangesRequireVersionBump is a tripwire: schemaSQL and
// schemaVersion must change together. There are no migrations by design — a
// version mismatch deletes the cache file and rebuilds it from raw sources —
// so an edited schemaSQL with an unchanged schemaVersion would silently leave
// existing caches on the old table shape forever (every statement is
// CREATE-IF-NOT-EXISTS and never re-runs on an adopted database).
//
// If this test fails after an intentional schema change: bump schemaVersion
// in schema.go AND update recordedSchemaVersion + recordedSchemaDigest below
// in the same commit (the failure message prints the new digest).
func TestSchemaSQLChangesRequireVersionBump(t *testing.T) {
	const recordedSchemaVersion = 8
	const recordedSchemaDigest = "c847b6dca1646ebed397dd8dba2c4cc35c80da953c5b4652ebaebeeb59fb97a5"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(schemaSQL)))
	if schemaVersion != recordedSchemaVersion || digest != recordedSchemaDigest {
		t.Fatalf("schemaSQL/schemaVersion drifted from the recorded pair.\n"+
			"  schemaVersion = %d (recorded %d)\n"+
			"  schemaSQL sha256 = %s\n"+
			"  (recorded %s)\n"+
			"Any schemaSQL edit requires a schemaVersion bump: mismatched caches are deleted and rebuilt, never migrated. "+
			"Update recordedSchemaVersion and recordedSchemaDigest here in the same commit as the schema change.",
			schemaVersion, recordedSchemaVersion, digest, recordedSchemaDigest)
	}
}
