package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/store/fixture"
)

func TestConnectWithFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build sample fixture
	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("Failed to create fixture database: %v", err)
	}

	// Cleanup the temp directory (fixture creates a temp dir containing the db)
	tmpDir := filepath.Dir(dbPath)
	defer os.RemoveAll(tmpDir)

	// Connect to the fixture database
	store, err := Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer store.Close()

	// Verify path is stored
	if store.Path() != dbPath {
		t.Errorf("Store.Path() = %q, want %q", store.Path(), dbPath)
	}

	// Verify schema is detected as valid
	schema := store.Schema()
	if !schema.IsValid {
		t.Errorf("Schema.IsValid = false, want true. Schema: %+v", schema)
	}

	// Verify individual tables are detected
	if !schema.HasSession {
		t.Error("Schema.HasSession = false, want true")
	}
	if !schema.HasMessage {
		t.Error("Schema.HasMessage = false, want true")
	}
	if !schema.HasProject {
		t.Error("Schema.HasProject = false, want true")
	}
	if !schema.HasWorkspace {
		t.Error("Schema.HasWorkspace = false, want true")
	}
	if !schema.HasPart {
		t.Error("Schema.HasPart = false, want true")
	}

	// Verify IsValidSchema shortcut
	if !store.IsValidSchema() {
		t.Error("IsValidSchema() = false, want true")
	}
}

func TestConnectMissingDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to connect to non-existent database
	_, err := Connect(ctx, "/nonexistent/path/to/db.sqlite")
	if err == nil {
		t.Error("Connect() with missing database should return error")
	}

	// Error should mention database not found
	if !containsSubstring(err.Error(), "database not found") {
		t.Errorf("Connect() error = %q, want error containing 'database not found'", err.Error())
	}
}

func TestConnectEmptyPath(t *testing.T) {
	ctx := context.Background()

	_, err := Connect(ctx, "")
	if err == nil {
		t.Error("Connect() with empty path should return error")
	}

	if !containsSubstring(err.Error(), "database path is required") {
		t.Errorf("Connect() error = %q, want error containing 'database path is required'", err.Error())
	}
}

func TestSchemaRefresh(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("Failed to create fixture database: %v", err)
	}
	defer os.Remove(dbPath)
	defer os.RemoveAll(filepathWithoutExt(dbPath))

	store, err := Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer store.Close()

	// Refresh schema (should still be valid)
	if err := store.RefreshSchema(ctx); err != nil {
		t.Errorf("RefreshSchema() failed: %v", err)
	}

	if !store.IsValidSchema() {
		t.Error("IsValidSchema() = false after refresh, want true")
	}
}

func filepathWithoutExt(path string) string {
	// Returns the directory containing the db file for cleanup
	// (fixture creates temp dir with db inside)
	dir := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			dir = path[:i+1]
			break
		}
	}
	// Go up one more level if the dir looks like a temp pattern
	if dir != "" {
		parent := ""
		for i := len(dir) - 2; i >= 0; i-- {
			if dir[i] == '/' {
				parent = dir[:i+1]
				break
			}
		}
		if parent != "" && containsSubstring(dir, "opencode-fixture") {
			return parent
		}
	}
	return dir
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
