package store

import (
	"strings"
	"testing"
)

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name         string
		dbPath       string
		wantContains []string
	}{
		{
			name:   "simple path",
			dbPath: "/path/to/db.sqlite",
			wantContains: []string{
				"/path/to/db.sqlite?",
				"mode=ro",
				"_journal=WAL",
				"_busy_timeout=5000",
				"_txlock=immediate",
			},
		},
		{
			name:   "windows style path",
			dbPath: "C:\\Users\\test\\db.sqlite",
			wantContains: []string{
				"C:\\Users\\test\\db.sqlite?",
				"mode=ro",
			},
		},
		{
			name:   "relative path",
			dbPath: "./data/opencode.db",
			wantContains: []string{
				"./data/opencode.db?",
				"mode=ro",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDSN(tt.dbPath)

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("buildDSN(%q) = %q, want to contain %q", tt.dbPath, result, want)
				}
			}
		})
	}
}

func TestBuildDSNReadOnly(t *testing.T) {
	// Critical: DSN must always be read-only for safety
	result := buildDSN("/path/to/db.sqlite")

	if !strings.Contains(result, "mode=ro") {
		t.Errorf("buildDSN() must include mode=ro for read-only safety, got: %q", result)
	}

	// Ensure no write mode is present
	if strings.Contains(result, "mode=rw") || strings.Contains(result, "mode=w") {
		t.Errorf("SECURITY ISSUE: buildDSN() includes write mode, should be read-only: %q", result)
	}
}

func TestBuildDSNJournalMode(t *testing.T) {
	result := buildDSN("/path/to/db.sqlite")

	// WAL mode is expected for concurrent reads
	if !strings.Contains(result, "_journal=WAL") {
		t.Errorf("buildDSN() should use WAL journal mode for concurrency, got: %q", result)
	}
}

func TestBuildDSNBusyTimeout(t *testing.T) {
	result := buildDSN("/path/to/db.sqlite")

	// Busy timeout should be set to handle concurrent access
	if !strings.Contains(result, "_busy_timeout=") {
		t.Errorf("buildDSN() should include busy_timeout, got: %q", result)
	}

	// Extract and verify the timeout value
	if !strings.Contains(result, "_busy_timeout=5000") {
		t.Errorf("buildDSN() busy_timeout should be 5000ms, got: %q", result)
	}
}

func TestSchemaInfoIsValid(t *testing.T) {
	tests := []struct {
		name     string
		info     SchemaInfo
		expected bool
	}{
		{
			name:     "all tables present",
			info:     SchemaInfo{HasSession: true, HasMessage: true, HasProject: true, HasWorkspace: true, HasPart: true, IsValid: true},
			expected: true,
		},
		{
			name:     "missing session",
			info:     SchemaInfo{HasSession: false, HasMessage: true, HasProject: true, HasWorkspace: true, HasPart: true, IsValid: false},
			expected: false,
		},
		{
			name:     "missing message",
			info:     SchemaInfo{HasSession: true, HasMessage: false, HasProject: true, HasWorkspace: true, HasPart: true, IsValid: false},
			expected: false,
		},
		{
			name:     "missing project",
			info:     SchemaInfo{HasSession: true, HasMessage: true, HasProject: false, HasWorkspace: true, HasPart: true, IsValid: false},
			expected: false,
		},
		{
			name:     "missing workspace",
			info:     SchemaInfo{HasSession: true, HasMessage: true, HasProject: true, HasWorkspace: false, HasPart: true, IsValid: false},
			expected: false,
		},
		{
			name:     "missing part",
			info:     SchemaInfo{HasSession: true, HasMessage: true, HasProject: true, HasWorkspace: true, HasPart: false, IsValid: false},
			expected: false,
		},
		{
			name:     "all missing",
			info:     SchemaInfo{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.info.IsValid != tt.expected {
				t.Errorf("SchemaInfo.IsValid = %v, want %v for %v", tt.info.IsValid, tt.expected, tt.info)
			}
		})
	}
}
