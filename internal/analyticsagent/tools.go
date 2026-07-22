package analyticsagent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	defaultListLimit  = 10
	maxListLimit      = 50
	defaultDailyLimit = 120
	maxDailyLimit     = 1000
	maxAggregateTopN  = 25
)

var errInvalidToolInput = errors.New("invalid analytics tool input")

type ToolRegistry struct {
	registry      *source.Registry
	projectRefKey []byte
}

type periodArgs struct {
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type sourcePeriodArgs struct {
	Source string `json:"source"`
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type sourcePeriodNoLimitArgs struct {
	Source string `json:"source"`
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type dailyArgs struct {
	Source      string `json:"source"`
	Period      string `json:"period,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Granularity string `json:"granularity,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type dimensionTrendArgs struct {
	Source      string `json:"source"`
	Dimension   string `json:"dimension"`
	Period      string `json:"period,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Granularity string `json:"granularity,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type sessionUsageArgs struct {
	Source string `json:"source"`
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Sort   string `json:"sort,omitempty"`
}

type aggregateArgs struct {
	Period       string `json:"period,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	IncludeTrend bool   `json:"include_trend,omitempty"`
	TrendLimit   int    `json:"trend_limit,omitempty"`
}

func NewToolRegistry(registry *source.Registry) *ToolRegistry {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Fail privacy-safe: project metrics remain usable but uncorrelated if the
		// operating system cannot provide entropy for an in-memory pseudonym key.
		key = nil
	}
	return &ToolRegistry{registry: registry, projectRefKey: key}
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_sources",
			Description: "List the currently registered analytics sources and whether each source is available. Call this before choosing source IDs.",
			Parameters:  rawSchema(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		{
			Name:        "get_overview",
			Description: "Get source-specific sessions, transcript messages, outbound assistant/API requests, tokens, days, and source-specific cost with provenance for a validated period. Kimi results can include request-accounting coverage and unavailable-usage counts.",
			Parameters:  rawSchema(sourcePeriodSchema(false)),
		},
		{
			Name:        "get_cross_source_overview",
			Description: "Compare every available source, including additive outbound request totals. Combined totals intentionally omit cost; source-specific costs retain provenance and must never be added together.",
			Parameters:  rawSchema(`{"type":"object","properties":{"period":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":25},"include_trend":{"type":"boolean"},"trend_limit":{"type":"integer","minimum":1,"maximum":1000}},"additionalProperties":false}`),
		},
		{
			Name:        "get_daily_usage",
			Description: "Get a bounded daily or hourly aggregate time series for one explicit source, including distinct transcript-message and outbound-request counts. Use requests for API-call/attempt questions. Use the dedicated model, tool, and project tools for accurate dimension rankings.",
			Parameters:  rawSchema(`{"type":"object","properties":{"source":{"type":"string"},"period":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},"granularity":{"type":"string","enum":["day","hour"]},"limit":{"type":"integer","minimum":1,"maximum":1000}},"required":["source"],"additionalProperties":false}`),
		},
		{
			Name:        "get_usage_trend_by_dimension",
			Description: "Get a bounded daily or hourly time series for one explicit source grouped by model, tool, or project. Best for questions about which dimension member grew, shrank, or spiked over time.",
			Parameters:  rawSchema(`{"type":"object","properties":{"source":{"type":"string"},"dimension":{"type":"string","enum":["model","tool","project"]},"period":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},"granularity":{"type":"string","enum":["day","hour"]},"limit":{"type":"integer","minimum":1,"maximum":1000}},"required":["source","dimension"],"additionalProperties":false}`),
		},
		{
			Name:        "get_session_usage",
			Description: "Rank coding sessions for one explicit source by recency, cost, or message volume using process-scoped opaque session references and aggregate metrics. Session titles, prompts, and transcripts are never returned.",
			Parameters:  rawSchema(`{"type":"object","properties":{"source":{"type":"string"},"period":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50},"sort":{"type":"string","enum":["newest","oldest","cost","messages"]}},"required":["source"],"additionalProperties":false}`),
		},
		{
			Name:        "get_model_usage",
			Description: "Rank models for one explicit source by outbound assistant/API requests, tokens, sessions, and source-specific cost provenance. The requests field is the unambiguous request count; messages is retained for compatibility.",
			Parameters:  rawSchema(sourcePeriodSchema(true)),
		},
		{
			Name:        "get_tool_usage",
			Description: "Rank coding-assistant tool names by invocations, successes, failures, and sessions. No tool input or output is available.",
			Parameters:  rawSchema(sourcePeriodSchema(true)),
		},
		{
			Name:        "get_project_usage",
			Description: "Rank projects using process-scoped opaque project references and aggregate metrics. Local project IDs, names, and paths are never returned.",
			Parameters:  rawSchema(sourcePeriodSchema(true)),
		},
	}
}

func sourcePeriodSchema(includeLimit bool) string {
	limit := ""
	if includeLimit {
		limit = `,"limit":{"type":"integer","minimum":1,"maximum":50}`
	}
	return `{"type":"object","properties":{"source":{"type":"string"},"period":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"}` + limit + `},"required":["source"],"additionalProperties":false}`
}

func rawSchema(value string) json.RawMessage {
	return json.RawMessage(value)
}

func isAnalyticsToolName(name string) bool {
	switch name {
	case "list_sources", "get_overview", "get_cross_source_overview", "get_daily_usage",
		"get_usage_trend_by_dimension", "get_session_usage",
		"get_model_usage", "get_tool_usage", "get_project_usage":
		return true
	default:
		return false
	}
}

func isSafeSourceID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, char := range value {
		alphaNumeric := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if alphaNumeric || (index > 0 && strings.ContainsRune("_.-", char)) {
			continue
		}
		return false
	}
	return true
}

// Execute always returns a safe JSON envelope suitable for sending to the
// provider. Internal errors and source diagnostics are deliberately collapsed
// so filesystem paths and transcript parsing details cannot escape.
func (r *ToolRegistry) Execute(ctx context.Context, name string, arguments json.RawMessage) json.RawMessage {
	data, err := r.execute(ctx, name, arguments)
	if err != nil {
		code := "tool_failed"
		message := "The analytics tool failed safely."
		if errors.Is(err, errInvalidToolInput) {
			code = "invalid_arguments"
			message = err.Error()
		}
		return marshalEnvelope(false, nil, &safeToolError{Code: code, Message: message})
	}
	return marshalEnvelope(true, data, nil)
}

func (r *ToolRegistry) execute(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
	if r == nil || r.registry == nil {
		return nil, errors.New("source registry is not configured")
	}
	switch name {
	case "list_sources":
		var args struct{}
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		return r.listSources(ctx), nil
	case "get_overview":
		var args sourcePeriodNoLimitArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		selected, pq, err := r.resolve(ctx, args.Source, periodArgs{Period: args.Period, From: args.From, To: args.To})
		if err != nil {
			return nil, err
		}
		result, err := selected.Overview(ctx, pq)
		if err != nil {
			return nil, errors.New("source overview failed")
		}
		return safeOverviewFrom(result, r.projectRefKey), nil
	case "get_cross_source_overview":
		var args aggregateArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		pq, err := validatePeriod(periodArgs{Period: args.Period, From: args.From, To: args.To})
		if err != nil {
			return nil, err
		}
		topN, err := validatedLimit(args.Limit, defaultListLimit, maxAggregateTopN)
		if err != nil {
			return nil, err
		}
		trendLimit, err := validatedLimit(args.TrendLimit, 90, maxDailyLimit)
		if err != nil {
			return nil, err
		}
		if args.IncludeTrend {
			if err := validateBucketWindow(periodArgs{Period: args.Period, From: args.From, To: args.To}, automaticTrendGranularity(args.Period), maxDailyLimit); err != nil {
				return nil, err
			}
		}
		result, err := source.AggregateOverview(ctx, r.registry, pq, source.AggregateOptions{
			IncludeTrend:     args.IncludeTrend,
			TopN:             topN,
			PerSourceTimeout: 3 * time.Second,
		})
		if err != nil {
			return nil, errors.New("cross-source overview failed")
		}
		unavailable := make([]string, 0)
		for _, info := range r.registry.List(ctx) {
			if !info.Available {
				unavailable = appendUnique(unavailable, string(info.ID))
			}
		}
		return safeCrossOverviewFrom(result, trendLimit, unavailable, r.projectRefKey), nil
	case "get_daily_usage":
		var args dailyArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		selected, pq, err := r.resolve(ctx, args.Source, periodArgs{Period: args.Period, From: args.From, To: args.To})
		if err != nil {
			return nil, err
		}
		limit, err := validatedLimit(args.Limit, defaultDailyLimit, maxDailyLimit)
		if err != nil {
			return nil, err
		}
		granularity := stats.Granularity(args.Granularity)
		if granularity != "" && granularity != stats.GranularityDay && granularity != stats.GranularityHour {
			return nil, invalidInput("granularity must be day or hour")
		}
		bucketGranularity := string(granularity)
		if bucketGranularity == "" {
			bucketGranularity = automaticTrendGranularity(args.Period)
		}
		if err := validateBucketWindow(periodArgs{Period: args.Period, From: args.From, To: args.To}, bucketGranularity, maxDailyLimit); err != nil {
			return nil, err
		}
		result, err := selected.Daily(ctx, pq, granularity)
		if err != nil {
			return nil, errors.New("daily usage query failed")
		}
		return safeDailyFrom(result, limit, r.projectRefKey), nil
	case "get_usage_trend_by_dimension":
		var args dimensionTrendArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		dimension := strings.TrimSpace(args.Dimension)
		if dimension != "model" && dimension != "tool" && dimension != "project" {
			return nil, invalidInput("dimension must be model, tool, or project")
		}
		selected, pq, err := r.resolve(ctx, args.Source, periodArgs{Period: args.Period, From: args.From, To: args.To})
		if err != nil {
			return nil, err
		}
		limit, err := validatedLimit(args.Limit, defaultDailyLimit, maxDailyLimit)
		if err != nil {
			return nil, err
		}
		granularity := stats.Granularity(args.Granularity)
		if granularity != "" && granularity != stats.GranularityDay && granularity != stats.GranularityHour {
			return nil, invalidInput("granularity must be day or hour")
		}
		bucketGranularity := string(granularity)
		if bucketGranularity == "" {
			bucketGranularity = automaticTrendGranularity(args.Period)
		}
		if err := validateBucketWindow(periodArgs{Period: args.Period, From: args.From, To: args.To}, bucketGranularity, maxDailyLimit); err != nil {
			return nil, err
		}
		var result stats.DailyDimensionStats
		if granularity == "" {
			result, err = selected.DailyDimension(ctx, dimension, pq)
		} else {
			result, err = selected.DailyDimension(ctx, dimension, pq, granularity)
		}
		if err != nil {
			return nil, errors.New("dimension trend query failed")
		}
		return safeDimensionTrendFrom(result, dimension, limit, r.projectRefKey), nil
	case "get_session_usage":
		var args sessionUsageArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		sort := strings.TrimSpace(args.Sort)
		switch sort {
		case "", "newest", "oldest", "cost", "messages":
		default:
			return nil, invalidInput("sort must be newest, oldest, cost, or messages")
		}
		selected, pq, err := r.resolve(ctx, args.Source, periodArgs{Period: args.Period, From: args.From, To: args.To})
		if err != nil {
			return nil, err
		}
		limit, err := validatedLimit(args.Limit, defaultListLimit, maxListLimit)
		if err != nil {
			return nil, err
		}
		result, err := selected.Sessions(ctx, stats.SessionQuery{
			Page: 1, PageSize: limit,
			Sort:   stats.ParseSessionSort(sort),
			Period: pq.Period, From: pq.From, To: pq.To,
		})
		if err != nil {
			return nil, errors.New("session usage query failed")
		}
		return safeSessionsFrom(result, limit, r.projectRefKey), nil
	case "get_model_usage":
		var args sourcePeriodArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		selected, pq, limit, err := r.resolveList(ctx, args)
		if err != nil {
			return nil, err
		}
		result, err := selected.Models(ctx, pq)
		if err != nil {
			return nil, errors.New("model usage query failed")
		}
		return safeModelsFrom(result, limit, r.projectRefKey), nil
	case "get_tool_usage":
		var args sourcePeriodArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		selected, pq, limit, err := r.resolveList(ctx, args)
		if err != nil {
			return nil, err
		}
		result, err := selected.Tools(ctx, pq)
		if err != nil {
			return nil, errors.New("tool usage query failed")
		}
		return safeToolsFrom(result, limit, r.projectRefKey), nil
	case "get_project_usage":
		var args sourcePeriodArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		selected, pq, limit, err := r.resolveList(ctx, args)
		if err != nil {
			return nil, err
		}
		result, err := selected.Projects(ctx, pq)
		if err != nil {
			return nil, errors.New("project usage query failed")
		}
		return safeProjectsFrom(result, limit, r.projectRefKey), nil
	default:
		return nil, invalidInput("unknown analytics tool")
	}
}

func (r *ToolRegistry) resolve(ctx context.Context, sourceID string, period periodArgs) (source.Source, stats.PeriodQuery, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, stats.PeriodQuery{}, invalidInput("source is required")
	}
	if !isSafeSourceID(sourceID) {
		return nil, stats.PeriodQuery{}, invalidInput("source is invalid or unavailable")
	}
	selected, err := r.registry.Resolve(sourceID)
	if err != nil {
		return nil, stats.PeriodQuery{}, invalidInput("source is invalid or unavailable")
	}
	if !selected.Info(ctx).Available {
		return nil, stats.PeriodQuery{}, invalidInput("source is unavailable")
	}
	pq, err := validatePeriod(period)
	if err != nil {
		return nil, stats.PeriodQuery{}, err
	}
	return selected, pq, nil
}

func (r *ToolRegistry) resolveList(ctx context.Context, args sourcePeriodArgs) (source.Source, stats.PeriodQuery, int, error) {
	selected, pq, err := r.resolve(ctx, args.Source, periodArgs{Period: args.Period, From: args.From, To: args.To})
	if err != nil {
		return nil, stats.PeriodQuery{}, 0, err
	}
	limit, err := validatedLimit(args.Limit, defaultListLimit, maxListLimit)
	if err != nil {
		return nil, stats.PeriodQuery{}, 0, err
	}
	return selected, pq, limit, nil
}

func validatePeriod(args periodArgs) (stats.PeriodQuery, error) {
	args.Period = strings.TrimSpace(args.Period)
	args.From = strings.TrimSpace(args.From)
	args.To = strings.TrimSpace(args.To)
	if args.From != "" && args.Period != "" {
		return stats.PeriodQuery{}, invalidInput("use either period or from/to, not both")
	}
	if args.To != "" && args.From == "" {
		return stats.PeriodQuery{}, invalidInput("to requires from")
	}
	if args.From != "" {
		from, err := time.Parse("2006-01-02", args.From)
		if err != nil {
			return stats.PeriodQuery{}, invalidInput("from must use YYYY-MM-DD")
		}
		if from.After(time.Now().UTC()) {
			return stats.PeriodQuery{}, invalidInput("from cannot be in the future")
		}
		if args.To != "" {
			to, err := time.Parse("2006-01-02", args.To)
			if err != nil {
				return stats.PeriodQuery{}, invalidInput("to must use YYYY-MM-DD")
			}
			if to.After(time.Now().UTC()) || from.After(to) {
				return stats.PeriodQuery{}, invalidInput("to must be current or past and not before from")
			}
		}
		return stats.PeriodQuery{From: args.From, To: args.To}, nil
	}
	if args.Period == "" {
		args.Period = "7d"
	}
	switch args.Period {
	case "1h", "6h", "12h", "24h", "72h", "1d", "7d", "14d", "30d", "1y", "all":
		return stats.PeriodQuery{Period: args.Period}, nil
	default:
		return stats.PeriodQuery{}, invalidInput("period is not supported")
	}
}

// validateBucketWindow prevents provider-generated ranges from reaching source
// implementations that materialize one empty bucket per day/hour before the
// result can be truncated. Aggregate-only tools may still query all time; only
// time-series allocation is capped here.
func validateBucketWindow(args periodArgs, granularity string, maximum int) error {
	args.Period = strings.TrimSpace(args.Period)
	args.From = strings.TrimSpace(args.From)
	args.To = strings.TrimSpace(args.To)
	if maximum <= 0 {
		return invalidInput("time-series bucket limit is invalid")
	}
	if granularity != "day" && granularity != "hour" {
		return invalidInput("granularity must be day or hour")
	}

	var start, end time.Time
	if args.From != "" {
		var err error
		start, err = time.Parse("2006-01-02", args.From)
		if err != nil {
			return invalidInput("from must use YYYY-MM-DD")
		}
		if args.To == "" {
			end = time.Now().UTC()
		} else {
			end, err = time.Parse("2006-01-02", args.To)
			if err != nil {
				return invalidInput("to must use YYYY-MM-DD")
			}
			end = end.AddDate(0, 0, 1)
		}
	} else {
		period := args.Period
		if period == "" {
			period = "7d"
		}
		if period == "all" {
			return invalidInput("all-time time series are not available; use an all-time overview or a bounded trend period")
		}
		now := time.Now().UTC()
		end = now
		switch period {
		case "1h":
			start = now.Add(-time.Hour)
		case "6h":
			start = now.Add(-6 * time.Hour)
		case "12h":
			start = now.Add(-12 * time.Hour)
		case "24h", "1d":
			start = now.Add(-24 * time.Hour)
		case "72h":
			start = now.Add(-72 * time.Hour)
		case "7d":
			start = now.AddDate(0, 0, -7)
		case "14d":
			start = now.AddDate(0, 0, -14)
		case "30d":
			start = now.AddDate(0, 0, -30)
		case "1y":
			start = now.AddDate(-1, 0, 0)
		default:
			return invalidInput("period is not supported")
		}
	}

	unit := 24 * time.Hour
	if granularity == "hour" {
		unit = time.Hour
	}
	duration := end.Sub(start)
	if duration <= 0 {
		return invalidInput("time-series range must be positive")
	}
	buckets := int64(duration / unit)
	if duration%unit != 0 {
		buckets++
	}
	if buckets > int64(maximum) {
		return invalidInput(fmt.Sprintf("%s time series may contain at most %d buckets; choose a shorter range or coarser granularity", granularity, maximum))
	}
	return nil
}

func automaticTrendGranularity(period string) string {
	period = strings.TrimSpace(period)
	switch period {
	case "1h", "6h", "12h", "24h", "72h", "1d":
		return "hour"
	default:
		return "day"
	}
}

func validatedLimit(value, defaultValue, maximum int) (int, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value < 1 || value > maximum {
		return 0, invalidInput(fmt.Sprintf("limit must be between 1 and %d", maximum))
	}
	return value, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		raw = json.RawMessage(`{}`)
		trimmed = raw
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return invalidInput("arguments must be a valid JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidInput("arguments must be a valid JSON object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalidInput("arguments must contain one JSON object")
	}
	return nil
}

func invalidInput(message string) error {
	return fmt.Errorf("%w: %s", errInvalidToolInput, message)
}

type safeToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func marshalEnvelope(ok bool, data any, toolErr *safeToolError) json.RawMessage {
	encoded, err := json.Marshal(struct {
		OK    bool           `json:"ok"`
		Data  any            `json:"data,omitempty"`
		Error *safeToolError `json:"error,omitempty"`
	}{OK: ok, Data: data, Error: toolErr})
	if err != nil {
		return json.RawMessage(`{"ok":false,"error":{"code":"encoding_failed","message":"The analytics tool failed safely."}}`)
	}
	return json.RawMessage(encoded)
}

type safeSourceInfo struct {
	ID           string         `json:"id"`
	Available    bool           `json:"available"`
	Capabilities []string       `json:"capabilities"`
	CostPolicy   safeCostPolicy `json:"cost_policy,omitempty"`
}

type safeCostPolicy struct {
	Status            string `json:"status,omitempty"`
	Currency          string `json:"currency,omitempty"`
	PricingSnapshotID string `json:"pricing_snapshot_id,omitempty"`
	PricingSource     string `json:"pricing_source,omitempty"`
	Note              string `json:"note,omitempty"`
}

type safeCostProvenance struct {
	Status            stats.CostStatus `json:"status"`
	Currency          string           `json:"currency,omitempty"`
	PricingSnapshotID string           `json:"pricing_snapshot_id,omitempty"`
	PricingSource     string           `json:"pricing_source,omitempty"`
	Note              string           `json:"note,omitempty"`
	MissingCount      int64            `json:"missing_count,omitempty"`
	ComputedCount     int64            `json:"computed_count,omitempty"`
	ReportedCount     int64            `json:"reported_count,omitempty"`
}

type safeRequestAccounting struct {
	UsageRecorded    int64               `json:"usage_recorded"`
	UsageRecovered   int64               `json:"usage_recovered"`
	UsageUnavailable int64               `json:"usage_unavailable"`
	TraceCoverage    stats.TraceCoverage `json:"trace_coverage"`
}

func (r *ToolRegistry) listSources(ctx context.Context) []safeSourceInfo {
	infos := r.registry.List(ctx)
	result := make([]safeSourceInfo, 0, len(infos))
	for _, info := range infos {
		if !isSafeSourceID(string(info.ID)) {
			continue
		}
		result = append(result, safeSourceInfo{
			ID:           string(info.ID),
			Available:    info.Available,
			Capabilities: safeCapabilities(info.Capabilities),
			CostPolicy:   safeCostPolicyFrom(info.CostPolicy, r.projectRefKey),
		})
	}
	return result
}

func safeCapabilities(values []string) []string {
	allowed := map[string]bool{"overview": true, "daily": true, "models": true, "tools": true, "projects": true}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if allowed[value] {
			out = append(out, value)
		}
	}
	return out
}

