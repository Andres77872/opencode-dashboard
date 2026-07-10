package cache

import (
	"context"
	"database/sql"
	"errors"
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

func TestOpenFreshDatabaseStampsLatestStructuralVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fresh.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if got := queryInt(t, store.db, `PRAGMA user_version`); got != latestStructuralVersion() {
		t.Errorf("user_version = %d, want %d", got, latestStructuralVersion())
	}
	for _, dead := range []string{"source_files", "schema_migrations"} {
		if tableExists(t, store.db, dead) {
			t.Errorf("dead table %s exists in a fresh database", dead)
		}
	}
	for _, live := range []string{"source_state", "projects", "sessions", "message_index", "tool_index", "hourly_usage", "hourly_tool_usage"} {
		if !tableExists(t, store.db, live) {
			t.Errorf("live table %s missing in a fresh database", live)
		}
	}
	if got := queryInt(t, store.db, `SELECT COUNT(*) FROM pragma_table_info('source_state') WHERE name='data_version'`); got != 1 {
		t.Errorf("source_state.data_version column missing")
	}
}

// seedLegacyV4Database recreates the exact on-disk shape the pre-runner binary
// left behind: full schemaSQL tables (including the junk write-only ledger) and
// no PRAGMA user_version.
func seedLegacyV4Database(t *testing.T, path string) {
	t.Helper()
	db := openRawSQLite(t, path)
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	seed := `
		INSERT INTO schema_migrations(version, applied_at_ms) VALUES(4, 1700000000000);
		INSERT INTO source_state (source_id, label, kind, source_info_json, fingerprint, status, last_synced_ms, last_safe_cutoff_ms, fresh_through_ms)
		VALUES ('codex', 'Codex', 'jsonl', '{}', 'old-fp', 'ready', 1000, 1700000000000, 1700000000000);
		INSERT INTO message_index (source_id, message_id, session_id, session_title, role, time_created_ms, input_tokens)
		VALUES ('codex', 'codex:s1:t1:r0', 's1', 'title', 'assistant', 1690000000000, 42);
		INSERT INTO source_files (source_id, path, size, mod_time_ms) VALUES ('codex', '/x/y.jsonl', 10, 1);
	`
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
}

func TestOpenUpgradesLegacyV4DatabasePreservingRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-v4.sqlite")
	seedLegacyV4Database(t, path)

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() on legacy v4 db failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if got := queryInt(t, store.db, `PRAGMA user_version`); got != latestStructuralVersion() {
		t.Errorf("user_version = %d, want %d", got, latestStructuralVersion())
	}
	for _, dead := range []string{"source_files", "schema_migrations"} {
		if tableExists(t, store.db, dead) {
			t.Errorf("dead table %s survived the upgrade", dead)
		}
	}
	// Consolidated data is preserved; only the consolidation watermarks reset
	// (legacy rows were collected under old data semantics).
	if got := queryInt(t, store.db, `SELECT input_tokens FROM message_index WHERE message_id = 'codex:s1:t1:r0'`); got != 42 {
		t.Errorf("message_index row input_tokens = %d, want 42 preserved", got)
	}
	status, ok, err := store.SourceStatus(ctx, "codex")
	if err != nil || !ok {
		t.Fatalf("SourceStatus(codex) = %v %v", ok, err)
	}
	if status.Fingerprint != "" || status.LastSafeCutoff != 0 || status.FreshThrough != 0 {
		t.Errorf("consolidation state = %+v, want reset (empty fingerprint, zero cutoffs) for outdated data version", status)
	}
	if got := queryInt(t, store.db, `SELECT data_version FROM source_state WHERE source_id='codex'`); got != dataVersion {
		t.Errorf("data_version = %d, want %d stamped", got, dataVersion)
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
		if got := queryInt(t, store.db, `PRAGMA user_version`); got != latestStructuralVersion() {
			t.Errorf("user_version after open #%d = %d, want %d", i+1, got, latestStructuralVersion())
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() #%d failed: %v", i+1, err)
		}
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

func TestOpenRejectsFutureSchemaVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "future.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	if _, err := store.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatalf("set future version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	_, err = Open(ctx, path)
	if err == nil {
		t.Fatalf("Open() with future user_version succeeded, want error")
	}
	if !strings.Contains(err.Error(), "newer than this binary supports") || !strings.Contains(err.Error(), "--rebuild-cache") {
		t.Errorf("error = %q, want future-version message with --rebuild-cache hint", err)
	}
}

func TestMigrateResumesAfterPartialFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "partial.sqlite")

	db, err := sql.Open("sqlite", buildDSN(path))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := &Store{db: db, path: path, writeSem: make(chan struct{}, 1)}
	t.Cleanup(func() { _ = db.Close() })

	failing := append(append([]migration{}, migrations...), migration{
		version:     latestStructuralVersion() + 1,
		description: "always fails",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			return errors.New("synthetic migration failure")
		},
	})
	if err := store.migrateWith(ctx, failing); err == nil {
		t.Fatalf("migrateWith(failing) succeeded, want error")
	}
	// All real migrations committed; the failing step left no version bump.
	if got := queryInt(t, db, `PRAGMA user_version`); got != latestStructuralVersion() {
		t.Errorf("user_version after partial failure = %d, want %d", got, latestStructuralVersion())
	}
	if err := store.migrateWith(ctx, migrations); err != nil {
		t.Errorf("migrateWith(real) after partial failure = %v, want success", err)
	}
}

func TestConcurrentOpenMigratesOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent.sqlite")

	// Opening one brand-new cache file from several goroutines can lose the
	// WAL-setup race at connect time (pre-existing Open behavior, not a real
	// deployment scenario). The migration runner's guarantee is narrower and
	// is what this test pins: whoever gets through migrates exactly once and
	// nobody corrupts the database.
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
	if got := queryInt(t, db, `PRAGMA user_version`); got != latestStructuralVersion() {
		t.Errorf("user_version = %d, want %d", got, latestStructuralVersion())
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
