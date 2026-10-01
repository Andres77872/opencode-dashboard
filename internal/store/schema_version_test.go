package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"opencode-dashboard/internal/store/fixture"
)

func connectFixture(t *testing.T, build func(context.Context) (string, error)) *Store {
	t.Helper()
	ctx := context.Background()
	path, err := build(ctx)
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	st, err := Connect(ctx, path)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// legacyRows seeds one imported and one not-yet-imported 1.x session.
func legacyRows(b *fixture.V2Builder) *fixture.V2Builder {
	return b.
		Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated) VALUES ('ses_old', 'p', 'old', '/w', '1.18.34', 1, 1)`).
		Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated) VALUES ('ses_pending', 'p', 'pending', '/w', '1.18.34', 2, 2)`)
}

func upgradedBuilder() *fixture.V2Builder {
	return fixture.NewV2Builder().
		WithLegacyTables().
		AddProject(fixture.NewProject("p", "/w")).
		AddSession(fixture.V2Session{ID: "ses_old", ProjectID: "p", Created: time.UnixMilli(1)})
}

func TestDetectSchemaVersion(t *testing.T) {
	tests := []struct {
		name        string
		build       func(context.Context) (string, error)
		wantVersion SchemaVersion
		wantImport  LegacyImportState
		wantLegacy  [2]int64 // imported, legacy
	}{
		{
			name:        "OpenCode 1.x",
			build:       fixture.SampleFixture,
			wantVersion: SchemaV1,
		},
		{
			name: "OpenCode 1.18 with its unused session_message table",
			build: func(ctx context.Context) (string, error) {
				path, err := fixture.SampleFixture(ctx)
				if err != nil {
					return "", err
				}
				db, err := sql.Open("sqlite", path)
				if err != nil {
					return "", err
				}
				defer db.Close()
				_, err = db.ExecContext(ctx, `CREATE TABLE session_message (id text PRIMARY KEY, session_id text NOT NULL, type text NOT NULL, seq integer NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`)
				return path, err
			},
			wantVersion: SchemaV1,
		},
		{
			name:        "fresh OpenCode 2 install",
			build:       fixture.NewV2Builder().Build,
			wantVersion: SchemaV2,
		},
		{
			name:        "upgraded database whose import completed",
			build:       legacyRows(upgradedBuilder().SetKV(legacyImportKey, `{"phase":"completed"}`)).Build,
			wantVersion: SchemaV2,
			wantImport:  LegacyImportCompleted,
		},
		{
			name:        "upgraded database mid-import keeps reading 1.x",
			build:       legacyRows(upgradedBuilder().SetKV(legacyImportKey, `{"phase":"sessions","cursor":"ses_old"}`)).Build,
			wantVersion: SchemaV1,
			wantImport:  LegacyImportRunning,
			wantLegacy:  [2]int64{1, 2},
		},
		{
			name:        "upgraded database before the import started keeps reading 1.x",
			build:       legacyRows(upgradedBuilder()).Build,
			wantVersion: SchemaV1,
			wantImport:  LegacyImportPending,
			wantLegacy:  [2]int64{1, 2},
		},
		{
			name: "unrecorded import whose sessions are all present reads 2.x",
			build: upgradedBuilder().
				Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated) VALUES ('ses_old', 'p', 'old', '/w', '1.18.34', 1, 1)`).
				Build,
			wantVersion: SchemaV2,
			wantImport:  LegacyImportPending,
			wantLegacy:  [2]int64{1, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := connectFixture(t, tt.build)
			schema := st.Schema()
			if schema.Version != tt.wantVersion || !schema.IsValid {
				t.Fatalf("Version = %v (valid %v), want %v", schema.Version, schema.IsValid, tt.wantVersion)
			}
			if schema.LegacyImport != tt.wantImport {
				t.Errorf("LegacyImport = %q, want %q", schema.LegacyImport, tt.wantImport)
			}
			if got := [2]int64{schema.ImportedSessions, schema.LegacySessions}; got != tt.wantLegacy {
				t.Errorf("imported/legacy sessions = %v, want %v", got, tt.wantLegacy)
			}
			if want := tt.wantImport == LegacyImportRunning || tt.wantImport == LegacyImportPending && tt.wantLegacy[0] < tt.wantLegacy[1]; schema.ImportIncomplete() != want {
				t.Errorf("ImportIncomplete() = %v, want %v", schema.ImportIncomplete(), want)
			}
		})
	}
}

func TestMissingTablesNamesClosestLayout(t *testing.T) {
	tests := []struct {
		name  string
		found map[string]bool
		want  []string
	}{
		{
			name:  "empty database is judged as 1.x",
			found: map[string]bool{},
			want:  []string{"session", "message", "project", "workspace", "part"},
		},
		{
			name:  "1.18 session_message alone is not a 2.x marker",
			found: map[string]bool{"session_message": true, "project": true, "workspace": true},
			want:  []string{"session", "message", "part"},
		},
		{
			name:  "partial 2.x database",
			found: map[string]bool{"session_v2": true, "project": true, "workspace": true},
			want:  []string{"session_message"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := classifySchema(tt.found)
			if info.IsValid {
				t.Fatalf("classifySchema(%v) is valid", tt.found)
			}
			if got := info.MissingTables(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MissingTables() = %v, want %v", got, tt.want)
			}
		})
	}
}

// OpenCode upgrades a database in place while the dashboard keeps it open, so
// a stale detection must be re-checked instead of trusted forever.
func TestSchemaRedetectsInPlaceUpgrade(t *testing.T) {
	ctx := context.Background()
	path, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	st, err := Connect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if got := st.Version(); got != SchemaV1 {
		t.Fatalf("Version() = %v, want v1", got)
	}

	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, ddl := range []string{
		`CREATE TABLE session_v2 (id text PRIMARY KEY, project_id text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL)`,
		`CREATE TABLE session_message (id text PRIMARY KEY, session_id text NOT NULL, type text NOT NULL, seq integer NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`,
		`CREATE TABLE kv (key text PRIMARY KEY, value text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL)`,
		`INSERT INTO kv VALUES ('migration.v1-v2', '{"phase":"completed"}', 0, 0)`,
	} {
		if _, err := writer.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}

	if got := st.Version(); got != SchemaV1 {
		t.Fatalf("Version() before the recheck interval = %v, want the cached v1", got)
	}
	st.mu.Lock()
	st.checkedAt = time.Now().Add(-schemaRecheckInterval)
	st.mu.Unlock()
	if got := st.Version(); got != SchemaV2 {
		t.Fatalf("Version() after the recheck interval = %v, want v2", got)
	}
}
