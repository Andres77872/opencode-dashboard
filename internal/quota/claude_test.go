package quota

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const statuslineBothWindows = `{
  "model": {"id": "claude-fable-5", "display_name": "Fable 5"},
  "workspace": {"current_dir": "/tmp"},
  "rate_limits": {
    "five_hour": {"used_percentage": 37.4, "resets_at": 1783814796},
    "seven_day": {"used_percentage": 31.0, "resets_at": 1784354593}
  }
}`

func TestParseStatuslineInput(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantErr      bool
		wantModel    string
		wantFiveHour *ClaudeWindow
		wantSevenDay *ClaudeWindow
	}{
		{
			name:         "both windows numeric resets",
			input:        statuslineBothWindows,
			wantModel:    "Fable 5",
			wantFiveHour: &ClaudeWindow{UsedPercentage: 37.4, ResetsAt: 1783814796},
			wantSevenDay: &ClaudeWindow{UsedPercentage: 31.0, ResetsAt: 1784354593},
		},
		{
			name:         "five hour only",
			input:        `{"model":{"display_name":"Fable 5"},"rate_limits":{"five_hour":{"used_percentage":12}}}`,
			wantModel:    "Fable 5",
			wantFiveHour: &ClaudeWindow{UsedPercentage: 12},
		},
		{
			name:         "resets_at as RFC3339 string",
			input:        `{"rate_limits":{"five_hour":{"used_percentage":50,"resets_at":"2026-07-11T22:06:36Z"}}}`,
			wantFiveHour: &ClaudeWindow{UsedPercentage: 50, ResetsAt: 1783807596},
		},
		{
			name:         "resets_at in epoch milliseconds",
			input:        `{"rate_limits":{"five_hour":{"used_percentage":50,"resets_at":1783814796000}}}`,
			wantFiveHour: &ClaudeWindow{UsedPercentage: 50, ResetsAt: 1783814796},
		},
		{
			name:      "no rate limits (API key session)",
			input:     `{"model":{"display_name":"Fable 5"},"cost":{"total_cost_usd":0.1}}`,
			wantModel: "Fable 5",
		},
		{
			name:    "garbage input",
			input:   "not json at all",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := ParseStatuslineInput(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseStatuslineInput = nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStatuslineInput failed: %v", err)
			}
			if input.Model.DisplayName != tt.wantModel {
				t.Errorf("model = %q, want %q", input.Model.DisplayName, tt.wantModel)
			}
			snap, ok := input.Snapshot(time.UnixMilli(1750000000000))
			wantOK := tt.wantFiveHour != nil || tt.wantSevenDay != nil
			if ok != wantOK {
				t.Errorf("Snapshot ok = %v, want %v", ok, wantOK)
			}
			assertClaudeWindow(t, "five_hour", snap.FiveHour, tt.wantFiveHour)
			assertClaudeWindow(t, "seven_day", snap.SevenDay, tt.wantSevenDay)
			if wantOK && snap.SavedAtMS != 1750000000000 {
				t.Errorf("saved_at_ms = %d, want 1750000000000", snap.SavedAtMS)
			}
		})
	}
}

func assertClaudeWindow(t *testing.T, name string, got, want *ClaudeWindow) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Errorf("%s = %#v, want %#v", name, got, want)
		return
	}
	if got == nil {
		return
	}
	if got.UsedPercentage != want.UsedPercentage || got.ResetsAt != want.ResetsAt {
		t.Errorf("%s = %+v, want %+v", name, *got, *want)
	}
}

func TestClaudeSnapshotRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "claude-rate-limits.json")
	snap := ClaudeSnapshot{
		SavedAtMS: 1750000000000,
		Model:     "Fable 5",
		FiveHour:  &ClaudeWindow{UsedPercentage: 37.4, ResetsAt: 1783814796},
		SevenDay:  &ClaudeWindow{UsedPercentage: 31.0, ResetsAt: 1784354593},
	}
	if err := WriteClaudeSnapshot(path, snap); err != nil {
		t.Fatalf("WriteClaudeSnapshot failed: %v", err)
	}

	got, err := ReadClaudeSnapshot(path)
	if err != nil {
		t.Fatalf("ReadClaudeSnapshot failed: %v", err)
	}
	if got.SavedAtMS != snap.SavedAtMS || got.Model != snap.Model {
		t.Errorf("roundtrip = %+v, want %+v", got, snap)
	}
	assertClaudeWindow(t, "five_hour", got.FiveHour, snap.FiveHour)
	assertClaudeWindow(t, "seven_day", got.SevenDay, snap.SevenDay)

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read snapshot dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("snapshot dir has %d entries, want 1 (no leftover temp files)", len(entries))
	}
}

func TestStatuslineText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"both windows", statuslineBothWindows, "Fable 5 · 5h 37% · wk 31%"},
		{"no rate limits", `{"model":{"display_name":"Fable 5"}}`, "Fable 5"},
		{"no model name", `{"rate_limits":{"seven_day":{"used_percentage":80.6}}}`, "Claude Code · wk 81%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := ParseStatuslineInput(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("ParseStatuslineInput failed: %v", err)
			}
			if got := StatuslineText(input); got != tt.want {
				t.Errorf("StatuslineText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaudeProviderQuota(t *testing.T) {
	now := time.UnixMilli(1783900000000)
	fixedNow := func() time.Time { return now }

	t.Run("missing snapshot is unavailable with setup help", func(t *testing.T) {
		p := &claudeProvider{snapshotPath: filepath.Join(t.TempDir(), "missing.json"), now: fixedNow}
		got := p.quota(context.Background())
		if got.Status != StatusUnavailable {
			t.Errorf("status = %q, want %q", got.Status, StatusUnavailable)
		}
		if got.Help == "" {
			t.Error("help = empty, want setup instructions")
		}
	})

	t.Run("fresh snapshot is ok", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "snap.json")
		writeTestSnapshot(t, path, ClaudeSnapshot{
			SavedAtMS: now.Add(-time.Minute).UnixMilli(),
			FiveHour:  &ClaudeWindow{UsedPercentage: 37.4, ResetsAt: 1783814796},
			SevenDay:  &ClaudeWindow{UsedPercentage: 31.0, ResetsAt: 1784354593},
		})
		p := &claudeProvider{snapshotPath: path, now: fixedNow}
		got := p.quota(context.Background())
		if got.Status != StatusOK {
			t.Fatalf("status = %q (reason %q), want ok", got.Status, got.Reason)
		}
		if len(got.Windows) != 2 {
			t.Fatalf("windows = %d, want 2", len(got.Windows))
		}
		if got.Windows[0].ID != "5h" || !approxEqual(got.Windows[0].UsedPercent, 37.4) {
			t.Errorf("first window = %+v, want 5h at 37.4%%", got.Windows[0])
		}
		if got.Windows[1].ID != "weekly" || got.Windows[1].WindowMinutes != 10080 {
			t.Errorf("second window = %+v, want weekly/10080", got.Windows[1])
		}
	})

	t.Run("old snapshot is stale", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "snap.json")
		writeTestSnapshot(t, path, ClaudeSnapshot{
			SavedAtMS: now.Add(-time.Hour).UnixMilli(),
			FiveHour:  &ClaudeWindow{UsedPercentage: 10},
		})
		p := &claudeProvider{snapshotPath: path, now: fixedNow}
		if got := p.quota(context.Background()); got.Status != StatusStale {
			t.Errorf("status = %q, want %q", got.Status, StatusStale)
		}
	})
}

func writeTestSnapshot(t *testing.T, path string, snap ClaudeSnapshot) {
	t.Helper()
	if err := WriteClaudeSnapshot(path, snap); err != nil {
		t.Fatalf("write test snapshot: %v", err)
	}
}

func approxEqual(got, want float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.0000001
}
