package web

import (
	"net/http"
	"strconv"
	"strings"

	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/version"
)

type Handlers struct {
	store *store.Store
}

func NewHandlers(s *store.Store) *Handlers {
	return &Handlers{store: s}
}

func (h *Handlers) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result, err := stats.Overview(ctx, h.store)
	if err != nil {
		InternalError("failed to compute overview").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Daily(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}
	result, err := stats.Daily(ctx, h.store, period)
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
		InternalError("failed to compute daily stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result, err := stats.Models(ctx, h.store)
	if err != nil {
		InternalError("failed to compute model stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Tools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result, err := stats.Tools(ctx, h.store)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		InternalError("failed to compute tool stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Projects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result, err := stats.Projects(ctx, h.store)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		InternalError("failed to compute project stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := parseIntQuery(r, "page", 1)
	limit := parseIntQuery(r, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	result, err := stats.Sessions(ctx, h.store, page, limit)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		InternalError("failed to list sessions").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) SessionByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := extractSessionID(r.URL.Path)
	if id == "" {
		BadRequest("session id required").Write(w)
		return
	}
	result, err := stats.SessionByID(ctx, h.store, id)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		InternalError("failed to get session").Write(w)
		return
	}
	if result == nil {
		NotFound("session not found").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Config(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result, err := stats.Config(ctx, h.store)
	if err != nil {
		InternalError("failed to read config").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildInfo string `json:"build_info"`
}

func (h *Handlers) Version(w http.ResponseWriter, r *http.Request) {
	info := VersionInfo{
		Version:   version.Version,
		Commit:    version.ShortCommit(),
		BuildInfo: version.BuildInfo(),
	}
	writeJSON(w, http.StatusOK, info)
}

func parseIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 1 {
		return defaultVal
	}
	return n
}

func extractSessionID(path string) string {
	prefix := "/api/v1/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	id = strings.TrimSuffix(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}