type safeOverview struct {
	SourceID          string                 `json:"source_id"`
	Sessions          int64                  `json:"sessions"`
	Messages          int64                  `json:"messages"`
	Requests          int64                  `json:"requests"`
	Cost              float64                `json:"cost"`
	Tokens            stats.TokenStats       `json:"tokens"`
	CostPerDay        float64                `json:"cost_per_day"`
	Days              int                    `json:"days"`
	CostStatus        stats.CostStatus       `json:"cost_status,omitempty"`
	CostProvenance    *safeCostProvenance    `json:"cost_provenance,omitempty"`
	RequestAccounting *safeRequestAccounting `json:"request_accounting,omitempty"`
}

func safeOverviewFrom(value stats.OverviewStats, key []byte) safeOverview {
	return safeOverview{
		SourceID: safeSourceRef(key, value.SourceID), Sessions: value.Sessions, Messages: value.Messages, Requests: value.Requests,
		Cost: value.Cost, Tokens: value.Tokens, CostPerDay: value.CostPerDay, Days: value.Days,
		CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key),
		RequestAccounting: safeRequestAccountingFrom(value.RequestAccounting),
	}
}

type safeCombinedTotals struct {
	Sessions int64            `json:"sessions"`
	Messages int64            `json:"messages"`
	Requests int64            `json:"requests"`
	Tokens   stats.TokenStats `json:"tokens"`
	Days     int              `json:"days"`
}

