// Package quota reports remaining subscription quota for the provider
// accounts on this machine, using only official surfaces: the Codex CLI's own
// rate-limit records in its rollout files, Claude Code's documented statusline
// JSON (persisted by the claude-statusline subcommand), and MiniMax's
// documented token_plan/remains endpoint. Values are normalized to
// used-percent with epoch-second reset times.
package quota

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusStale       Status = "stale"
	StatusUnavailable Status = "unavailable"
)

const (
	ProviderCodex   = "codex"
	ProviderClaude  = "claude_code"
	ProviderMiniMax = "minimax"

	// staleAfter marks snapshots older than this as stale: Codex and Claude
	// data only refreshes while their CLIs are running.
	staleAfter = 15 * time.Minute

	// providerTimeout bounds each provider so one slow lookup (e.g. the
	// MiniMax HTTP call) never delays the others or the endpoint.
	providerTimeout = 4 * time.Second
)

// Window is one enforcement window of a provider quota.
type Window struct {
	ID            string  `json:"id"` // "5h" | "weekly"
	UsedPercent   float64 `json:"used_percent"`
	ResetsAt      int64   `json:"resets_at,omitempty"` // epoch seconds
	WindowMinutes int64   `json:"window_minutes,omitempty"`
}

// ProviderQuota is one provider's latest known quota state.
type ProviderQuota struct {
	Provider string   `json:"provider"`
	Label    string   `json:"label"`
	Plan     string   `json:"plan,omitempty"`
	Windows  []Window `json:"windows,omitempty"`
	AsOfMS   int64    `json:"as_of_ms,omitempty"` // when the data was observed
	Status   Status   `json:"status"`
	Reason   string   `json:"reason,omitempty"`
	Help     string   `json:"help,omitempty"` // setup guidance when unavailable
}

// Response is the payload of GET /api/v1/quotas.
type Response struct {
	Providers   []ProviderQuota `json:"providers"`
	FetchedAtMS int64           `json:"fetched_at_ms"`
}

// Options configures a Service.
type Options struct {
	CodexHome          string
	ClaudeSnapshotPath string
	MiniMaxAuthPath    string           // opencode auth.json fallback for the API key
	HTTPClient         *http.Client     // nil => default client with providerTimeout
	Now                func() time.Time // nil => time.Now; test seam
}

type provider interface {
	quota(ctx context.Context) ProviderQuota
}

// Service aggregates quota from all providers.
type Service struct {
	providers []provider
	now       func() time.Time
	timeout   time.Duration
}

func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: providerTimeout}
	}
	return &Service{
		now:     now,
		timeout: providerTimeout,
		providers: []provider{
			&codexProvider{home: opts.CodexHome, now: now},
			&claudeProvider{snapshotPath: opts.ClaudeSnapshotPath, now: now},
			&minimaxProvider{authPath: opts.MiniMaxAuthPath, client: client, now: now},
		},
	}
}

// Quotas queries all providers concurrently. Providers never fail the whole
// response: errors surface as StatusUnavailable entries.
func (s *Service) Quotas(ctx context.Context) Response {
	results := make([]ProviderQuota, len(s.providers))
	var wg sync.WaitGroup
	for i, p := range s.providers {
		wg.Add(1)
		go func(i int, p provider) {
			defer wg.Done()
			providerCtx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()
			results[i] = p.quota(providerCtx)
		}(i, p)
	}
	wg.Wait()
	return Response{Providers: results, FetchedAtMS: s.now().UnixMilli()}
}

// statusForAge classifies a snapshot timestamp against staleAfter.
func statusForAge(asOf, now time.Time) Status {
	if now.Sub(asOf) > staleAfter {
		return StatusStale
	}
	return StatusOK
}
