package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	busyTimeout        = 5000 * time.Millisecond
	maxReadConnections = 4
)

// SchemaVersion identifies which OpenCode storage layout a database uses.
type SchemaVersion int

const (
	// SchemaUnknown is a database that matches neither supported layout.
	SchemaUnknown SchemaVersion = 0
	// SchemaV1 is OpenCode 1.x: session, message (one row per user/assistant
	// message with JSON metadata) and part (text, tool, step-finish, ...).
	SchemaV1 SchemaVersion = 1
	// SchemaV2 is OpenCode 2.x: session_v2 and session_message, where each
	// row is one projected transcript item (user prompt, assistant step,
	// compaction, ...) and assistant content (text, reasoning, tool calls)
	// is embedded in session_message.data.
	SchemaV2 SchemaVersion = 2
)

func (v SchemaVersion) String() string {
	switch v {
	case SchemaV1:
		return "v1"
	case SchemaV2:
		return "v2"
	default:
		return "unknown"
	}
}

// LegacyImportState describes OpenCode 2's one-time import of the 1.x tables
// that an upgraded database still carries. OpenCode 2 runs the import in the
// background of its server and leaves the 1.x tables in place afterwards, so
// they are a frozen pre-upgrade snapshot that must never be read once the 2.x
// tables are complete. Until then the 1.x tables are the complete history and
// are read instead (see SchemaInfo.ImportIncomplete).
type LegacyImportState string

const (
	// LegacyImportNone means the database has no 1.x tables to import.
	LegacyImportNone LegacyImportState = ""
	// LegacyImportPending means 1.x tables exist and OpenCode has not started
	// (or not recorded) the import yet.
	LegacyImportPending LegacyImportState = "pending"
	// LegacyImportRunning means the import started but has not completed;
	// older sessions are still missing from session_v2.
	LegacyImportRunning LegacyImportState = "running"
	// LegacyImportCompleted means every 1.x session was imported.
	LegacyImportCompleted LegacyImportState = "completed"
)

// legacyImportKey is OpenCode 2's kv key for the v1->v2 import progress
// (packages/core/src/database/v1-migration.bun.ts, MIGRATION_STATE_KEY).
const legacyImportKey = "migration.v1-v2"

var v1RequiredTables = []string{"session", "message", "project", "workspace", "part"}

var v2RequiredTables = []string{"session_v2", "session_message", "project"}

// schemaTables is every table detectSchema looks for.
var schemaTables = []string{"session", "message", "project", "workspace", "part", "session_v2", "session_message", "kv"}

// schemaRecheckInterval bounds how long a detected layout is trusted. OpenCode
// upgrades the database in place (1.x -> 2.x migrations and the background
// history import), so a long-running dashboard must notice the switch without
// a restart.
const schemaRecheckInterval = 15 * time.Second

type SchemaInfo struct {
	// Version is the layout queries must use. It is SchemaUnknown when the
	// required tables of neither layout are present.
	Version SchemaVersion `json:"version"`

	HasSession   bool `json:"has_session"`
	HasMessage   bool `json:"has_message"`
	HasProject   bool `json:"has_project"`
	HasWorkspace bool `json:"has_workspace"`
	HasPart      bool `json:"has_part"`

	HasSessionV2      bool `json:"has_session_v2"`
	HasSessionMessage bool `json:"has_session_message"`

	// LegacyImport is only set for databases with 2.x tables that still carry
	// 1.x tables. LegacySessions/ImportedSessions report the import progress
	// (both are left zero once the import completed).
	LegacyImport     LegacyImportState `json:"legacy_import,omitempty"`
	LegacySessions   int64             `json:"legacy_sessions,omitempty"`
	ImportedSessions int64             `json:"imported_sessions,omitempty"`

	IsValid bool `json:"is_valid"`
}

// MissingTables lists the tables the closest supported layout still needs.
// A database with session_v2 is judged against the 2.x layout; everything else
// against 1.x. session_message alone is no 2.x marker: OpenCode 1.18 already
// creates it (empty) next to its 1.x tables.
func (s SchemaInfo) MissingTables() []string {
	present := map[string]bool{
		"session":         s.HasSession,
		"message":         s.HasMessage,
		"project":         s.HasProject,
		"workspace":       s.HasWorkspace,
		"part":            s.HasPart,
		"session_v2":      s.HasSessionV2,
		"session_message": s.HasSessionMessage,
	}
	required := v1RequiredTables
	if s.HasSessionV2 {
		required = v2RequiredTables
	}
	missing := make([]string, 0, len(required))
	for _, table := range required {
		if !present[table] {
			missing = append(missing, table)
		}
	}
	return missing
}

// ImportIncomplete reports whether OpenCode 2 has not yet copied every 1.x
// session of an upgraded database into its 2.x tables.
func (s SchemaInfo) ImportIncomplete() bool {
	switch s.LegacyImport {
	case LegacyImportPending, LegacyImportRunning:
		return s.ImportedSessions < s.LegacySessions
	default:
		return false
	}
}

func hasV1Tables(info SchemaInfo) bool {
	return info.HasSession && info.HasMessage && info.HasProject && info.HasWorkspace && info.HasPart
}

func classifySchema(found map[string]bool) SchemaInfo {
	info := SchemaInfo{
		HasSession:        found["session"],
		HasMessage:        found["message"],
		HasProject:        found["project"],
		HasWorkspace:      found["workspace"],
		HasPart:           found["part"],
		HasSessionV2:      found["session_v2"],
		HasSessionMessage: found["session_message"],
	}
	switch {
	// The 2.x tables win whenever they exist: an upgraded database keeps its
	// 1.x tables untouched, and OpenCode 2 only reads and writes session_v2.
	// session_v2 is the marker; OpenCode 1.18 already has an unused
	// session_message table.
	case info.HasSessionV2 && info.HasSessionMessage && info.HasProject:
		info.Version = SchemaV2
	case hasV1Tables(info):
		info.Version = SchemaV1
	}
	info.IsValid = info.Version != SchemaUnknown
	return info
}