type safeSourceOverview struct {
	SourceID           string              `json:"source_id"`
	Overview           safeOverview        `json:"overview"`
	MessageShare       float64             `json:"message_share"`
	TokenShare         float64             `json:"token_share"`
	MessagesPerSession float64             `json:"messages_per_session"`
	TokensPerMessage   stats.AvgTokenStats `json:"tokens_per_message"`
	Trend              []safeDay           `json:"trend,omitempty"`
	TrendTruncated     bool                `json:"trend_truncated,omitempty"`
}

type safeProjectMetric struct {
	Rank           int                 `json:"rank"`
	ProjectRef     string              `json:"project_ref"`
	SourceID       string              `json:"source_id,omitempty"`
	Sessions       int64               `json:"sessions"`
	Messages       int64               `json:"messages"`
	Cost           float64             `json:"cost"`
	Tokens         stats.TokenStats    `json:"tokens"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

type safeCrossOverview struct {
	CombinedTotals       safeCombinedTotals      `json:"combined_totals_without_cost"`
	Sources              []safeSourceOverview    `json:"sources"`
	MessagesPerSession   float64                 `json:"messages_per_session"`
	TokensPerMessage     stats.AvgTokenStats     `json:"tokens_per_message"`
	TopModels            []safeModel             `json:"top_models"`
	TopProjects          []safeProjectMetric     `json:"top_projects"`
	TopTools             []safeTool              `json:"top_tools"`
	UnavailableSources   []string                `json:"unavailable_sources,omitempty"`
	IncompleteDimensions []safeDimensionOmission `json:"incomplete_dimensions,omitempty"`
}

type safeDimensionOmission struct {
	SourceID  string `json:"source_id"`
	Dimension string `json:"dimension"`
}

func safeCrossOverviewFrom(value source.AllSourcesOverview, trendLimit int, unavailable []string, projectRefKey []byte) safeCrossOverview {
	result := safeCrossOverview{
		CombinedTotals: safeCombinedTotals{
			Sessions: value.Total.Sessions, Messages: value.Total.Messages, Requests: value.Total.Requests,
			Tokens: value.Total.Tokens, Days: value.Total.Days,
		},
		MessagesPerSession: value.MessagesPerSession,
		TokensPerMessage:   value.TokensPerMessage,
		UnavailableSources: safeSourceRefs(projectRefKey, unavailable),
	}
	for _, item := range limitSlice(value.TopTools, maxAggregateTopN) {
		result.TopTools = append(result.TopTools, safeToolFrom(item, projectRefKey))
	}
	for _, item := range limitSlice(value.TopModels, maxAggregateTopN) {
		result.TopModels = append(result.TopModels, safeModelFrom(item, projectRefKey))
	}
	for _, item := range value.Sources {
		rawTrend, truncated := lastItems(item.Trend, trendLimit)
		trend := make([]safeDay, 0, len(rawTrend))
		for _, day := range rawTrend {
			trend = append(trend, safeDayFrom(day, projectRefKey))
		}
		result.Sources = append(result.Sources, safeSourceOverview{
			SourceID: safeSourceRef(projectRefKey, item.SourceID), Overview: safeOverviewFrom(item.Overview, projectRefKey),
			MessageShare: item.MessageShare, TokenShare: item.TokenShare,
			MessagesPerSession: item.MessagesPerSession, TokensPerMessage: item.TokensPerMessage,
			Trend: trend, TrendTruncated: truncated,
		})
	}
	for i, item := range value.TopProjects {
		result.TopProjects = append(result.TopProjects, safeProjectFrom(item, i+1, projectRefKey))
	}
	for _, item := range value.Errors {
		result.UnavailableSources = appendUnique(result.UnavailableSources, safeSourceRef(projectRefKey, item.SourceID))
	}
	for _, item := range value.PartialErrors {
		switch item.Dimension {
		case "models", "tools", "projects", "trend":
			result.IncompleteDimensions = append(result.IncompleteDimensions, safeDimensionOmission{
				SourceID:  safeSourceRef(projectRefKey, item.SourceID),
				Dimension: item.Dimension,
			})
		}
	}
	return result
}

type safeDaily struct {
	SourceID          string                 `json:"source_id"`
	Granularity       stats.Granularity      `json:"granularity"`
	Days              []safeDay              `json:"days"`
	Truncated         bool                   `json:"truncated,omitempty"`
	CostStatus        stats.CostStatus       `json:"cost_status,omitempty"`
	CostProvenance    *safeCostProvenance    `json:"cost_provenance,omitempty"`
	RequestAccounting *safeRequestAccounting `json:"request_accounting,omitempty"`
}

func safeDailyFrom(value stats.DailyStats, limit int, key []byte) safeDaily {
	rawDays, truncated := lastItems(value.Days, limit)
	days := make([]safeDay, 0, len(rawDays))
	for _, day := range rawDays {
		days = append(days, safeDayFrom(day, key))
	}
	granularity := value.Granularity
	if granularity != stats.GranularityDay && granularity != stats.GranularityHour {
		granularity = stats.GranularityDay
	}
	return safeDaily{SourceID: safeSourceRef(key, value.SourceID), Granularity: granularity, Days: days, Truncated: truncated, CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key), RequestAccounting: safeRequestAccountingFrom(value.RequestAccounting)}
}

type safeDay struct {
	SourceID          string                 `json:"source_id,omitempty"`
	Date              string                 `json:"date"`
	Sessions          int64                  `json:"sessions"`
	Messages          int64                  `json:"messages"`
	Requests          int64                  `json:"requests"`
	Cost              float64                `json:"cost"`
	Tokens            stats.TokenStats       `json:"tokens"`
	CostStatus        stats.CostStatus       `json:"cost_status,omitempty"`
	CostProvenance    *safeCostProvenance    `json:"cost_provenance,omitempty"`
	RequestAccounting *safeRequestAccounting `json:"request_accounting,omitempty"`
}

func safeDayFrom(value stats.DayStats, key []byte) safeDay {
	return safeDay{SourceID: safeSourceRef(key, value.SourceID), Date: safeDate(value.Date), Sessions: value.Sessions, Messages: value.Messages, Requests: value.Requests, Cost: value.Cost, Tokens: value.Tokens, CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key), RequestAccounting: safeRequestAccountingFrom(value.RequestAccounting)}
}

type safeModelList struct {
	SourceID       string              `json:"source_id"`
	Models         []safeModel         `json:"models"`
	Truncated      bool                `json:"truncated,omitempty"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

func safeModelsFrom(value stats.ModelStats, limit int, key []byte) safeModelList {
	rawModels, truncated := firstItems(value.Models, limit)
	models := make([]safeModel, 0, len(rawModels))
	for _, model := range rawModels {
		models = append(models, safeModelFrom(model, key))
	}
	return safeModelList{SourceID: safeSourceRef(key, value.SourceID), Models: models, Truncated: truncated, CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key)}
}

