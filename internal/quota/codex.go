package quota

import (
	"context"
	"sync"
	"time"

	"opencode-dashboard/internal/source/codex"
)

// codexTTL memoizes the rollout scan; quota moves slowly, so 30s is plenty.
const codexTTL = 30 * time.Second

type codexProvider struct {
	home string
	now  func() time.Time

	mu        sync.Mutex
	cached    ProviderQuota
	fetchedAt time.Time
}

func (p *codexProvider) quota(ctx context.Context) ProviderQuota {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if !p.fetchedAt.IsZero() && now.Sub(p.fetchedAt) < codexTTL {
		return p.cached
	}
	p.cached = p.fetch(ctx, now)
	p.fetchedAt = now
	return p.cached
}

func (p *codexProvider) fetch(ctx context.Context, now time.Time) ProviderQuota {
	result := ProviderQuota{Provider: ProviderCodex, Label: "Codex"}
	limits, err := codex.LatestRateLimits(ctx, p.home)
	if err != nil {
		result.Status = StatusUnavailable
		result.Reason = err.Error()
		return result
	}
	if limits == nil {
		result.Status = StatusUnavailable
		result.Reason = "no rate-limit snapshot found in Codex sessions"
		result.Help = "Run a Codex CLI session; the CLI records rate limits with each turn."
		return result
	}
	if w := limits.Primary; w != nil {
		result.Windows = append(result.Windows, Window{ID: "5h", UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt, WindowMinutes: w.WindowMinutes})
	}
	if w := limits.Secondary; w != nil {
		result.Windows = append(result.Windows, Window{ID: "weekly", UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt, WindowMinutes: w.WindowMinutes})
	}
	result.Plan = limits.PlanType
	result.AsOfMS = limits.ObservedAt.UnixMilli()
	result.Status = statusForAge(limits.ObservedAt, now)
	return result
}
