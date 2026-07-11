package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// RateWindow is one enforcement window of the Codex subscription quota.
type RateWindow struct {
	UsedPercent   float64
	WindowMinutes int64
	ResetsAt      int64 // epoch seconds
}

// RateLimits is the newest quota snapshot the Codex CLI recorded in its
// rollout transcripts (token_count events carry a rate_limits object).
type RateLimits struct {
	Primary    *RateWindow // short/session window (window_minutes 300 = 5h)
	Secondary  *RateWindow // weekly window (window_minutes 10080)
	PlanType   string
	ObservedAt time.Time
}

const (
	// Rollouts older than this cannot hold a current window snapshot: the
	// longest window is 7 days, so anything past 14 days is pure noise.
	rateLimitMaxFileAge = 14 * 24 * time.Hour
	rateLimitMaxFiles   = 20
)

// LatestRateLimits scans rollout transcripts newest-first for the most recent
// rate_limits snapshot. Files are append-only and per-session, so the last
// matching line of the newest-modified file wins. Returns (nil, nil) when no
// snapshot exists (e.g. the CLI never ran or predates rate-limit recording).
func LatestRateLimits(ctx context.Context, codexHome string) (*RateLimits, error) {
	discovery := discoverTranscripts(ctx, codexHome)
	if discovery.diagnostics.Status == "unavailable" {
		return nil, errors.New(discovery.diagnostics.Reason)
	}

	files := append([]transcriptFile(nil), discovery.files...)
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.After(files[j].ModTime) })

	cutoff := time.Now().UTC().Add(-rateLimitMaxFileAge)
	examined := 0
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.ModTime.Before(cutoff) || examined >= rateLimitMaxFiles {
			break
		}
		examined++
		snapshot, err := lastRateLimitsInFile(ctx, file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if snapshot != nil {
			return snapshot, nil
		}
	}
	return nil, nil
}

// rolloutRateLimitLine is the narrow shape needed from a rollout line; the
// full record families stay in parser.go.
type rolloutRateLimitLine struct {
	Timestamp string `json:"timestamp"`
	Payload   struct {
		RateLimits *rawRateLimits `json:"rate_limits"`
		Info       struct {
			RateLimits *rawRateLimits `json:"rate_limits"`
		} `json:"info"`
	} `json:"payload"`
}

type rawRateLimits struct {
	Primary   *rawRateWindow `json:"primary"`
	Secondary *rawRateWindow `json:"secondary"`
	PlanType  string         `json:"plan_type"`
}

type rawRateWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func lastRateLimitsInFile(ctx context.Context, file transcriptFile) (*RateLimits, error) {
	fh, err := os.Open(file.Path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	reader := bufio.NewReaderSize(fh, 128*1024)
	var latest *RateLimits
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := reader.ReadString('\n')
		if line != "" {
			// Cheap pre-filter: most lines carry no rate_limits object.
			if strings.Contains(line, `"rate_limits"`) {
				if snapshot := parseRateLimitLine(line, file.ModTime); snapshot != nil {
					latest = snapshot
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			break
		}
		return nil, err
	}
	return latest, nil
}

// parseRateLimitLine returns nil for malformed lines or lines whose
// rate_limits object carries no window; this is a best-effort telemetry
// read, not stats, so bad lines are skipped silently.
func parseRateLimitLine(line string, fallback time.Time) *RateLimits {
	var raw rolloutRateLimitLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &raw); err != nil {
		return nil
	}
	limits := raw.Payload.Info.RateLimits
	if limits == nil {
		limits = raw.Payload.RateLimits
	}
	if limits == nil || (limits.Primary == nil && limits.Secondary == nil) {
		return nil
	}
	observed := fallback.UTC()
	if ts, err := time.Parse(time.RFC3339Nano, raw.Timestamp); err == nil {
		observed = ts.UTC()
	}
	return &RateLimits{
		Primary:    limits.Primary.toWindow(),
		Secondary:  limits.Secondary.toWindow(),
		PlanType:   limits.PlanType,
		ObservedAt: observed,
	}
}

func (w *rawRateWindow) toWindow() *RateWindow {
	if w == nil {
		return nil
	}
	return &RateWindow{UsedPercent: w.UsedPercent, WindowMinutes: w.WindowMinutes, ResetsAt: w.ResetsAt}
}
