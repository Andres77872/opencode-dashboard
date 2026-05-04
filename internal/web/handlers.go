package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

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
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := stats.Overview(ctx, h.store, pq)
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
		InternalError("failed to compute overview").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Daily(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}

	// Check for dimension query param — if present, route to dimension endpoint
	if dim := r.URL.Query().Get("dimension"); dim != "" {
		result, err := stats.DailyDimension(ctx, h.store, dim, pq)
		if err != nil {
			if strings.Contains(err.Error(), "invalid dimension") {
				BadRequest(err.Error()).Write(w)
				return
			}
			if strings.Contains(err.Error(), "invalid period") {
				BadRequest(err.Error()).Write(w)
				return
			}
			InternalError("failed to compute dimension stats").Write(w)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Parse granularity param
	granStr := r.URL.Query().Get("granularity")
	var result stats.DailyStats
	var err error
	switch granStr {
	case "hour":
		result, err = stats.Daily(ctx, h.store, pq, stats.GranularityHour)
	case "day":
		result, err = stats.Daily(ctx, h.store, pq, stats.GranularityDay)
	default:
		// Don't pass granularity — let Daily decide based on period (auto-hour for 1d)
		result, err = stats.Daily(ctx, h.store, pq)
	}
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
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := stats.Models(ctx, h.store, pq)
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
		InternalError("failed to compute model stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Tools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := stats.Tools(ctx, h.store, pq)
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
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
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := stats.Projects(ctx, h.store, pq)
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		InternalError("failed to compute project stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) ProjectDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		BadRequest("missing project id").Write(w)
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	page := parseIntQuery(r, "page", 1)
	limit := parseIntQuery(r, "limit", 10)

	result, err := stats.ProjectByID(ctx, h.store, id, pq, page, limit)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
		InternalError("failed to get project detail").Write(w)
		return
	}
	if result == nil {
		NotFound("project not found").Write(w)
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
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}

	var projectID string
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		projectID = strings.TrimSpace(pid)
	}

	result, err := stats.SessionsWithQuery(ctx, h.store, stats.SessionQuery{
		Page:      page,
		PageSize:  limit,
		Filter:    r.URL.Query().Get("filter"),
		ProjectID: projectID,
		Sort:      stats.SessionSortNewest,
		Period:    pq.Period,
		From:      pq.From,
		To:        pq.To,
	})
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
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

// parsePeriodQuery parses period, from, and to query parameters into a PeriodQuery.
// Priority: from > period > default "7d".
// Returns an APIError (HTTP 400) on validation failure.
func parsePeriodQuery(r *http.Request) (stats.PeriodQuery, *APIError) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	period := r.URL.Query().Get("period")

	// If from is present, use explicit range mode
	if from != "" {
		// Validate from format
		fromTime, err := time.Parse("2006-01-02", from)
		if err != nil {
			return stats.PeriodQuery{}, &APIError{
				Error:   http.StatusText(http.StatusBadRequest),
				Code:    http.StatusBadRequest,
				Message: "invalid from date: expected YYYY-MM-DD format",
			}
		}

		// Reject future from date
		if fromTime.After(time.Now().UTC()) {
			return stats.PeriodQuery{}, &APIError{
				Error:   http.StatusText(http.StatusBadRequest),
				Code:    http.StatusBadRequest,
				Message: "from date cannot be in the future",
			}
		}

		// Validate to format and constraints when present
		if to != "" {
			toTime, err := time.Parse("2006-01-02", to)
			if err != nil {
				return stats.PeriodQuery{}, &APIError{
					Error:   http.StatusText(http.StatusBadRequest),
					Code:    http.StatusBadRequest,
					Message: "invalid to date: expected YYYY-MM-DD format",
				}
			}

			// Reject future to date
			if toTime.After(time.Now().UTC()) {
				return stats.PeriodQuery{}, &APIError{
					Error:   http.StatusText(http.StatusBadRequest),
					Code:    http.StatusBadRequest,
					Message: "to date cannot be in the future",
				}
			}

			if fromTime.After(toTime) {
				return stats.PeriodQuery{}, &APIError{
					Error:   http.StatusText(http.StatusBadRequest),
					Code:    http.StatusBadRequest,
					Message: "from date must be before or equal to to date",
				}
			}
		}

		return stats.PeriodQuery{From: from, To: to}, nil
	}

	// Period mode: use period or default to "7d"
	if period == "" {
		period = "7d"
	}

	return stats.PeriodQuery{Period: period}, nil
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

func (h *Handlers) Messages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	page := parseIntQuery(r, "page", 1)
	limit := parseIntQuery(r, "limit", 50)
	if limit > 100 {
		limit = 100
	}
	sort := stats.ParseMessageSort(r.URL.Query().Get("sort"))

	result, err := stats.MessagesByPeriod(ctx, h.store, pq, page, limit, sort)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		if strings.Contains(err.Error(), "invalid period") {
			BadRequest(err.Error()).Write(w)
			return
		}
		InternalError("failed to list messages").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) MessageByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := extractMessageID(r.URL.Path)
	if id == "" {
		BadRequest("message id required").Write(w)
		return
	}

	result, err := stats.MessageByID(ctx, h.store, id)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		InternalError("failed to get message").Write(w)
		return
	}
	if result == nil {
		NotFound("message not found").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func extractMessageID(path string) string {
	prefix := "/api/v1/messages/"
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
