package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/version"
)

type Handlers struct {
	registry       *source.Registry
	cache          CacheManager
	quotas         QuotaService
	assistant      AssistantService
	chatlog        AssistantChatStore
	pricingAliases PricingAliasStore
	// pricingRates is the cross-source catalog index. Handlers only invalidate
	// it; the sources themselves read it while resolving alias targets.
	pricingRates PricingRateInvalidator
	logger       *slog.Logger
}

// PricingRateInvalidator drops the cached cross-source catalog index after a
// source's bundled pricing may have changed.
type PricingRateInvalidator interface {
	Invalidate()
}

// NewHandlers builds the API handlers from the same options the server takes,
// so a service reaches the handlers exactly as it was configured. Every service
// is optional; the endpoints that need one report it unavailable when it is nil.
func NewHandlers(opts ServerOptions) *Handlers {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Registry == nil {
		opts.Registry = source.NewRegistry(source.SourceOpenCode)
	}
	return &Handlers{
		registry:       opts.Registry,
		cache:          opts.Cache,
		quotas:         opts.Quotas,
		assistant:      opts.Assistant,
		chatlog:        opts.ChatLog,
		pricingAliases: opts.PricingAliases,
		pricingRates:   opts.PricingRates,
		logger:         opts.Logger,
	}
}

func (h *Handlers) Sources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, source.SourceListResponse{
		DefaultSourceID: h.registry.DefaultID(),
		StartupSourceID: h.registry.StartupID(),
		Sources:         h.registry.List(r.Context()),
	})
}

func (h *Handlers) sourceForRequest(w http.ResponseWriter, r *http.Request) (source.Source, bool) {
	selected, err := h.registry.Resolve(r.URL.Query().Get("source"))
	if err != nil {
		SourceError(err).Write(w)
		return nil, false
	}
	return selected, true
}

