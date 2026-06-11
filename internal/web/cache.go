package web

import (
	"context"
	"net/http"
	"strings"
)

type CacheManager interface {
	Status(context.Context) (CacheStatusResponse, error)
	Sync(context.Context, string, string) (CacheStatusResponse, error)
}

type CacheStatusResponse struct {
	Enabled       bool                `json:"enabled"`
	Path          string              `json:"path,omitempty"`
	Source        string              `json:"source,omitempty"`
	Active        bool                `json:"active"`
	LastUpdatedMS int64               `json:"last_updated_ms,omitempty"`
	Sources       []CacheSourceStatus `json:"sources,omitempty"`
	Sync          *CacheSyncStatus    `json:"sync,omitempty"`
}

type CacheSourceStatus struct {
	SourceID       string `json:"source_id"`
	Label          string `json:"label"`
	Available      bool   `json:"available"`
	Cached         bool   `json:"cached"`
	NeedsSync      bool   `json:"needs_sync"`
	Status         string `json:"status,omitempty"`
	Reason         string `json:"reason,omitempty"`
	LastSyncedMS   int64  `json:"last_synced_ms,omitempty"`
	SafeCutoffMS   int64  `json:"safe_cutoff_ms,omitempty"`
	FreshThroughMS int64  `json:"fresh_through_ms,omitempty"`
	FillAttemptMS  int64  `json:"fill_attempt_ms,omitempty"`
	FillError      string `json:"fill_error,omitempty"`
}

type CacheSyncStatus struct {
	Running         bool            `json:"running"`
	Status          string          `json:"status"`
	Mode            string          `json:"mode,omitempty"`
	Target          string          `json:"target,omitempty"`
	CurrentSourceID string          `json:"current_source_id,omitempty"`
	Total           int             `json:"total"`
	Completed       int             `json:"completed"`
	CurrentPhase    string          `json:"current_phase,omitempty"`
	ItemsDone       int64           `json:"items_done,omitempty"`
	ItemsTotal      int64           `json:"items_total,omitempty"`
	SafeCutoffMS    int64           `json:"safe_cutoff_ms,omitempty"`
	StartedAtMS     int64           `json:"started_at_ms,omitempty"`
	UpdatedAtMS     int64           `json:"updated_at_ms,omitempty"`
	FinishedAtMS    int64           `json:"finished_at_ms,omitempty"`
	Error           string          `json:"error,omitempty"`
	Logs            []CacheLogEntry `json:"logs,omitempty"`
}

type CacheLogEntry struct {
	TimeMS   int64  `json:"time_ms"`
	Level    string `json:"level"`
	SourceID string `json:"source_id,omitempty"`
	Message  string `json:"message"`
}

func (h *Handlers) CacheStatus(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeJSONNoStore(w, http.StatusOK, CacheStatusResponse{Enabled: false})
		return
	}
	result, err := h.cache.Status(r.Context())
	if err != nil {
		InternalError("failed to read cache status").Write(w)
		return
	}
	writeJSONNoStore(w, http.StatusOK, result)
}

func (h *Handlers) CacheSync(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		BadRequest("dashboard cache is disabled").Write(w)
		return
	}
	sourceID := strings.TrimSpace(r.URL.Query().Get("source"))
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	result, err := h.cache.Sync(r.Context(), sourceID, mode)
	if err != nil {
		if strings.Contains(err.Error(), "invalid source") || strings.Contains(err.Error(), "invalid cache sync mode") || strings.Contains(err.Error(), "unavailable") || strings.Contains(err.Error(), "disabled") {
			BadRequest(err.Error()).Write(w)
			return
		}
		InternalError("failed to sync cache").Write(w)
		return
	}
	writeJSONNoStore(w, http.StatusOK, result)
}
