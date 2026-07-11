package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/quota"
)

const statuslineFixture = `{
  "model": {"id": "claude-fable-5", "display_name": "Fable 5"},
  "rate_limits": {
    "five_hour": {"used_percentage": 37.4, "resets_at": 1783814796},
    "seven_day": {"used_percentage": 31.0, "resets_at": 1784354593}
  }
}`

func TestRunClaudeStatuslineWritesSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-rate-limits.json")
	var out, errOut bytes.Buffer

	if err := runClaudeStatusline(strings.NewReader(statuslineFixture), &out, &errOut, path); err != nil {
		t.Fatalf("runClaudeStatusline failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "Fable 5 · 5h 37% · wk 31%" {
		t.Errorf("statusline output = %q", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want empty", errOut.String())
	}

	snap, err := quota.ReadClaudeSnapshot(path)
	if err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
	if snap.FiveHour == nil || snap.FiveHour.UsedPercentage != 37.4 || snap.FiveHour.ResetsAt != 1783814796 {
		t.Errorf("five_hour = %+v, want 37.4%% resetting at 1783814796", snap.FiveHour)
	}
	if snap.SevenDay == nil || snap.SevenDay.UsedPercentage != 31.0 {
		t.Errorf("seven_day = %+v, want 31.0%%", snap.SevenDay)
	}
	if snap.Model != "Fable 5" {
		t.Errorf("model = %q, want Fable 5", snap.Model)
	}
	if snap.SavedAtMS == 0 {
		t.Error("saved_at_ms = 0, want timestamp")
	}
}

func TestRunClaudeStatuslineNoRateLimitsWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-rate-limits.json")
	var out, errOut bytes.Buffer

	input := `{"model":{"display_name":"Fable 5"},"cost":{"total_cost_usd":0.42}}`
	if err := runClaudeStatusline(strings.NewReader(input), &out, &errOut, path); err != nil {
		t.Fatalf("runClaudeStatusline failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "Fable 5" {
		t.Errorf("statusline output = %q, want model name only", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("snapshot file exists for input without rate limits (stat err = %v)", err)
	}
}

func TestRunClaudeStatuslineMalformedInputStillPrints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-rate-limits.json")
	var out, errOut bytes.Buffer

	if err := runClaudeStatusline(strings.NewReader("not json"), &out, &errOut, path); err != nil {
		t.Fatalf("runClaudeStatusline returned error for malformed input: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "Claude Code" {
		t.Errorf("statusline output = %q, want fallback line", got)
	}
	if errOut.Len() == 0 {
		t.Error("stderr = empty, want parse diagnostics")
	}
}

func TestInstallClaudeStatusline(t *testing.T) {
	const command = "opencode-dashboard claude-statusline"

	t.Run("creates settings.json when missing", func(t *testing.T) {
		settingsPath := filepath.Join(t.TempDir(), "settings.json")
		var out bytes.Buffer
		if err := installClaudeStatuslineAt(&out, settingsPath, command, false); err != nil {
			t.Fatalf("install failed: %v", err)
		}
		assertStatuslineConfigured(t, settingsPath, command)
	})

	t.Run("preserves existing settings keys", func(t *testing.T) {
		settingsPath := filepath.Join(t.TempDir(), "settings.json")
		existing := `{"model":"opus","theme":"dark"}`
		if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
		var out bytes.Buffer
		if err := installClaudeStatuslineAt(&out, settingsPath, command, false); err != nil {
			t.Fatalf("install failed: %v", err)
		}
		settings := assertStatuslineConfigured(t, settingsPath, command)
		if settings["model"] != "opus" || settings["theme"] != "dark" {
			t.Errorf("existing keys lost: %#v", settings)
		}
	})

	t.Run("refuses to replace a foreign statusLine without force", func(t *testing.T) {
		settingsPath := filepath.Join(t.TempDir(), "settings.json")
		existing := `{"statusLine":{"type":"command","command":"my-custom-statusline"}}`
		if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
		var out bytes.Buffer
		if err := installClaudeStatuslineAt(&out, settingsPath, command, false); err == nil {
			t.Fatal("install succeeded, want refusal without --force")
		}
		if err := installClaudeStatuslineAt(&out, settingsPath, command, true); err != nil {
			t.Fatalf("forced install failed: %v", err)
		}
		assertStatuslineConfigured(t, settingsPath, command)
	})

	t.Run("idempotent when already configured", func(t *testing.T) {
		settingsPath := filepath.Join(t.TempDir(), "settings.json")
		var out bytes.Buffer
		if err := installClaudeStatuslineAt(&out, settingsPath, command, false); err != nil {
			t.Fatalf("first install failed: %v", err)
		}
		out.Reset()
		if err := installClaudeStatuslineAt(&out, settingsPath, command, false); err != nil {
			t.Fatalf("second install failed: %v", err)
		}
		if !strings.Contains(out.String(), "already configured") {
			t.Errorf("second install output = %q, want already-configured notice", out.String())
		}
	})
}

func TestClaudeStatuslineInstalled(t *testing.T) {
	tests := []struct {
		name     string
		settings string // empty => no settings.json written
		want     bool
	}{
		{
			name:     "bare command configured",
			settings: `{"statusLine":{"type":"command","command":"opencode-dashboard claude-statusline"}}`,
			want:     true,
		},
		{
			name:     "absolute path configured",
			settings: `{"statusLine":{"type":"command","command":"/home/user/.local/bin/opencode-dashboard claude-statusline --file /tmp/x.json"}}`,
			want:     true,
		},
		{
			name:     "foreign statusline",
			settings: `{"statusLine":{"type":"command","command":"my-custom-statusline"}}`,
			want:     false,
		},
		{
			name:     "no statusLine key",
			settings: `{"model":"opus"}`,
			want:     false,
		},
		{
			name:     "malformed settings",
			settings: `{not json`,
			want:     false,
		},
		{
			name: "missing settings.json",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeHome := t.TempDir()
			if tt.settings != "" {
				if err := os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte(tt.settings), 0o644); err != nil {
					t.Fatalf("seed settings: %v", err)
				}
			}
			if got := claudeStatuslineInstalled(claudeHome); got != tt.want {
				t.Errorf("claudeStatuslineInstalled = %v, want %v", got, tt.want)
			}
		})
	}
}

func assertStatuslineConfigured(t *testing.T, settingsPath, command string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	entry, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine missing or wrong shape: %#v", settings)
	}
	if entry["type"] != "command" || entry["command"] != command {
		t.Errorf("statusLine = %#v, want command %q", entry, command)
	}
	return settings
}