type safeModel struct {
	SourceID            string               `json:"source_id,omitempty"`
	ModelID             string               `json:"model_id"`
	ProviderID          string               `json:"provider_id"`
	Sessions            int64                `json:"sessions"`
	Messages            int64                `json:"messages"`
	Requests            int64                `json:"requests"`
	Cost                float64              `json:"cost"`
	Tokens              stats.TokenStats     `json:"tokens"`
	CostStatus          stats.CostStatus     `json:"cost_status,omitempty"`
	CostProvenance      *safeCostProvenance  `json:"cost_provenance,omitempty"`
	AvgTokensPerMessage *stats.AvgTokenStats `json:"avg_tokens_per_message,omitempty"`
	AvgTokensPerSession *stats.AvgTokenStats `json:"avg_tokens_per_session,omitempty"`
}

func safeModelFrom(value stats.ModelEntry, key []byte) safeModel {
	return safeModel{
		SourceID: safeSourceRef(key, value.SourceID), ModelID: safeOutboundIdentifier(key, "model", value.ModelID), ProviderID: safeOutboundIdentifier(key, "provider", value.ProviderID),
		Sessions: value.Sessions, Messages: value.Messages, Requests: value.Messages, Cost: value.Cost, Tokens: value.Tokens,
		CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key),
		AvgTokensPerMessage: value.AvgTokensPerMessage, AvgTokensPerSession: value.AvgTokensPerSession,
	}
}

