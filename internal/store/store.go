package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	busyTimeout = 5000 * time.Millisecond
)

var requiredTables = []string{
	"session",
	"message",
	"project",
	"workspace",
	"part",
}

type SchemaInfo struct {
	HasSession   bool `json:"has_session"`
	HasMessage   bool `json:"has_message"`
	HasProject   bool `json:"has_project"`
	HasWorkspace bool `json:"has_workspace"`
	HasPart      bool `json:"has_part"`
	IsValid      bool `json:"is_valid"`
}

var ErrInvalidSchema = fmt.Errorf("database schema is not valid")

type Store struct {
	db   *sql.DB
	path string

	mu     sync.RWMutex
	schema SchemaInfo
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

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

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

func (s *Store) Schema() SchemaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schema
}

func (s *Store) IsValidSchema() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schema.IsValid
}

func (s *Store) detectSchema(ctx context.Context) (SchemaInfo, error) {
	var info SchemaInfo

	query := `SELECT name FROM sqlite_master WHERE type = 'table' AND name IN (?, ?, ?, ?, ?)`
	rows, err := s.db.QueryContext(ctx, query, requiredTables[0], requiredTables[1], requiredTables[2], requiredTables[3], requiredTables[4])
	if err != nil {
		return info, fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return info, fmt.Errorf("failed to scan table name: %w", err)
		}
		found[name] = true
	}

	if err := rows.Err(); err != nil {
		return info, fmt.Errorf("error iterating schema rows: %w", err)
	}

	info.HasSession = found["session"]
	info.HasMessage = found["message"]
	info.HasProject = found["project"]
	info.HasWorkspace = found["workspace"]
	info.HasPart = found["part"]
	info.IsValid = info.HasSession && info.HasMessage && info.HasProject && info.HasWorkspace && info.HasPart

	return info, nil
}

func (s *Store) RefreshSchema(ctx context.Context) error {
	schema, err := s.detectSchema(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.schema = schema
	s.mu.Unlock()

	return nil
}

func buildDSN(dbPath string) string {
	params := []string{
		"mode=ro",
		"_journal=WAL",
		fmt.Sprintf("_busy_timeout=%d", busyTimeout.Milliseconds()),
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
