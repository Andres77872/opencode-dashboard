package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeSetupHelp tells the user how to enable Claude quota collection; it is
// surfaced by the API whenever no snapshot exists yet.
const ClaudeSetupHelp = `Run "opencode-dashboard claude-statusline --install" (or add {"statusLine":{"type":"command","command":"opencode-dashboard claude-statusline"}} to ~/.claude/settings.json), then send a message in Claude Code. Pro/Max only; quota appears after the first response.`

// ClaudeWindow is one persisted rate-limit window.
type ClaudeWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at,omitempty"` // epoch seconds
}

// ClaudeSnapshot is the file the claude-statusline subcommand writes and the
// dashboard reads.
type ClaudeSnapshot struct {
	SavedAtMS int64         `json:"saved_at_ms"`
	Model     string        `json:"model,omitempty"`
	FiveHour  *ClaudeWindow `json:"five_hour,omitempty"`
	SevenDay  *ClaudeWindow `json:"seven_day,omitempty"`
}

// StatuslineInput is the narrow slice of Claude Code's documented statusline
// JSON (code.claude.com/docs/en/statusline) this feature needs. rate_limits is
// only present for Pro/Max subscribers after the first API response, and each
// window may be independently absent.
type StatuslineInput struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	RateLimits struct {
		FiveHour *statuslineWindow `json:"five_hour"`
		SevenDay *statuslineWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type statuslineWindow struct {
	UsedPercentage float64   `json:"used_percentage"`
	ResetsAt       epochTime `json:"resets_at"`
}

// epochTime tolerates the two plausible wire forms of resets_at: a numeric
// epoch (seconds or milliseconds) or an RFC3339 string.
type epochTime int64

func (t *epochTime) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*t = 0
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return nil // best-effort field: ignore unparseable values
		}
		if ts, err := time.Parse(time.RFC3339Nano, text); err == nil {
			*t = epochTime(ts.Unix())
		}
		return nil
	}
	var numeric float64
	if err := json.Unmarshal(data, &numeric); err != nil {
		return nil
	}
	seconds := int64(numeric)
	if seconds > 100_000_000_000 { // epoch milliseconds
		seconds /= 1000
	}
	*t = epochTime(seconds)
	return nil
}

func ParseStatuslineInput(r io.Reader) (StatuslineInput, error) {
	var input StatuslineInput
	body, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return input, fmt.Errorf("read statusline input: %w", err)
	}
	if err := json.Unmarshal(body, &input); err != nil {
		return input, fmt.Errorf("parse statusline input: %w", err)
	}
	return input, nil
}

// Snapshot converts the input to the persisted form; ok is false when the
// input carries no rate-limit window (API-key sessions, first render).
func (in StatuslineInput) Snapshot(now time.Time) (ClaudeSnapshot, bool) {
	snap := ClaudeSnapshot{SavedAtMS: now.UnixMilli(), Model: in.Model.DisplayName}
	if w := in.RateLimits.FiveHour; w != nil {
		snap.FiveHour = &ClaudeWindow{UsedPercentage: w.UsedPercentage, ResetsAt: int64(w.ResetsAt)}
	}
	if w := in.RateLimits.SevenDay; w != nil {
		snap.SevenDay = &ClaudeWindow{UsedPercentage: w.UsedPercentage, ResetsAt: int64(w.ResetsAt)}
	}
	return snap, snap.FiveHour != nil || snap.SevenDay != nil
}

// WriteClaudeSnapshot persists atomically (temp file + rename) so a dashboard
// read never sees a torn write from a concurrent statusline invocation.
func WriteClaudeSnapshot(path string, snap ClaudeSnapshot) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".claude-rate-limits-*.tmp")
	if err != nil {
		return fmt.Errorf("create snapshot temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace snapshot: %w", err)
	}
	return nil
}

func ReadClaudeSnapshot(path string) (*ClaudeSnapshot, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap ClaudeSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot %s: %w", path, err)
	}
	return &snap, nil
}

// StatuslineText renders the one-line status Claude Code displays, e.g.
// "Fable 5 · 5h 37% · wk 31%".
func StatuslineText(in StatuslineInput) string {
	parts := make([]string, 0, 3)
	if name := strings.TrimSpace(in.Model.DisplayName); name != "" {
		parts = append(parts, name)
	} else {
		parts = append(parts, "Claude Code")
	}
	if w := in.RateLimits.FiveHour; w != nil {
		parts = append(parts, fmt.Sprintf("5h %d%%", int(math.Round(w.UsedPercentage))))
	}
	if w := in.RateLimits.SevenDay; w != nil {
		parts = append(parts, fmt.Sprintf("wk %d%%", int(math.Round(w.UsedPercentage))))
	}
	return strings.Join(parts, " · ")
}

type claudeProvider struct {
	snapshotPath string
	now          func() time.Time
}

func (p *claudeProvider) quota(_ context.Context) ProviderQuota {
	result := ProviderQuota{Provider: ProviderClaude, Label: "Claude Code"}
	snap, err := ReadClaudeSnapshot(p.snapshotPath)
	if err != nil {
		result.Status = StatusUnavailable
		if os.IsNotExist(err) {
			result.Reason = "no Claude rate-limit snapshot recorded yet"
		} else {
			result.Reason = err.Error()
		}
		result.Help = ClaudeSetupHelp
		return result
	}
	if w := snap.FiveHour; w != nil {
		result.Windows = append(result.Windows, Window{ID: "5h", UsedPercent: w.UsedPercentage, ResetsAt: w.ResetsAt, WindowMinutes: 300})
	}
	if w := snap.SevenDay; w != nil {
		result.Windows = append(result.Windows, Window{ID: "weekly", UsedPercent: w.UsedPercentage, ResetsAt: w.ResetsAt, WindowMinutes: 10080})
	}
	if len(result.Windows) == 0 {
		result.Status = StatusUnavailable
		result.Reason = "recorded snapshot has no rate-limit windows"
		result.Help = ClaudeSetupHelp
		return result
	}
	result.AsOfMS = snap.SavedAtMS
	result.Status = statusForAge(time.UnixMilli(snap.SavedAtMS), p.now())
	return result
}