func (h *Handlers) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	applyModelFilterParams(r, &pq)
	result, err := selected.Overview(ctx, pq)
	if err != nil {
		QueryError(err, "failed to compute overview").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// applyModelFilterParams reads the optional model/provider filter params.
// Only Overview, Daily, and Messages support them; other endpoints ignore
// unknown params as usual.
func applyModelFilterParams(r *http.Request, pq *stats.PeriodQuery) {
	pq.Model = strings.TrimSpace(r.URL.Query().Get("model"))
	pq.Provider = strings.TrimSpace(r.URL.Query().Get("provider"))
}

// OverviewAll returns the cross-source aggregated dashboard. Unlike Overview,
// it has no `source` param — it merges data across every available source.
func (h *Handlers) OverviewAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	dimension := strings.TrimSpace(r.URL.Query().Get("dimension"))
	switch dimension {
	case "", "source", "model":
	default:
		BadRequest("invalid overview dimension: supported values are source and model").Write(w)
		return
	}
	if dimension == "model" {
		result, err := source.AggregateModelUsage(ctx, h.registry, pq, source.ModelUsageOptions{
			IncludeTrend: r.URL.Query().Get("trend") == "true",
		})
		if err != nil {
			InternalError("failed to compute model usage").Write(w)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	opts := source.AggregateOptions{
		IncludeTrend: r.URL.Query().Get("trend") == "true",
		TopN:         parseIntQuery(r, "top", 10, maxTopNQuery),
		// Model totals have their own lazy dimension endpoint. Keeping them out
		// of the source-grouped cold path avoids a large-database Models scan.
		SkipModels: true,
	}
	result, err := source.AggregateOverview(ctx, h.registry, pq, opts)
	if err != nil {
		QueryError(err, "failed to compute aggregated overview").Write(w)
		return
	}
	// Gap states were just recorded by the aggregation above, so the warnings
	// reflect exactly this response's degraded sources.
	warnings := h.recentWarnings(ctx)
	resp := struct {
		source.AllSourcesOverview
		Warnings []SourceWarning `json:"warnings,omitempty"`
	}{AllSourcesOverview: result, Warnings: warnings}
	if len(warnings) > 0 || len(result.Errors) > 0 {
		// A degraded body (zeroed recent hours or missing sources) must never
		// be replayed from the browser cache after the condition clears.
		writeJSONNoStore(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// recentWarnings is nil-safe around the optional cache manager.
func (h *Handlers) recentWarnings(ctx context.Context) []SourceWarning {
	if h.cache == nil {
		return nil
	}
	return h.cache.RecentWarnings(ctx)
}

func (h *Handlers) Daily(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}

	applyModelFilterParams(r, &pq)

	// Check for dimension query param — if present, route to dimension endpoint.
	// The granularity param behaves exactly as it does for the plain daily
	// route: explicit hour/day wins, otherwise the period's auto rule applies.
	if dim := r.URL.Query().Get("dimension"); dim != "" {
		if pq.Model != "" || pq.Provider != "" {
			BadRequest("model/provider filters cannot be combined with dimension").Write(w)
			return
		}
		var result stats.DailyDimensionStats
		var err error
		switch r.URL.Query().Get("granularity") {
		case "hour":
			result, err = selected.DailyDimension(ctx, dim, pq, stats.GranularityHour)
		case "day":
			result, err = selected.DailyDimension(ctx, dim, pq, stats.GranularityDay)
		default:
			result, err = selected.DailyDimension(ctx, dim, pq)
		}
		if err != nil {
			QueryError(err, "failed to compute dimension stats").Write(w)
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
		result, err = selected.Daily(ctx, pq, stats.GranularityHour)
	case "day":
		result, err = selected.Daily(ctx, pq, stats.GranularityDay)
	default:
		// Don't pass granularity — let Daily decide based on period (auto-hour for 1d)
		result, err = selected.Daily(ctx, pq)
	}
	if err != nil {
		QueryError(err, "failed to compute daily stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := selected.Models(ctx, pq)
	if err != nil {
		QueryError(err, "failed to compute model stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Tools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := selected.Tools(ctx, pq)
	if err != nil {
		if errors.Is(err, store.ErrInvalidSchema) {
			InternalError("database schema invalid").Write(w)
			return
		}
		QueryError(err, "failed to compute tool stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) Projects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	result, err := selected.Projects(ctx, pq)
	if err != nil {
		if errors.Is(err, store.ErrInvalidSchema) {
			InternalError("database schema invalid").Write(w)
			return
		}
		QueryError(err, "failed to compute project stats").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) ProjectDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
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
	page := parseIntQuery(r, "page", 1, maxPageQuery)
	limit := parseIntQuery(r, "limit", 10, maxLimitQuery)

	result, err := selected.ProjectByID(ctx, id, pq, page, limit)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		QueryError(err, "failed to get project detail").Write(w)
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
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	page := parseIntQuery(r, "page", 1, maxPageQuery)
	limit := parseIntQuery(r, "limit", 20, maxLimitQuery)
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}

	var projectID string
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		projectID = strings.TrimSpace(pid)
	}

	result, err := selected.Sessions(ctx, stats.SessionQuery{
		Page:      page,
		PageSize:  limit,
		Filter:    r.URL.Query().Get("filter"),
		ProjectID: projectID,
		Sort:      stats.ParseSessionSort(r.URL.Query().Get("sort")),
		Period:    pq.Period,
		From:      pq.From,
		To:        pq.To,
	})
	if err != nil {
		if errors.Is(err, store.ErrInvalidSchema) {
			InternalError("database schema invalid").Write(w)
			return
		}
		QueryError(err, "failed to list sessions").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) SessionByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	id := extractSessionID(r.URL.Path)
	if id == "" {
		BadRequest("session id required").Write(w)
		return
	}
	result, err := selected.SessionByID(ctx, id)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		h.logger.Error("failed to get session", "id", id, "source", r.URL.Query().Get("source"), "err", err)
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
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	result, err := selected.Config(ctx)
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

// Upper bounds for paging params. Without them `(page-1)*limit` overflows int
// and the merge layer slices with a negative offset, panicking the handler.
const (
	maxPageQuery  = 100_000
	maxLimitQuery = 100
	maxTopNQuery  = 100
)

// parseIntQuery reads a positive integer query param, clamped to [1, maxVal].
// Out-of-range, negative, and unparseable values fall back to defaultVal; values
// above maxVal are clamped down rather than rejected, so a too-large ?limit=
// still returns data instead of a 400.
func parseIntQuery(r *http.Request, key string, defaultVal, maxVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 1 {
		return defaultVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

// parsePeriodQuery parses period, from, and to query parameters into a PeriodQuery.
// Priority: from > period > stats.DefaultPeriodPreset.
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

	// Period mode: use period or the shared backend default.
	if period == "" {
		period = stats.DefaultPeriodPreset
	}
	if !stats.IsSupportedPeriodPreset(period) {
		return stats.PeriodQuery{}, &APIError{
			Error:   http.StatusText(http.StatusBadRequest),
			Code:    http.StatusBadRequest,
			Message: stats.InvalidPeriodError(period).Error(),
		}
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
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	pq, apierr := parsePeriodQuery(r)
	if apierr != nil {
		apierr.Write(w)
		return
	}
	applyModelFilterParams(r, &pq)
	page := parseIntQuery(r, "page", 1, maxPageQuery)
	limit := parseIntQuery(r, "limit", 50, maxLimitQuery)
	sort := stats.ParseMessageSort(r.URL.Query().Get("sort"))

	result, err := selected.Messages(ctx, pq, page, limit, sort)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		QueryError(err, "failed to list messages").Write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) MessageByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	id := extractMessageID(r.URL.Path)
	if id == "" {
		BadRequest("message id required").Write(w)
		return
	}

	result, err := selected.MessageByID(ctx, id)
	if err != nil {
		if err == store.ErrInvalidSchema {
			InternalError("database schema invalid").Write(w)
			return
		}
		h.logger.Error("failed to get message", "id", id, "source", r.URL.Query().Get("source"), "err", err)
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
