package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opencode-dashboard/internal/quota"
	"opencode-dashboard/internal/source"
)

type fakeQuotaService struct {
	response quota.Response
}

func (f *fakeQuotaService) Quotas(context.Context) quota.Response {
	return f.response
}

func TestQuotasEndpoint(t *testing.T) {
	registry := source.NewRegistry(source.SourceOpenCode)
	fake := &fakeQuotaService{response: quota.Response{
		FetchedAtMS: 1783900000000,
		Providers: []quota.ProviderQuota{
			{
				Provider: quota.ProviderCodex,
				Label:    "Codex",
				Plan:     "pro",
				Status:   quota.StatusOK,
				AsOfMS:   1783899000000,
				Windows: []quota.Window{
					{ID: "5h", UsedPercent: 37, ResetsAt: 1783814796, WindowMinutes: 300},
					{ID: "weekly", UsedPercent: 31, ResetsAt: 1784354593, WindowMinutes: 10080},
				},
			},
			{Provider: quota.ProviderClaude, Label: "Claude Code", Status: quota.StatusUnavailable, Reason: "no snapshot", Help: "set up the statusline"},
		},
	}}
	server := NewServerWithServices("", registry, nil, nil, fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body quota.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(body.Providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(body.Providers))
	}
	codexEntry := body.Providers[0]
	if codexEntry.Provider != quota.ProviderCodex || len(codexEntry.Windows) != 2 || codexEntry.Windows[0].UsedPercent != 37 {
		t.Errorf("codex entry = %+v, want two windows with 37%% first", codexEntry)
	}
	claudeEntry := body.Providers[1]
	if claudeEntry.Status != quota.StatusUnavailable || claudeEntry.Help == "" {
		t.Errorf("claude entry = %+v, want unavailable with help", claudeEntry)
	}
}

func TestQuotasEndpointWithoutService(t *testing.T) {
	registry := source.NewRegistry(source.SourceOpenCode)
	server := NewServerWithCache("", registry, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotas", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body quota.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if body.Providers == nil || len(body.Providers) != 0 {
		t.Errorf("providers = %#v, want empty array", body.Providers)
	}
}
