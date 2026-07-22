package quota

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type fakeProvider struct {
	result ProviderQuota
	block  bool // wait for ctx cancellation before returning
}

func (f *fakeProvider) quota(ctx context.Context) ProviderQuota {
	if f.block {
		<-ctx.Done()
		return ProviderQuota{Provider: f.result.Provider, Status: StatusUnavailable, Reason: ctx.Err().Error()}
	}
	return f.result
}

type timeoutOverrideTestProvider struct {
	timeout  time.Duration
	observed chan time.Duration
}

func (p *timeoutOverrideTestProvider) quotaTimeout() time.Duration {
	return p.timeout
}

func (p *timeoutOverrideTestProvider) quota(ctx context.Context) ProviderQuota {
	deadline, ok := ctx.Deadline()
	if !ok {
		p.observed <- 0
	} else {
		p.observed <- time.Until(deadline)
	}
	return ProviderQuota{Provider: ProviderKimi, Status: StatusOK}
}

func TestServiceQuotasStableOrder(t *testing.T) {
	svc := &Service{
		now:     time.Now,
		timeout: time.Second,
		providers: []provider{
			&fakeProvider{result: ProviderQuota{Provider: ProviderCodex, Status: StatusOK}},
			&fakeProvider{result: ProviderQuota{Provider: ProviderClaude, Status: StatusUnavailable}},
			&fakeProvider{result: ProviderQuota{Provider: ProviderKimi, Status: StatusOK}},
			&fakeProvider{result: ProviderQuota{Provider: ProviderMiniMax, Status: StatusStale}},
		},
	}
	got := svc.Quotas(context.Background())
	if len(got.Providers) != 4 {
		t.Fatalf("providers = %d, want 4", len(got.Providers))
	}
	wantOrder := []string{ProviderCodex, ProviderClaude, ProviderKimi, ProviderMiniMax}
	for i, want := range wantOrder {
		if got.Providers[i].Provider != want {
			t.Errorf("providers[%d] = %q, want %q", i, got.Providers[i].Provider, want)
		}
	}
	if got.FetchedAtMS == 0 {
		t.Error("fetched_at_ms = 0, want current time")
	}
}

func TestServiceQuotasSlowProviderIsBounded(t *testing.T) {
	svc := &Service{
		now:     time.Now,
		timeout: 50 * time.Millisecond,
		providers: []provider{
			&fakeProvider{result: ProviderQuota{Provider: ProviderCodex, Status: StatusOK}},
			&fakeProvider{result: ProviderQuota{Provider: ProviderClaude}, block: true},
		},
	}
	start := time.Now()
	got := svc.Quotas(context.Background())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Quotas took %v, want bounded by the 50ms provider timeout", elapsed)
	}
	if got.Providers[1].Status != StatusUnavailable {
		t.Errorf("blocked provider status = %q, want unavailable", got.Providers[1].Status)
	}
	if got.Providers[0].Status != StatusOK {
		t.Errorf("fast provider status = %q, want ok", got.Providers[0].Status)
	}
}

func TestServiceQuotasHonorsProviderTimeoutOverride(t *testing.T) {
	observed := make(chan time.Duration, 1)
	overrideProvider := &timeoutOverrideTestProvider{timeout: 500 * time.Millisecond, observed: observed}
	svc := &Service{
		now:       time.Now,
		timeout:   20 * time.Millisecond,
		providers: []provider{overrideProvider},
	}

	got := svc.Quotas(context.Background())
	if got.Providers[0].Status != StatusOK {
		t.Fatalf("status = %q, want ok", got.Providers[0].Status)
	}
	remaining := <-observed
	if remaining < 400*time.Millisecond || remaining > overrideProvider.timeout {
		t.Errorf("provider deadline remaining = %v, want override near %v", remaining, overrideProvider.timeout)
	}
	if got := (&kimiProvider{}).quotaTimeout(); got < kimiRefreshBudget+kimiUsageTimeout {
		t.Errorf("Kimi service timeout = %v, want room for %v refresh plus %v usage",
			got, kimiRefreshBudget, kimiUsageTimeout)
	}
}

func TestNewServiceBuildsAllProviders(t *testing.T) {
	t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "") // no key => minimax stays offline
	svc := NewService(Options{
		CodexHome:          t.TempDir(),
		ClaudeSnapshotPath: "x",
		KimiHome:           t.TempDir(),
		MiniMaxAuthPath:    filepath.Join(t.TempDir(), "absent.json"),
	})
	got := svc.Quotas(context.Background())
	if len(got.Providers) != 4 {
		t.Fatalf("providers = %d, want 4", len(got.Providers))
	}
}
