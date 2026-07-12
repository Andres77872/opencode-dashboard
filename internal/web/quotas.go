package web

import (
	"context"
	"net/http"
	"time"

	"opencode-dashboard/internal/quota"
)

// QuotaService reports remaining provider subscription quota; implemented by
// quota.Service and injected like CacheManager.
type QuotaService interface {
	Quotas(ctx context.Context) quota.Response
}

func (h *Handlers) Quotas(w http.ResponseWriter, r *http.Request) {
	if h.quotas == nil {
		writeJSONNoStore(w, http.StatusOK, quota.Response{Providers: []quota.ProviderQuota{}, FetchedAtMS: time.Now().UnixMilli()})
		return
	}
	writeJSONNoStore(w, http.StatusOK, h.quotas.Quotas(r.Context()))
}