type safeToolList struct {
	SourceID  string     `json:"source_id"`
	Tools     []safeTool `json:"tools"`
	Truncated bool       `json:"truncated,omitempty"`
}

type safeTool struct {
	SourceID    string `json:"source_id,omitempty"`
	Name        string `json:"name"`
	Invocations int64  `json:"invocations"`
	Successes   int64  `json:"successes"`
	Failures    int64  `json:"failures"`
	Sessions    int64  `json:"sessions"`
}

func safeToolsFrom(value stats.ToolStats, limit int, key []byte) safeToolList {
	rawTools, truncated := firstItems(value.Tools, limit)
	tools := make([]safeTool, 0, len(rawTools))
	for _, tool := range rawTools {
		tools = append(tools, safeToolFrom(tool, key))
	}
	return safeToolList{SourceID: safeSourceRef(key, value.SourceID), Tools: tools, Truncated: truncated}
}

func safeToolFrom(value stats.ToolEntry, key []byte) safeTool {
	return safeTool{
		SourceID: safeSourceRef(key, value.SourceID), Name: safeOutboundIdentifier(key, "tool", value.Name),
		Invocations: value.Invocations, Successes: value.Successes, Failures: value.Failures, Sessions: value.Sessions,
	}
}

type safeDimensionTrend struct {
	SourceID       string              `json:"source_id"`
	Dimension      string              `json:"dimension"`
	Granularity    stats.Granularity   `json:"granularity"`
	Entries        []safeDimensionDay  `json:"entries"`
	Truncated      bool                `json:"truncated,omitempty"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

type safeDimensionDay struct {
	Date           string              `json:"date"`
	DimensionKey   string              `json:"dimension_key"`
	Sessions       int64               `json:"sessions"`
	Messages       int64               `json:"messages"`
	Cost           float64             `json:"cost"`
	Tokens         stats.TokenStats    `json:"tokens"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

func safeDimensionTrendFrom(value stats.DailyDimensionStats, dimension string, limit int, key []byte) safeDimensionTrend {
	rawDays, truncated := lastItems(value.Days, limit)
	entries := make([]safeDimensionDay, 0, len(rawDays))
	for _, day := range rawDays {
		sourceID := day.SourceID
		if sourceID == "" {
			sourceID = value.SourceID
		}
		dimensionKey := ""
		switch dimension {
		case "model":
			dimensionKey = safeOutboundIdentifier(key, "model", day.Dimension)
		case "tool":
			dimensionKey = safeOutboundIdentifier(key, "tool", day.Dimension)
		default:
			dimensionKey = opaqueProjectRef(key, sourceID, day.Dimension)
		}
		entries = append(entries, safeDimensionDay{
			Date: safeDate(day.Date), DimensionKey: dimensionKey,
			Sessions: day.Sessions, Messages: day.Messages, Cost: day.Cost, Tokens: day.Tokens,
			CostStatus: safeCostStatus(day.CostStatus), CostProvenance: safeProvenance(day.CostProvenance, key),
		})
	}
	granularity := value.Granularity
	if granularity != stats.GranularityDay && granularity != stats.GranularityHour {
		granularity = stats.GranularityDay
	}
	return safeDimensionTrend{
		SourceID: safeSourceRef(key, value.SourceID), Dimension: dimension, Granularity: granularity,
		Entries: entries, Truncated: truncated,
		CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key),
	}
}

