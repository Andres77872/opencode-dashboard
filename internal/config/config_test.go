package config

import (
	"path/filepath"
	"testing"
)

func TestSourceDefaultsToOpenCode(t *testing.T) {
	cfg := New()

	if got := cfg.Source(); got != "opencode" {
		t.Errorf("Source() = %q, want opencode", got)
	}
}

func TestWithSourceAcceptsSupportedSourceStrings(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "explicit opencode",
			source: "opencode",
		},
		{
			name:   "explicit claude code",
			source: "claude_code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := New(WithSource(tt.source))

			if got := cfg.Source(); got != tt.source {
				t.Errorf("Source() = %q, want %q", got, tt.source)
			}
		})
	}
}

func TestClaudeHomeResolution(t *testing.T) {
	tests := []struct {
		name       string
		explicit   string
		env        string
		wantPath   func(home string) string
		wantSource string
	}{
		{
			name:     "explicit claude home wins",
			explicit: "/selected/by/flag",
			env:      "/selected/by/env",
			wantPath: func(string) string {
				return "/selected/by/flag"
			},
			wantSource: "--claude-home",
		},
		{
			name: "CLAUDE_CONFIG_DIR is used when flag omitted",
			env:  "/selected/by/env",
			wantPath: func(string) string {
				return "/selected/by/env"
			},
			wantSource: "CLAUDE_CONFIG_DIR",
		},
		{
			name: "HOME dot claude fallback is used last",
			wantPath: func(home string) string {
				return filepath.Join(home, ".claude")
			},
			wantSource: "$HOME/.claude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", tt.env)

			cfg := New(WithClaudeHome(tt.explicit))

			if got := cfg.ClaudeHome(); got != tt.wantPath(home) {
				t.Errorf("ClaudeHome() = %q, want %q", got, tt.wantPath(home))
			}
			if got := cfg.ClaudeHomeSource(); got != tt.wantSource {
				t.Errorf("ClaudeHomeSource() = %q, want %q", got, tt.wantSource)
			}
		})
	}
}

func TestOpenCodePathControlsRemainOpenCodeOnly(t *testing.T) {
	t.Run("db flag selects only OpenCode database", func(t *testing.T) {
		home := t.TempDir()
		dbPath := filepath.Join(home, "opencode.db")
		t.Setenv("HOME", home)
		t.Setenv("CLAUDE_CONFIG_DIR", "")

		cfg := New(WithDBPath(dbPath))

		if got := cfg.DBPath(); got != dbPath {
			t.Errorf("DBPath() = %q, want explicit OpenCode DB %q", got, dbPath)
		}
		if got := cfg.ClaudeHome(); got == dbPath {
			t.Errorf("ClaudeHome() = %q, want it not to reuse --db", got)
		}
		if got := cfg.ClaudeHome(); got != filepath.Join(home, ".claude") {
			t.Errorf("ClaudeHome() = %q, want HOME fallback %q", got, filepath.Join(home, ".claude"))
		}
	})

	t.Run("channel selects only OpenCode database", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		t.Setenv("CLAUDE_CONFIG_DIR", "")

		cfg := New(WithChannel("beta"))
		wantDB := filepath.Join(home, ".local", "share", AppName, BetaChannelDBName)

		if got := cfg.DBPath(); got != wantDB {
			t.Errorf("DBPath() = %q, want beta channel OpenCode DB %q", got, wantDB)
		}
		if got := cfg.ClaudeHome(); got != filepath.Join(home, ".claude") {
			t.Errorf("ClaudeHome() = %q, want HOME fallback %q", got, filepath.Join(home, ".claude"))
		}
	})

	t.Run("OPENCODE_DASHBOARD_DB selects only OpenCode database", func(t *testing.T) {
		home := t.TempDir()
		dbPath := filepath.Join(home, "from-env.db")
		t.Setenv("HOME", home)
		t.Setenv(EnvDBPath, dbPath)
		t.Setenv("CLAUDE_CONFIG_DIR", "")

		cfg := New()

		if got := cfg.DBPath(); got != dbPath {
			t.Errorf("DBPath() = %q, want env OpenCode DB %q", got, dbPath)
		}
		if got := cfg.ClaudeHome(); got == dbPath {
			t.Errorf("ClaudeHome() = %q, want it not to reuse %s", got, EnvDBPath)
		}
		if got := cfg.ClaudeHome(); got != filepath.Join(home, ".claude") {
			t.Errorf("ClaudeHome() = %q, want HOME fallback %q", got, filepath.Join(home, ".claude"))
		}
	})
}