var ErrInvalidSchema = fmt.Errorf("database schema is not valid")

type Store struct {
	db   *sql.DB
	path string

	mu        sync.RWMutex
	schema    SchemaInfo
	checkedAt time.Time
	// refreshMu serializes re-detection so concurrent readers of a stale
	// schema trigger one sqlite_master scan instead of one each.
	refreshMu sync.Mutex
}

func Connect(ctx context.Context, dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path is required")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("database not found: %s", dbPath)
	} else if err != nil {
		return nil, fmt.Errorf("failed to access database: %w", err)
	}

	dsn := buildDSN(dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Overview pages request several independent aggregates at once. OpenCode's
	// database is opened read-only and normally runs in WAL mode, so allowing a
	// small reader pool avoids serializing every card behind one long scan.
	db.SetMaxOpenConns(maxReadConnections)
	db.SetMaxIdleConns(maxReadConnections)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := setPragmas(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set pragmas: %w", err)
	}

	store := &Store{
		db:   db,
		path: dbPath,
	}

	schema, err := store.detectSchema(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to detect schema: %w", err)
	}
	store.schema = schema
	store.checkedAt = time.Now()

	return store, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) DB() *sql.DB {
	return s.db
}

// Schema returns the detected layout, re-detecting it when the last check is
// older than schemaRecheckInterval. A failed re-detection keeps the previous
// result: a transient lock must not flip a working source to invalid.
func (s *Store) Schema() SchemaInfo {
	s.mu.RLock()
	schema, checkedAt := s.schema, s.checkedAt
	s.mu.RUnlock()
	if s.db == nil || time.Since(checkedAt) < schemaRecheckInterval {
		return schema
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.RLock()
	schema, checkedAt = s.schema, s.checkedAt
	s.mu.RUnlock()
	if time.Since(checkedAt) < schemaRecheckInterval {
		return schema
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	next, err := s.detectSchema(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkedAt = time.Now()
	if err == nil {
		s.schema = next
	}
	return s.schema
}

// Version is shorthand for Schema().Version.
func (s *Store) Version() SchemaVersion {
	return s.Schema().Version
}

func (s *Store) IsValidSchema() bool {
	return s.Schema().IsValid
}

func (s *Store) detectSchema(ctx context.Context) (SchemaInfo, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(schemaTables)), ", ")
	args := make([]any, len(schemaTables))
	for i, table := range schemaTables {
		args[i] = table
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name IN (`+placeholders+`)`, args...)
	if err != nil {
		return SchemaInfo{}, fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return SchemaInfo{}, fmt.Errorf("failed to scan table name: %w", err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return SchemaInfo{}, fmt.Errorf("error iterating schema rows: %w", err)
	}

	info := classifySchema(found)
	if info.Version == SchemaV2 && info.HasSession {
		if err := s.detectLegacyImport(ctx, &info, found["kv"]); err != nil {
			return SchemaInfo{}, err
		}
		// OpenCode 2 imports 1.x history in the background after upgrading.
		// Until it finishes, the 2.x tables lack older sessions while the
		// untouched 1.x tables still hold all of them, so keep reading 1.x:
		// consolidating partial history would hide it from the cache.
		if info.ImportIncomplete() && hasV1Tables(info) {
			info.Version = SchemaV1
		}
	}
	return info, nil
}

// detectLegacyImport reports how far OpenCode 2 got importing the 1.x tables
// of an upgraded database. The kv state is authoritative for completion; the
// session counts only describe progress.
func (s *Store) detectLegacyImport(ctx context.Context, info *SchemaInfo, hasKV bool) error {
	info.LegacyImport = LegacyImportPending
	if hasKV {
		var phase sql.NullString
		err := s.db.QueryRowContext(ctx, `SELECT json_extract(value, '$.phase') FROM kv WHERE key = ?`, legacyImportKey).Scan(&phase)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("failed to read OpenCode import state: %w", err)
		case phase.String == "completed":
			info.LegacyImport = LegacyImportCompleted
		case phase.Valid:
			info.LegacyImport = LegacyImportRunning
		}
	}
	if info.LegacyImport == LegacyImportCompleted {
		return nil
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN EXISTS (SELECT 1 FROM session_v2 v WHERE v.id = legacy.id) THEN 1 ELSE 0 END), 0)
		FROM session legacy
	`).Scan(&info.LegacySessions, &info.ImportedSessions)
	if err != nil {
		return fmt.Errorf("failed to count OpenCode sessions awaiting import: %w", err)
	}
	return nil
}

func (s *Store) RefreshSchema(ctx context.Context) error {
	schema, err := s.detectSchema(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.schema = schema
	s.checkedAt = time.Now()
	s.mu.Unlock()

	return nil
}

func buildDSN(dbPath string) string {
	params := []string{
		"mode=ro",
		"_journal=WAL",
		fmt.Sprintf("_busy_timeout=%d", busyTimeout.Milliseconds()),
		fmt.Sprintf("_pragma=busy_timeout(%d)", busyTimeout.Milliseconds()),
		"_pragma=query_only(1)",
		"_pragma=temp_store(MEMORY)",
		"_pragma=cache_size(-16384)",
		"_pragma=mmap_size(268435456)",
		"_txlock=immediate",
	}

	return dbPath + "?" + strings.Join(params, "&")
}

func setPragmas(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}

	return nil
}
