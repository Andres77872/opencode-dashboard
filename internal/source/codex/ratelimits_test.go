package codex

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func rateLimitLine(ts string, usedPrimary, usedSecondary float64) string {
	return `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"token_count",` +
		`"info":{"total_token_usage":{"total_tokens":100},"rate_limits":{"limit_id":"codex",` +
		`"primary":{"used_percent":` + floatLit(usedPrimary) + `,"window_minutes":300,"resets_at":1783814796},` +
		`"secondary":{"used_percent":` + floatLit(usedSecondary) + `,"window_minutes":10080,"resets_at":1784354593},` +
		`"plan_type":"pro"}}}}`
}

func floatLit(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func chtimes(t *testing.T, home, relPath string, mod time.Time) {
	t.Helper()
	path := filepath.Join(home, filepath.FromSlash(relPath))
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", relPath, err)
	}
}

func TestLatestRateLimitsNewestFileWins(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/07/10/rollout-old.jsonl": {
			rateLimitLine("2026-07-10T10:00:00.000Z", 90.0, 80.0),
		},
		"sessions/2026/07/11/rollout-new.jsonl": {
			rateLimitLine("2026-07-11T09:00:00.000Z", 12.0, 20.0),
			`{"timestamp":"2026-07-11T10:00:00.000Z","type":"response_item","payload":{"type":"message"}}`,
			rateLimitLine("2026-07-11T11:00:00.000Z", 37.0, 31.0),
		},
	})
	now := time.Now()
	chtimes(t, home, "sessions/2026/07/10/rollout-old.jsonl", now.Add(-2*time.Hour))
	chtimes(t, home, "sessions/2026/07/11/rollout-new.jsonl", now.Add(-1*time.Hour))

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got == nil {
		t.Fatal("LatestRateLimits = nil, want snapshot")
	}
	if !approxEqual(got.Primary.UsedPercent, 37.0) {
		t.Errorf("primary used_percent = %v, want 37.0 (last line of newest file)", got.Primary.UsedPercent)
	}
	if !approxEqual(got.Secondary.UsedPercent, 31.0) {
		t.Errorf("secondary used_percent = %v, want 31.0", got.Secondary.UsedPercent)
	}
	if got.PlanType != "pro" {
		t.Errorf("plan_type = %q, want %q", got.PlanType, "pro")
	}
	if got.Primary.WindowMinutes != 300 || got.Secondary.WindowMinutes != 10080 {
		t.Errorf("window_minutes = %d/%d, want 300/10080", got.Primary.WindowMinutes, got.Secondary.WindowMinutes)
	}
	if got.Primary.ResetsAt != 1783814796 {
		t.Errorf("primary resets_at = %d, want 1783814796", got.Primary.ResetsAt)
	}
	want := time.Date(2026, 7, 11, 11, 0, 0, 0, time.UTC)
	if !got.ObservedAt.Equal(want) {
		t.Errorf("observed_at = %v, want %v", got.ObservedAt, want)
	}
}

func TestLatestRateLimitsFallsThroughFilesWithoutSnapshot(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/07/10/rollout-with-limits.jsonl": {
			rateLimitLine("2026-07-10T10:00:00.000Z", 55.0, 44.0),
		},
		"sessions/2026/07/11/rollout-no-limits.jsonl": {
			`{"timestamp":"2026-07-11T10:00:00.000Z","type":"response_item","payload":{"type":"message"}}`,
		},
	})
	now := time.Now()
	chtimes(t, home, "sessions/2026/07/10/rollout-with-limits.jsonl", now.Add(-2*time.Hour))
	chtimes(t, home, "sessions/2026/07/11/rollout-no-limits.jsonl", now.Add(-1*time.Hour))

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got == nil {
		t.Fatal("LatestRateLimits = nil, want snapshot from older file")
	}
	if !approxEqual(got.Primary.UsedPercent, 55.0) {
		t.Errorf("primary used_percent = %v, want 55.0", got.Primary.UsedPercent)
	}
}

func TestLatestRateLimitsPayloadLevelPlacement(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/07/11/rollout-payload-level.jsonl": {
			`{"timestamp":"2026-07-11T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count",` +
				`"rate_limits":{"primary":{"used_percent":42.5,"window_minutes":300,"resets_at":1783814796},"plan_type":"plus"}}}`,
		},
	})

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got == nil {
		t.Fatal("LatestRateLimits = nil, want payload-level snapshot")
	}
	if !approxEqual(got.Primary.UsedPercent, 42.5) {
		t.Errorf("primary used_percent = %v, want 42.5", got.Primary.UsedPercent)
	}
	if got.Secondary != nil {
		t.Errorf("secondary = %#v, want nil", got.Secondary)
	}
	if got.PlanType != "plus" {
		t.Errorf("plan_type = %q, want %q", got.PlanType, "plus")
	}
}

func TestLatestRateLimitsSkipsMalformedAndPlanOnlyLines(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/07/11/rollout-mixed.jsonl": {
			`this is not json but mentions "rate_limits"`,
			`{"timestamp":"2026-07-11T09:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"rate_limits":{"plan_type":"pro"}}}}`,
			rateLimitLine("2026-07-11T10:00:00.000Z", 61.0, 15.0),
			`{"broken json with "rate_limits" inside`,
		},
	})

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got == nil {
		t.Fatal("LatestRateLimits = nil, want valid snapshot despite malformed neighbors")
	}
	if !approxEqual(got.Primary.UsedPercent, 61.0) {
		t.Errorf("primary used_percent = %v, want 61.0", got.Primary.UsedPercent)
	}
}

func TestLatestRateLimitsSecondaryOnly(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/07/11/rollout-secondary.jsonl": {
			`{"timestamp":"2026-07-11T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count",` +
				`"info":{"rate_limits":{"secondary":{"used_percent":75.0,"window_minutes":10080,"resets_at":1784354593},"plan_type":"pro"}}}}`,
		},
	})

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got == nil {
		t.Fatal("LatestRateLimits = nil, want secondary-only snapshot")
	}
	if got.Primary != nil {
		t.Errorf("primary = %#v, want nil", got.Primary)
	}
	if !approxEqual(got.Secondary.UsedPercent, 75.0) {
		t.Errorf("secondary used_percent = %v, want 75.0", got.Secondary.UsedPercent)
	}
}

func TestLatestRateLimitsIgnoresFilesOlderThanCutoff(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/06/01/rollout-ancient.jsonl": {
			rateLimitLine("2026-06-01T10:00:00.000Z", 99.0, 99.0),
		},
	})
	chtimes(t, home, "sessions/2026/06/01/rollout-ancient.jsonl", time.Now().Add(-30*24*time.Hour))

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got != nil {
		t.Fatalf("LatestRateLimits = %#v, want nil for stale-only files", got)
	}
}

func TestLatestRateLimitsMissingHome(t *testing.T) {
	if _, err := LatestRateLimits(testContext(t), filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("LatestRateLimits = nil error, want unavailable error for missing home")
	}
}

func TestLatestRateLimitsNoTranscripts(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got != nil {
		t.Fatalf("LatestRateLimits = %#v, want nil for empty home", got)
	}
}
