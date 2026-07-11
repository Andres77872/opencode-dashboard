package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/version"
)

const (
	// minimaxRemainsURL is MiniMax's documented quota endpoint
	// (platform.minimax.io/docs/token-plan/faq).
	minimaxRemainsURL = "https://www.minimax.io/v1/token_plan/remains"
	minimaxTTL        = 60 * time.Second
	minimaxHelp       = "Set " + config.EnvMiniMaxAPIKey + " or sign in to the MiniMax coding plan in opencode."
)

type minimaxProvider struct {
	authPath string
	client   *http.Client
	now      func() time.Time
	url      string // test seam; empty => minimaxRemainsURL

	mu        sync.Mutex
	cached    *ProviderQuota
	fetchedAt time.Time
	lastGood  *ProviderQuota
}

func (p *minimaxProvider) quota(ctx context.Context) ProviderQuota {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.cached != nil && now.Sub(p.fetchedAt) < minimaxTTL {
		return *p.cached
	}
	result := p.fetch(ctx, now)
	p.cached = &result
	p.fetchedAt = now
	return result
}

func (p *minimaxProvider) fetch(ctx context.Context, now time.Time) ProviderQuota {
	result := ProviderQuota{Provider: ProviderMiniMax, Label: "MiniMax", Plan: "coding plan"}

	key, err := resolveMiniMaxKey(p.authPath)
	if err != nil || key == "" {
		result.Status = StatusUnavailable
		result.Reason = "no MiniMax API key found"
		if err != nil {
			result.Reason = err.Error()
		}
		result.Help = minimaxHelp
		return result
	}

	remains, err := p.fetchRemains(ctx, key)
	if err != nil {
		// Keep showing the last good data through transient failures.
		if p.lastGood != nil {
			stale := *p.lastGood
			stale.Status = StatusStale
			stale.Reason = err.Error()
			return stale
		}
		result.Status = StatusUnavailable
		result.Reason = err.Error()
		return result
	}

	entry := pickMiniMaxModel(remains.ModelRemains)
	if entry == nil {
		result.Status = StatusUnavailable
		result.Reason = "MiniMax response contained no model quota entries"
		return result
	}
	result.Windows = []Window{
		{
			ID:            "5h",
			UsedPercent:   100 - entry.CurrentIntervalRemainingPercent,
			ResetsAt:      entry.EndTime / 1000,
			WindowMinutes: (entry.EndTime - entry.StartTime) / 60_000,
		},
		{
			ID:            "weekly",
			UsedPercent:   100 - entry.CurrentWeeklyRemainingPercent,
			ResetsAt:      entry.WeeklyEndTime / 1000,
			WindowMinutes: (entry.WeeklyEndTime - entry.WeeklyStartTime) / 60_000,
		},
	}
	result.AsOfMS = now.UnixMilli()
	result.Status = StatusOK
	good := result
	p.lastGood = &good
	return result
}

type minimaxRemainsResponse struct {
	ModelRemains []minimaxModelRemain `json:"model_remains"`
	BaseResp     struct {
		StatusCode int64  `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type minimaxModelRemain struct {
	ModelName                       string  `json:"model_name"`
	StartTime                       int64   `json:"start_time"` // epoch ms
	EndTime                         int64   `json:"end_time"`
	CurrentIntervalRemainingPercent float64 `json:"current_interval_remaining_percent"`
	WeeklyStartTime                 int64   `json:"weekly_start_time"`
	WeeklyEndTime                   int64   `json:"weekly_end_time"`
	CurrentWeeklyRemainingPercent   float64 `json:"current_weekly_remaining_percent"`
}

func (p *minimaxProvider) fetchRemains(ctx context.Context, key string) (*minimaxRemainsResponse, error) {
	url := p.url
	if url == "" {
		url = minimaxRemainsURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact MiniMax: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("MiniMax API key was rejected (status %d)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("MiniMax API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read MiniMax response: %w", err)
	}
	var remains minimaxRemainsResponse
	if err := json.Unmarshal(body, &remains); err != nil {
		return nil, fmt.Errorf("parse MiniMax response: %w", err)
	}
	if remains.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("MiniMax API error: %s (code %d)", remains.BaseResp.StatusMsg, remains.BaseResp.StatusCode)
	}
	return &remains, nil
}

// pickMiniMaxModel prefers the "general" entry (the coding-plan text model);
// other entries cover auxiliary quotas like video generation.
func pickMiniMaxModel(entries []minimaxModelRemain) *minimaxModelRemain {
	for i := range entries {
		if strings.EqualFold(entries[i].ModelName, "general") {
			return &entries[i]
		}
	}
	if len(entries) > 0 {
		return &entries[0]
	}
	return nil
}

// resolveMiniMaxKey prefers the env var and falls back to opencode's auth
// store; only the minimax-coding-plan key is decoded, nothing else is read.
func resolveMiniMaxKey(authPath string) (string, error) {
	if key := strings.TrimSpace(os.Getenv(config.EnvMiniMaxAPIKey)); key != "" {
		return key, nil
	}
	if authPath == "" {
		return "", nil
	}
	body, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read opencode auth store: %w", err)
	}
	var store map[string]struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &store); err != nil {
		return "", fmt.Errorf("parse opencode auth store: %w", err)
	}
	return strings.TrimSpace(store["minimax-coding-plan"].Key), nil
}
