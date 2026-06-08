package claudecode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestClaudeCodeDiscoveryAndAvailability(t *testing.T) {
	tests := []struct {
		name                string
		fixture             string
		wantAvailable       bool
		wantOverviewErr     error
		wantScannedFiles    int64
		wantMessages        int64
		wantReasonSubstring string
	}{
		{
			name:             "valid home discovers project JSONL transcripts recursively",
			fixture:          "valid_home",
			wantAvailable:    true,
			wantScannedFiles: 7,
			wantMessages:     14,
		},
		{
			name:             "empty projects root is available with empty Claude data",
			fixture:          "no_transcripts_home",
			wantAvailable:    true,
			wantScannedFiles: 0,
			wantMessages:     0,
		},
		{
			name:                "missing projects root is an unavailable Claude source",
			fixture:             "missing_projects_home",
			wantAvailable:       false,
			wantOverviewErr:     source.ErrUnavailableSource,
			wantReasonSubstring: "projects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newFixtureSource(t, tt.fixture)
			ctx := testContext(t)

			info := src.Info(ctx)
			if info.ID != source.SourceClaudeCode {
				t.Errorf("Info().ID = %q, want %q", info.ID, source.SourceClaudeCode)
			}
			if info.Kind != "jsonl" {
				t.Errorf("Info().Kind = %q, want jsonl", info.Kind)
			}
			if info.Available != tt.wantAvailable {
				t.Errorf("Info().Available = %v, want %v", info.Available, tt.wantAvailable)
			}
			if info.PathSource != "test fixture" {
				t.Errorf("Info().PathSource = %q, want test fixture", info.PathSource)
			}
			if !info.ReadOnly || !info.LocalOnly {
				t.Errorf("Info() read/local flags = %v/%v, want true/true", info.ReadOnly, info.LocalOnly)
			}
			if !hasString(info.Capabilities, "messages") || !hasString(info.Capabilities, "config") {
				t.Errorf("Info().Capabilities = %#v, want messages and config", info.Capabilities)
			}

			overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
			if tt.wantOverviewErr != nil {
				if !errors.Is(err, tt.wantOverviewErr) {
					t.Fatalf("Overview() error = %v, want errors.Is(..., %v)", err, tt.wantOverviewErr)
				}
				info = src.Info(ctx)
				if !strings.Contains(strings.ToLower(info.Diagnostics.Reason), tt.wantReasonSubstring) {
					t.Errorf("Info().Diagnostics.Reason = %q, want containing %q", info.Diagnostics.Reason, tt.wantReasonSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("Overview() unexpected error: %v", err)
			}
			assertAllSourceID(t, overview.SourceID)
			if overview.Messages != tt.wantMessages {
				t.Errorf("Overview().Messages = %d, want %d per-request message rows", overview.Messages, tt.wantMessages)
			}

			info = src.Info(ctx)
			if info.Diagnostics.ScannedFiles != tt.wantScannedFiles {
				t.Errorf("Info().Diagnostics.ScannedFiles = %d, want %d", info.Diagnostics.ScannedFiles, tt.wantScannedFiles)
			}
		})
	}
}

func TestDiscoveryIncludesSubagentsButSkipsToolResultsAndDebug(t *testing.T) {
	discovered := discoverTranscripts(testContext(t), fixturePath(t, "safe_shape_home"))
	if !discovered.available {
		t.Fatalf("discoverTranscripts(safe_shape_home).available = false, diagnostics: %#v", discovered.diagnostics)
	}
	// Subagent transcripts are real API requests and ARE discovered; only the
	// tool-results/ and debug/ support directories stay skipped. So we expect the
	// top-level session plus the subagent transcript.
	if discovered.diagnostics.ScannedFiles != 2 {
		t.Errorf("ScannedFiles = %d, want 2 (top-level session + subagent)", discovered.diagnostics.ScannedFiles)
	}
	if len(discovered.files) != 2 {
		t.Fatalf("discovered files len = %d, want 2 (top-level session + subagent): %#v", len(discovered.files), discovered.files)
	}

	var topLevel, subagent *transcriptFile
	for i := range discovered.files {
		file := &discovered.files[i]
		path := filepath.ToSlash(file.Path)
		if file.ProjectID != "-redacted-safe" {
			t.Errorf("ProjectID = %q, want -redacted-safe", file.ProjectID)
		}
		// Subagent transcripts carry the parent session id via sessionIDFromParts.
		if file.SessionID != "safe-shape-session" {
			t.Errorf("SessionID = %q, want safe-shape-session for %q", file.SessionID, path)
		}
		if strings.Contains(path, "/subagents/") {
			subagent = file
		} else {
			topLevel = file
		}
		// tool-results/ and debug/ support directories must never be discovered.
		for _, skipped := range []string{"/tool-results/", "/debug/"} {
			if strings.Contains(path, skipped) {
				t.Errorf("discovered nested support transcript %q in path %q; tool-results/ and debug/ must be skipped", skipped, path)
			}
		}
	}

	if topLevel == nil {
		t.Fatalf("top-level safe-shape-session.jsonl not discovered: %#v", discovered.files)
	}
	if filepath.Base(topLevel.Path) != "safe-shape-session.jsonl" {
		t.Errorf("top-level file = %q, want safe-shape-session.jsonl", topLevel.Path)
	}
	if subagent == nil {
		t.Fatalf("subagent transcript under subagents/ not discovered: %#v", discovered.files)
	}
	if filepath.Base(subagent.Path) != "skipped-agent.jsonl" {
		t.Errorf("subagent file = %q, want skipped-agent.jsonl under subagents/", subagent.Path)
	}
}

func TestDiscoveryReportsUnreadableProjectsRootWhenPermissionsApply(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can traverse unreadable directories; permission assertion is not meaningful")
	}
	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	if err := os.Mkdir(projects, 0o700); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
	if err := os.Chmod(projects, 0); err != nil {
		t.Fatalf("chmod projects unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(projects, 0o700) })

	discovered := discoverTranscripts(testContext(t), home)
	if discovered.available {
		t.Skip("filesystem allowed traversal despite chmod 000; permission assertion is not meaningful here")
	}
	if discovered.diagnostics.Status != "unavailable" {
		t.Errorf("diagnostics status = %q, want unavailable", discovered.diagnostics.Status)
	}
	if !strings.Contains(strings.ToLower(discovered.diagnostics.Reason), "read") && !strings.Contains(strings.ToLower(discovered.diagnostics.Reason), "permission") {
		t.Errorf("diagnostics reason = %q, want read/permission reason", discovered.diagnostics.Reason)
	}
}

func TestParserTreatsDisappearedTranscriptAsMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	_, _, err := parseTranscriptFile(testContext(t), transcriptFile{Path: missing, ProjectID: "-tmp-project", SessionID: "gone"})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parseTranscriptFile missing error = %v, want os.ErrNotExist", err)
	}
}
