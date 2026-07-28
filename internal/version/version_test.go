package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Version and GitCommit are package-level vars set by ldflags, so each test
// restores them rather than leaving a value behind for the next one.
func setBuildVars(t *testing.T, version, commit string) {
	t.Helper()
	originalVersion, originalCommit := Version, GitCommit
	t.Cleanup(func() { Version, GitCommit = originalVersion, originalCommit })
	Version, GitCommit = version, commit
}

func TestShortCommitTruncatesOnlyFullLengthHashes(t *testing.T) {
	cases := []struct {
		name   string
		commit string
		want   string
	}{
		{"full hash is truncated to seven", "abc1234def5678", "abc1234"},
		{"exactly seven is unchanged", "abc1234", "abc1234"},
		{"short hash is passed through rather than panicking", "abc12", "abc12"},
		{"unset sentinel is preserved", "unknown", "unknown"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setBuildVars(t, "v1.2.3", tc.commit)
			if got := ShortCommit(); got != tc.want {
				t.Errorf("ShortCommit() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildInfoAndUserAgentUseTheInjectedValues(t *testing.T) {
	setBuildVars(t, "v1.2.3", "abc1234def5678")
	if got, want := BuildInfo(), "v1.2.3 (abc1234)"; got != want {
		t.Errorf("BuildInfo() = %q, want %q", got, want)
	}
	if got, want := UserAgent(), "opencode-dashboard/v1.2.3"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

func TestDefaultsAreDevelopmentPlaceholders(t *testing.T) {
	// An unstamped build must still produce usable strings rather than empty
	// ones, because UserAgent goes out on update checks.
	setBuildVars(t, "dev", "unknown")
	if got, want := BuildInfo(), "dev (unknown)"; got != want {
		t.Errorf("BuildInfo() = %q, want %q", got, want)
	}
	if got, want := UserAgent(), "opencode-dashboard/dev"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

// The release ldflags address these variables by their fully qualified import
// path. A rename here compiles fine but silently ships an unstamped binary, so
// the release config is checked against the names it targets.
func TestReleaseLdflagsTargetTheseVariables(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Skipf("release config unavailable: %v", err)
	}
	for _, symbol := range []string{
		"opencode-dashboard/internal/version.Version",
		"opencode-dashboard/internal/version.GitCommit",
	} {
		if !strings.Contains(string(config), symbol) {
			t.Errorf(".goreleaser.yaml no longer stamps %s; releases would ship as %q", symbol, "dev")
		}
	}
}