type safeSessionList struct {
	SourceID       string              `json:"source_id"`
	Sessions       []safeSession       `json:"sessions"`
	TotalSessions  int64               `json:"total_sessions"`
	Truncated      bool                `json:"truncated,omitempty"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

type safeSession struct {
	Rank           int                 `json:"rank"`
	SessionRef     string              `json:"session_ref"`
	ProjectRef     string              `json:"project_ref,omitempty"`
	StartedAt      string              `json:"started_at,omitempty"`
	LastActiveAt   string              `json:"last_active_at,omitempty"`
	Messages       int64               `json:"messages"`
	Cost           float64             `json:"cost"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

func safeSessionsFrom(value stats.SessionList, limit int, key []byte) safeSessionList {
	rawSessions, truncated := firstItems(value.Sessions, limit)
	result := safeSessionList{
		SourceID: safeSourceRef(key, value.SourceID), TotalSessions: value.Total,
		Truncated:  truncated || value.Total > int64(len(rawSessions)),
		CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, key),
	}
	for i, session := range rawSessions {
		sourceID := session.SourceID
		if sourceID == "" {
			sourceID = value.SourceID
		}
		projectRef := ""
		if session.ProjectID != "" {
			projectRef = opaqueProjectRef(key, sourceID, session.ProjectID)
		}
		result.Sessions = append(result.Sessions, safeSession{
			Rank:       i + 1,
			SessionRef: opaqueValueRef(key, "session", sourceID+"\x00"+session.ID),
			ProjectRef: projectRef,
			StartedAt:  safeSessionTime(session.TimeCreated), LastActiveAt: safeSessionTime(session.TimeUpdated),
			Messages: session.MessageCount, Cost: session.Cost,
			CostStatus: safeCostStatus(session.CostStatus), CostProvenance: safeProvenance(session.CostProvenance, key),
		})
	}
	return result
}

func safeSessionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return safeDate(value.UTC().Format(time.RFC3339))
}

type safeProjectList struct {
	SourceID       string              `json:"source_id"`
	Projects       []safeProjectMetric `json:"projects"`
	Truncated      bool                `json:"truncated,omitempty"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

func safeProjectsFrom(value stats.ProjectStats, limit int, projectRefKey []byte) safeProjectList {
	end := len(value.Projects)
	if end > limit {
		end = limit
	}
	result := safeProjectList{SourceID: safeSourceRef(projectRefKey, value.SourceID), Truncated: len(value.Projects) > end, CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, projectRefKey)}
	for i, item := range value.Projects[:end] {
		if item.SourceID == "" {
			item.SourceID = value.SourceID
		}
		result.Projects = append(result.Projects, safeProjectFrom(item, i+1, projectRefKey))
	}
	return result
}

func safeProjectFrom(value stats.ProjectEntry, rank int, projectRefKey []byte) safeProjectMetric {
	return safeProjectMetric{
		Rank: rank, ProjectRef: opaqueProjectRef(projectRefKey, value.SourceID, value.ProjectID), SourceID: safeSourceRef(projectRefKey, value.SourceID),
		Sessions: value.Sessions, Messages: value.Messages, Cost: value.Cost, Tokens: value.Tokens,
		CostStatus: safeCostStatus(value.CostStatus), CostProvenance: safeProvenance(value.CostProvenance, projectRefKey),
	}
}

func safeProvenance(value *stats.CostProvenance, key []byte) *safeCostProvenance {
	if value == nil {
		return nil
	}
	return &safeCostProvenance{
		Status: safeCostStatus(value.Status), Currency: safeCurrency(value.Currency), PricingSnapshotID: safeOutboundIdentifier(key, "pricing", value.PricingSnapshotID),
		PricingSource: safePricingSource(value.PricingSource), Note: safeCostNote(value.Note),
		MissingCount: value.MissingCount, ComputedCount: value.ComputedCount, ReportedCount: value.ReportedCount,
	}
}

func safeRequestAccountingFrom(value *stats.RequestAccounting) *safeRequestAccounting {
	if value == nil {
		return nil
	}
	coverage := value.TraceCoverage
	switch coverage {
	case stats.TraceCoverageComplete, stats.TraceCoverageMixed, stats.TraceCoverageSuccessfulOnly, stats.TraceCoverageUnknown:
	default:
		coverage = stats.TraceCoverageUnknown
	}
	return &safeRequestAccounting{
		UsageRecorded:    value.UsageRecorded,
		UsageRecovered:   value.UsageRecovered,
		UsageUnavailable: value.UsageUnavailable,
		TraceCoverage:    coverage,
	}
}

func safeCostPolicyFrom(value source.CostPolicy, key []byte) safeCostPolicy {
	return safeCostPolicy{
		Status:            string(safeCostStatus(stats.CostStatus(value.Status))),
		Currency:          safeCurrency(value.Currency),
		PricingSnapshotID: safeOutboundIdentifier(key, "pricing", value.PricingSnapshotID),
		PricingSource:     safePricingSource(value.PricingSource),
		Note:              safeCostNote(value.Note),
	}
}

func safePricingSource(value string) string {
	value = strings.TrimSpace(value)
	// Pricing metadata is sent to the optional external analytics provider.
	// Only fixed public catalog URLs bundled by this application may leave the
	// machine; arbitrary locally supplied URLs can contain private identifiers.
	switch value {
	case "https://platform.kimi.ai/docs/pricing/chat",
		"https://developers.openai.com/api/docs/pricing",
		"https://www.alibabacloud.com/help/en/model-studio/model-pricing":
		return value
	default:
		return ""
	}
}

func safeCostNote(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(value))
	// Cost notes are otherwise free-form metadata and may contain private local
	// data. Only emit the fixed Kimi accounting disclosures used by this source.
	switch value {
	case "Estimated from Kimi API list prices as an API-equivalent value. Kimi Code memberships and coding plans are not billed per transcript token, so this is not actual subscription spend.",
		"Kimi Code cost is unknown because supported model pricing or request usage is unavailable",
		"aggregate mixes estimated API-equivalent and missing Kimi Code costs",
		"aggregate Kimi Code cost is unknown because supported model pricing or request usage is unavailable":
		return value
	default:
		return ""
	}
}

func safeCostStatus(value stats.CostStatus) stats.CostStatus {
	switch value {
	case stats.CostReported, stats.CostComputed, stats.CostApproximate,
		stats.CostEstimatedAPIEquivalent, stats.CostMixed, stats.CostMissing:
		return value
	default:
		return ""
	}
}

func safeCurrency(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 3 {
		return ""
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return ""
		}
	}
	return value
}

func safeSourceRef(key []byte, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSafeSourceID(value) {
		return value
	}
	return opaqueValueRef(key, "source", value)
}

func safeSourceRefs(key []byte, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if ref := safeSourceRef(key, value); ref != "" {
			result = appendUnique(result, ref)
		}
	}
	return result
}

func safeOutboundIdentifier(key []byte, prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSafePublicIdentifier(value) {
		switch prefix {
		case "provider":
			if isKnownProviderIdentifier(value) {
				return value
			}
		case "tool":
			if isKnownToolIdentifier(value) {
				return value
			}
		}
	}
	return opaqueValueRef(key, prefix, value)
}

func isSafePublicIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		alphaNumeric := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if !alphaNumeric && !strings.ContainsRune("._:@+-", char) {
			return false
		}
	}
	return true
}

func isKnownProviderIdentifier(value string) bool {
	switch strings.ToLower(value) {
	case "openai", "anthropic", "google", "google-vertex", "vertex", "minimax", "minimax-coding-plan",
		"deepseek", "mistral", "groq", "openrouter", "azure", "azure-openai", "aws-bedrock",
		"bedrock", "xai", "cohere", "ollama", "github-copilot", "codex", "claude_code", "opencode":
		return true
	default:
		return false
	}
}

func isKnownToolIdentifier(value string) bool {
	switch strings.ToLower(value) {
	case "bash", "shell", "terminal", "execute", "exec", "exec_command", "read", "write", "edit",
		"apply_patch", "patch", "grep", "rg", "glob", "find", "list", "ls", "search", "webfetch",
		"web_fetch", "websearch", "web_search", "task", "todowrite", "todo_write", "notebookedit",
		"notebook_edit", "multi_tool_use", "computer", "python", "view_image", "imagegen":
		return true
	default:
		return false
	}
}

func safeDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 40 {
		return "date-redacted"
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || strings.ContainsRune("-:+.TZ ", char) {
			continue
		}
		return "date-redacted"
	}
	return value
}

func opaqueValueRef(key []byte, prefix, value string) string {
	if len(key) == 0 {
		return prefix + "-redacted"
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(prefix))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return prefix + "-" + hex.EncodeToString(mac.Sum(nil)[:12])
}

func opaqueProjectRef(key []byte, sourceID, projectID string) string {
	return opaqueValueRef(key, "project", sourceID+"\x00"+projectID)
}

func firstItems[T any](values []T, limit int) ([]T, bool) {
	if len(values) <= limit {
		return values, false
	}
	return values[:limit], true
}

func lastItems[T any](values []T, limit int) ([]T, bool) {
	if len(values) <= limit {
		return values, false
	}
	return values[len(values)-limit:], true
}

func limitSlice[T any](values []T, limit int) []T {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}
