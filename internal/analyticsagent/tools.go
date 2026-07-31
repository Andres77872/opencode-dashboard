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
	// Published model, provider, and tool identifiers are short; anything
	// longer is not a product name.
	maxPublicIdentifierBytes = 96
	maxProjectNameBytes      = 64
)

var errInvalidToolInput = errors.New("invalid analytics tool input")

type ToolRegistry struct {
	registry       *source.Registry
	projectRefKey  []byte
	cacheIntegrity CacheIntegrityProvider
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
	Limit  *int   `json:"limit,omitempty"`
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
	Limit       *int   `json:"limit,omitempty"`
}

type dimensionTrendArgs struct {
	Source      string `json:"source"`
	Dimension   string `json:"dimension"`
	Period      string `json:"period,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Granularity string `json:"granularity,omitempty"`
	Limit       *int   `json:"limit,omitempty"`
}

type sessionUsageArgs struct {
	Source string `json:"source"`
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
	Sort   string `json:"sort,omitempty"`
}

type aggregateArgs struct {
	Period       string `json:"period,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	Limit        *int   `json:"limit,omitempty"`
	IncludeTrend bool   `json:"include_trend,omitempty"`
	TrendLimit   *int   `json:"trend_limit,omitempty"`
}

func NewToolRegistry(registry *source.Registry) *ToolRegistry {
	return NewToolRegistryWithCache(registry, nil)
}

func NewToolRegistryWithCache(registry *source.Registry, cacheIntegrity CacheIntegrityProvider) *ToolRegistry {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Fail privacy-safe: project metrics remain usable but uncorrelated if the
		// operating system cannot provide entropy for an in-memory pseudonym key.
		key = nil
	}
	return &ToolRegistry{registry: registry, projectRefKey: key, cacheIntegrity: cacheIntegrity}
}

// DefinitionsFor returns the definitions for an agent's tool allowlist, in the
// registry's canonical order. Names outside the registry are ignored, so a
// roster typo can only reduce an agent's reach, never widen it.
func (r *ToolRegistry) DefinitionsFor(allowed []string) []ToolDefinition {
	permitted := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		permitted[name] = struct{}{}
	}
	all := r.Definitions()
	result := make([]ToolDefinition, 0, len(all))
	for _, definition := range all {
		if _, ok := permitted[definition.Name]; ok {
			result = append(result, definition)
		}
	}
	return result
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_sources",
			Description: "List registered analytics sources, availability, safe capabilities, and cost policy. Call this before choosing an exact source ID.",
			Parameters:  objectSchema(nil, nil, "This tool takes no arguments."),
		},
		{
			Name:        "get_overview",
			Description: "Get source-specific sessions, transcript messages, outbound assistant/API requests, tokens, days, and source-specific cost with provenance. Kimi results can include request-accounting coverage and unavailable-usage counts.",
			Parameters:  sourceRangeSchema(false, false),
		},
		{
			Name:        "get_source_integrity",
			Description: "Audit aggregate source availability, ingestion, request accounting, cost evidence, and sanitized cache freshness for one source or all registered sources. Returns no request/session identifiers, paths, transcripts, event timestamps, or raw errors.",
			Parameters:  integritySchema(),
		},
		{
			Name:        "get_cross_source_overview",
			Description: "Compare every available source, including additive outbound request totals. Combined totals omit cost; source-specific costs retain provenance and must never be added. Top-ranking truncation is reported per dimension. With include_trend=true, use a bounded period because all-time time series are unavailable.",
			Parameters:  crossSourceSchema(),
		},
		{
			Name:        "get_daily_usage",
			Description: "Get a bounded daily or hourly aggregate time series for one source, including distinct transcript-message and outbound-request counts. The default returns the latest 120 buckets and reports truncated=true when earlier buckets were omitted. Use requests for API-call/attempt questions.",
			Parameters:  dailySchema(false),
		},
		{
			Name:        "get_usage_trend_by_dimension",
			Description: "Get a bounded daily or hourly series grouped by model, tool, or project. Counts are outbound assistant/API requests associated with the dimension, not tool invocations or outcomes. The default returns the latest 120 buckets and reports truncated=true when incomplete.",
			Parameters:  dailySchema(true),
		},
		{
			Name:        "get_session_usage",
			Description: "Rank coding sessions by relative recency, cost, or message volume using opaque session references and aggregate metrics. Exact activity timestamps, session titles, prompts, and transcripts are never returned. The default returns the first 10 newest sessions.",
			Parameters:  sessionSchema(),
		},
		{
			Name:        "get_model_usage",
			Description: "Rank models for one explicit source by outbound assistant/API requests, tokens, sessions, and source-specific cost provenance. The requests field is the unambiguous request count; messages is retained for compatibility.",
			Parameters:  sourceRangeSchema(true, false),
		},
		{
			Name:        "get_tool_usage",
			Description: "Rank coding-assistant tool names by invocations, successes, failures, and sessions. Compare explicit windows with this tool for reliability changes; dimension trends do not contain invocation outcomes. No tool input or output is available.",
			Parameters:  sourceRangeSchema(true, false),
		},
		{
			Name:        "get_project_usage",
			Description: "Rank projects by aggregate metrics. Each row carries a stable project_ref and, when it is safe to read, the project's own name without its directories. Local project IDs and filesystem paths are never returned.",
			Parameters:  sourceRangeSchema(true, false),
		},
	}
}

const (
	sourceIDPattern = `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`
	datePattern     = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
)

func objectSchema(properties map[string]any, required []string, description string) json.RawMessage {
	return objectSchemaWithAlternatives(properties, required, description, nil)
}

func timeObjectSchema(properties map[string]any, required []string, description string) json.RawMessage {
	return objectSchemaWithAlternatives(properties, required, description, timeModeAlternatives())
}

func objectSchemaWithAlternatives(properties map[string]any, required []string, description string, alternatives []any) json.RawMessage {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	if description != "" {
		schema["description"] = description
	}
	if len(alternatives) > 0 {
		schema["oneOf"] = alternatives
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	}
	return json.RawMessage(encoded)
}

// timeModeAlternatives makes the mutually exclusive time contract structural,
// not merely prose. Keeping DEFAULT separate from PRESET gives the model three
// unambiguous construction templates. CUSTOM requires from, so to cannot appear
// alone.
func timeModeAlternatives() []any {
	return []any{
		map[string]any{
			"title":       "PRESET time mode",
			"description": "Require period by itself. Never include from or to in this mode.",
			"required":    []string{"period"},
			"not": map[string]any{"anyOf": []any{
				map[string]any{"required": []string{"from"}},
				map[string]any{"required": []string{"to"}},
			}},
		},
		map[string]any{
			"title":       "CUSTOM time mode",
			"description": "Require from, optionally include to, and never include period.",
			"required":    []string{"from"},
			"not":         map[string]any{"required": []string{"period"}},
		},
		map[string]any{
			"title":       "DEFAULT " + stats.DefaultPeriodPreset + " time mode",
			"description": "Omit period, from, and to. The backend applies the " + stats.DefaultPeriodPreset + " default.",
			"not": map[string]any{"anyOf": []any{
				map[string]any{"required": []string{"period"}},
				map[string]any{"required": []string{"from"}},
				map[string]any{"required": []string{"to"}},
			}},
		},
	}
}

func sourceProperty() map[string]any {
	return map[string]any{
		"type": "string", "pattern": sourceIDPattern, "minLength": 1, "maxLength": 64,
		"description": "Exact source ID returned by list_sources.",
	}
}

func periodProperties(boundedSeries bool) map[string]any {
	presets := stats.SupportedPeriodPresets()
	if boundedSeries {
		bounded := make([]string, 0, len(presets)-1)
		for _, preset := range presets {
			if preset != "all" {
				bounded = append(bounded, preset)
			}
		}
		presets = bounded
	}
	return map[string]any{
		"period": map[string]any{
			"type": "string", "enum": presets, "examples": []string{"7d", "30d"},
			"description": "PRESET time mode only. Use one exact preset and NEVER include from or to in the same call. Omit all time fields to use the " + stats.DefaultPeriodPreset + " default. Hour presets are rolling UTC windows; day presets are UTC calendar-aligned.",
		},
		"from": map[string]any{
			"type": "string", "pattern": datePattern, "examples": []string{"2026-07-01"},
			"description": "CUSTOM time mode only. Inclusive UTC start date in YYYY-MM-DD format. When from is present, period MUST be absent.",
		},
		"to": map[string]any{
			"type": "string", "pattern": datePattern, "examples": []string{"2026-07-31"},
			"description": "CUSTOM time mode only. Inclusive UTC end date in YYYY-MM-DD format. Requires from and forbids period; omit to to continue through now.",
		},
	}
}

func timeModeSchemaDescription(detail string) string {
	description := "TIME MODE IS EXCLUSIVE. PRESET: send period only. CUSTOM: send required from plus optional to and no period. DEFAULT: omit period, from, and to; the backend applies " + stats.DefaultPeriodPreset + "."
	if detail != "" {
		description += " " + detail
	}
	return description
}

func sourceRangeSchema(includeLimit, boundedSeries bool) json.RawMessage {
	properties := periodProperties(boundedSeries)
	properties["source"] = sourceProperty()
	if includeLimit {
		properties["limit"] = map[string]any{
			"type": "integer", "minimum": 1, "maximum": maxListLimit, "default": defaultListLimit,
			"description": "Maximum ranked rows to return. Results report truncated=true when more rows exist.",
		}
	}
	return timeObjectSchema(properties, []string{"source"}, timeModeSchemaDescription(""))
}

func integritySchema() json.RawMessage {
	properties := periodProperties(false)
	properties["source"] = sourceProperty()
	return timeObjectSchema(properties, nil, timeModeSchemaDescription("Omit source to audit every registered source."))
}

func crossSourceSchema() json.RawMessage {
	properties := periodProperties(false)
	properties["limit"] = map[string]any{
		"type": "integer", "minimum": 1, "maximum": maxAggregateTopN, "default": defaultListLimit,
		"description": "Maximum top models, tools, and projects to return. Each ranking reports its own *_truncated flag when more rows exist.",
	}
	properties["include_trend"] = map[string]any{
		"type": "boolean", "default": false,
		"description": "Include per-source time series. When true, all is invalid and the range must fit at most 1000 buckets.",
	}
	properties["trend_limit"] = map[string]any{
		"type": "integer", "minimum": 1, "maximum": maxDailyLimit, "default": 90,
		"description": "Most recent trend buckets retained per source; trend_truncated=true means earlier buckets were omitted.",
	}
	return timeObjectSchema(properties, nil, timeModeSchemaDescription("With include_trend=true, use a bounded period or custom range."))
}

func dailySchema(withDimension bool) json.RawMessage {
	properties := periodProperties(true)
	properties["source"] = sourceProperty()
	properties["granularity"] = map[string]any{
		"type": "string", "enum": []string{"day", "hour"},
		"description": "Bucket size. Omit for automatic selection: hour for 1d and hour presets, otherwise day.",
	}
	properties["limit"] = map[string]any{
		"type": "integer", "minimum": 1, "maximum": maxDailyLimit, "default": defaultDailyLimit,
		"description": "Most recent buckets retained. Dimension trends retain every row in each selected bucket. Set high enough for complete coverage; truncated=true means earlier buckets were omitted.",
	}
	required := []string{"source"}
	if withDimension {
		properties["dimension"] = map[string]any{
			"type": "string", "enum": []string{"model", "tool", "project"},
			"description": "Dimension to group. Tool rows count associated assistant/API requests, not tool invocations or outcomes.",
		}
		required = append(required, "dimension")
	}
	return timeObjectSchema(properties, required, timeModeSchemaDescription("All-time series are unavailable; the selected granularity may contain at most 1000 buckets."))
}

func sessionSchema() json.RawMessage {
	properties := periodProperties(false)
	properties["source"] = sourceProperty()
	properties["limit"] = map[string]any{
		"type": "integer", "minimum": 1, "maximum": maxListLimit, "default": defaultListLimit,
		"description": "Maximum ranked sessions to return.",
	}
	properties["sort"] = map[string]any{
		"type": "string", "enum": []string{"newest", "oldest", "cost", "messages"}, "default": "newest",
		"description": "Ranking order. Recency sorts reveal relative ordering only, never exact timestamps.",
	}
	return timeObjectSchema(properties, []string{"source"}, timeModeSchemaDescription(""))
}

func isAnalyticsToolName(name string) bool {
	switch name {
	case "list_sources", "get_overview", "get_cross_source_overview", "get_daily_usage",
		"get_usage_trend_by_dimension", "get_session_usage",
		"get_model_usage", "get_tool_usage", "get_project_usage", "get_source_integrity":
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
	prepared, err := r.Prepare(name, arguments)
	if err != nil {
		return toolExecutionError(err)
	}
	return r.executePrepared(ctx, name, prepared)
}

// executePrepared executes arguments that already passed Prepare. Keeping this
// boundary explicit lets the runner validate before it streams or persists a
// call while direct registry callers receive the same behavior through Execute.
func (r *ToolRegistry) executePrepared(ctx context.Context, name string, arguments json.RawMessage) json.RawMessage {
	data, err := r.execute(ctx, name, arguments)
	if err != nil {
		return toolExecutionError(err)
	}
	return marshalEnvelope(true, data, nil)
}

func toolExecutionError(err error) json.RawMessage {
	code := "tool_failed"
	message := "The analytics tool failed safely."
	if errors.Is(err, errInvalidToolInput) {
		code = "invalid_arguments"
		message = err.Error()
	}
	return marshalEnvelope(false, nil, &safeToolError{Code: code, Message: message})
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
	case "get_source_integrity":
		var args integrityArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		return r.sourceIntegrity(ctx, args)
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
		// Fetch one sentinel row beyond the public limit so the privacy-safe
		// response can state whether each independent ranking is complete.
		result, err := source.AggregateOverview(ctx, r.registry, pq, source.AggregateOptions{
			IncludeTrend:     args.IncludeTrend,
			TopN:             topN + 1,
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
		return safeCrossOverviewFrom(result, trendLimit, topN, unavailable, r.projectRefKey), nil
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
	return validatePeriodAt(args, time.Now().UTC())
}

func validatePeriodAt(args periodArgs, now time.Time) (stats.PeriodQuery, error) {
	args.Period = strings.TrimSpace(args.Period)
	args.From = strings.TrimSpace(args.From)
	args.To = strings.TrimSpace(args.To)
	if args.Period != "" && args.To != "" && args.From == "" {
		return stats.PeriodQuery{}, invalidInput("time mode conflict: PRESET mode removes to and keeps period; CUSTOM mode removes period and must add the required from date before keeping to")
	}
	if args.Period != "" && (args.From != "" || args.To != "") {
		return stats.PeriodQuery{}, invalidInput("time mode conflict: PRESET mode keeps period and removes from/to; CUSTOM mode keeps required from plus optional to and removes period")
	}
	if args.To != "" && args.From == "" {
		return stats.PeriodQuery{}, invalidInput("to requires from")
	}
	if args.From != "" {
		from, err := time.Parse("2006-01-02", args.From)
		if err != nil {
			return stats.PeriodQuery{}, invalidInput("from must use YYYY-MM-DD")
		}
		if from.After(now.UTC()) {
			return stats.PeriodQuery{}, invalidInput("from cannot be in the future")
		}
		if args.To != "" {
			to, err := time.Parse("2006-01-02", args.To)
			if err != nil {
				return stats.PeriodQuery{}, invalidInput("to must use YYYY-MM-DD")
			}
			if to.After(now.UTC()) || from.After(to) {
				return stats.PeriodQuery{}, invalidInput("to must be current or past and not before from")
			}
		}
		return stats.PeriodQuery{From: args.From, To: args.To}, nil
	}
	if args.Period == "" {
		args.Period = stats.DefaultPeriodPreset
	}
	if stats.IsSupportedPeriodPreset(args.Period) {
		return stats.PeriodQuery{Period: args.Period}, nil
	}
	return stats.PeriodQuery{}, invalidInput(fmt.Sprintf(
		"period must be one of %s; for custom dates omit period and use from/to in YYYY-MM-DD format",
		strings.Join(stats.SupportedPeriodPresets(), ", "),
	))
}

// validateBucketWindow prevents provider-generated ranges from reaching source
// implementations that materialize one empty bucket per day/hour before the
// result can be truncated. Aggregate-only tools may still query all time; only
// time-series allocation is capped here.
func validateBucketWindow(args periodArgs, granularity string, maximum int) error {
	if maximum <= 0 {
		return invalidInput("time-series bucket limit is invalid")
	}
	if granularity != "day" && granularity != "hour" {
		return invalidInput("granularity must be day or hour")
	}

	pq, err := validatePeriod(args)
	if err != nil {
		return err
	}
	if pq.Period == "all" {
		return invalidInput("all-time time series are not available; use an all-time overview or a bounded trend period")
	}
	window, err := stats.ComputePeriodWindowFromQuery(context.Background(), nil, pq)
	if err != nil {
		return invalidInput("time-series period could not be resolved")
	}
	start := time.UnixMilli(window.StartMs).UTC()
	end := time.UnixMilli(window.EndMs).UTC()

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
	return string(stats.ResolveGranularity(stats.PeriodQuery{Period: strings.TrimSpace(period)}))
}

func validatedLimit(value *int, defaultValue, maximum int) (int, error) {
	if value == nil {
		return defaultValue, nil
	}
	if *value < 1 || *value > maximum {
		return 0, invalidInput(fmt.Sprintf("limit must be between 1 and %d", maximum))
	}
	return *value, nil
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err == nil {
		for field, value := range fields {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return invalidInput(field + " cannot be null; omit it or use the type required by the tool schema")
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &typeErr):
			field := strings.TrimSpace(typeErr.Field)
			if field == "" {
				field = "argument"
			}
			return invalidInput(fmt.Sprintf("%s must have the type required by the tool schema", field))
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return invalidInput(strings.TrimPrefix(err.Error(), "json: "))
		default:
			return invalidInput("arguments must be a valid JSON object matching the tool schema")
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalidInput("arguments must contain one JSON object")
	}
	return nil
}

func invalidInput(message string) error {
	return &invalidToolInputError{message: message}
}

type invalidToolInputError struct{ message string }

func (e *invalidToolInputError) Error() string { return e.message }
func (e *invalidToolInputError) Unwrap() error { return errInvalidToolInput }

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
	ID            string                   `json:"id"`
	Available     bool                     `json:"available"`
	Capabilities  []string                 `json:"capabilities"`
	CostPolicy    safeCostPolicy           `json:"cost_policy,omitempty"`
	DataIntegrity *safeSourceScanIntegrity `json:"data_integrity,omitempty"`
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
	UsageRecorded           int64                         `json:"usage_recorded"`
	UsageRecovered          int64                         `json:"usage_recovered"`
	UsageUnavailable        int64                         `json:"usage_unavailable"`
	UsageUnavailableReasons stats.UsageUnavailableReasons `json:"usage_unavailable_reasons"`
	TraceCoverage           stats.TraceCoverage           `json:"trace_coverage"`
}

func (r *ToolRegistry) listSources(ctx context.Context) []safeSourceInfo {
	infos := r.registry.List(ctx)
	result := make([]safeSourceInfo, 0, len(infos))
	for _, info := range infos {
		if !isSafeSourceID(string(info.ID)) {
			continue
		}
		item := safeSourceInfo{
			ID:           string(info.ID),
			Available:    info.Available,
			Capabilities: safeCapabilities(info.Capabilities),
			CostPolicy:   safeCostPolicyFrom(info.CostPolicy, r.projectRefKey),
		}
		if scan, assessed := safeScanIntegrity(info); assessed {
			item.DataIntegrity = &scan
		}
		result = append(result, item)
	}
	return result
}

func safeCapabilities(values []string) []string {
	allowed := map[string]bool{"overview": true, "daily": true, "models": true, "tools": true, "projects": true, "sessions": true}
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
	Rank int `json:"rank"`
	// ProjectRef is the stable pseudonym; ProjectName is the leaf name only,
	// present when it is safe to read, so rankings can be named in a report.
	ProjectRef     string              `json:"project_ref"`
	ProjectName    string              `json:"project_name,omitempty"`
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
	TopModelsTruncated   bool                    `json:"top_models_truncated,omitempty"`
	TopProjects          []safeProjectMetric     `json:"top_projects"`
	TopProjectsTruncated bool                    `json:"top_projects_truncated,omitempty"`
	TopTools             []safeTool              `json:"top_tools"`
	TopToolsTruncated    bool                    `json:"top_tools_truncated,omitempty"`
	UnavailableSources   []string                `json:"unavailable_sources,omitempty"`
	IncompleteDimensions []safeDimensionOmission `json:"incomplete_dimensions,omitempty"`
}

type safeDimensionOmission struct {
	SourceID  string `json:"source_id"`
	Dimension string `json:"dimension"`
}

func safeCrossOverviewFrom(value source.AllSourcesOverview, trendLimit, topN int, unavailable []string, projectRefKey []byte) safeCrossOverview {
	result := safeCrossOverview{
		CombinedTotals: safeCombinedTotals{
			Sessions: value.Total.Sessions, Messages: value.Total.Messages, Requests: value.Total.Requests,
			Tokens: value.Total.Tokens, Days: value.Total.Days,
		},
		MessagesPerSession:   value.MessagesPerSession,
		TokensPerMessage:     value.TokensPerMessage,
		UnavailableSources:   safeSourceRefs(projectRefKey, unavailable),
		TopModelsTruncated:   len(value.TopModels) > topN,
		TopProjectsTruncated: len(value.TopProjects) > topN,
		TopToolsTruncated:    len(value.TopTools) > topN,
	}
	for _, item := range limitSlice(value.TopTools, topN) {
		result.TopTools = append(result.TopTools, safeToolFrom(item, projectRefKey))
	}
	for _, item := range limitSlice(value.TopModels, topN) {
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
	for i, item := range limitSlice(value.TopProjects, topN) {
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
	Date string `json:"date"`
	// DimensionKey is readable for models and tools and an opaque reference for
	// projects, which carry their leaf name in DimensionName when it is safe.
	DimensionKey   string              `json:"dimension_key"`
	DimensionName  string              `json:"dimension_name,omitempty"`
	Sessions       int64               `json:"sessions"`
	Requests       int64               `json:"requests"`
	Cost           float64             `json:"cost"`
	Tokens         stats.TokenStats    `json:"tokens"`
	CostStatus     stats.CostStatus    `json:"cost_status,omitempty"`
	CostProvenance *safeCostProvenance `json:"cost_provenance,omitempty"`
}

func safeDimensionTrendFrom(value stats.DailyDimensionStats, dimension string, limit int, key []byte) safeDimensionTrend {
	rawDays, truncated := lastDimensionBuckets(value.Days, limit)
	entries := make([]safeDimensionDay, 0, len(rawDays))
	for _, day := range rawDays {
		sourceID := day.SourceID
		if sourceID == "" {
			sourceID = value.SourceID
		}
		dimensionKey := ""
		dimensionName := ""
		switch dimension {
		case "model":
			dimensionKey = safeOutboundIdentifier(key, "model", day.Dimension)
		case "tool":
			dimensionKey = safeOutboundIdentifier(key, "tool", day.Dimension)
		default:
			dimensionKey = opaqueProjectRef(key, sourceID, day.Dimension)
			dimensionName = safeProjectName(day.Dimension)
		}
		entries = append(entries, safeDimensionDay{
			Date: safeDate(day.Date), DimensionKey: dimensionKey, DimensionName: dimensionName,
			Sessions: day.Sessions, Requests: day.Messages, Cost: day.Cost, Tokens: day.Tokens,
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

// lastDimensionBuckets retains whole time buckets. Dimension rows are sorted
// within a bucket by their own metric, so slicing rows directly could drop the
// leading dimension from the oldest retained bucket and make a partial bucket
// look complete.
func lastDimensionBuckets(values []stats.DimensionDayStats, limit int) ([]stats.DimensionDayStats, bool) {
	if limit <= 0 || len(values) == 0 {
		return []stats.DimensionDayStats{}, len(values) > 0
	}
	retainedDates := make(map[string]struct{}, limit)
	for index := len(values) - 1; index >= 0; index-- {
		date := values[index].Date
		if _, retained := retainedDates[date]; retained {
			continue
		}
		if len(retainedDates) == limit {
			break
		}
		retainedDates[date] = struct{}{}
	}
	result := make([]stats.DimensionDayStats, 0, len(values))
	truncated := false
	for _, value := range values {
		if _, retained := retainedDates[value.Date]; retained {
			result = append(result, value)
		} else {
			truncated = true
		}
	}
	return result, truncated
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
	Rank int `json:"rank"`
	// Session titles are the user's own first prompt and never travel; the
	// project a session belongs to is named the same way rankings are.
	SessionRef     string              `json:"session_ref"`
	ProjectRef     string              `json:"project_ref,omitempty"`
	ProjectName    string              `json:"project_name,omitempty"`
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
			Rank:        i + 1,
			SessionRef:  opaqueValueRef(key, "session", sourceID+"\x00"+session.ID),
			ProjectRef:  projectRef,
			ProjectName: safeProjectName(session.ProjectName),
			Messages:    session.MessageCount, Cost: session.Cost,
			CostStatus: safeCostStatus(session.CostStatus), CostProvenance: safeProvenance(session.CostProvenance, key),
		})
	}
	return result
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
		Rank: rank, ProjectRef: opaqueProjectRef(projectRefKey, value.SourceID, value.ProjectID),
		ProjectName: safeProjectName(value.ProjectName), SourceID: safeSourceRef(projectRefKey, value.SourceID),
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
		UsageRecorded:           value.UsageRecorded,
		UsageRecovered:          value.UsageRecovered,
		UsageUnavailable:        value.UsageUnavailable,
		UsageUnavailableReasons: stats.NormalizeUsageUnavailableReasons(value.UsageUnavailable, value.UsageUnavailableReasons),
		TraceCoverage:           coverage,
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

// safeOutboundIdentifier decides whether a model, provider, tool, or pricing
// identifier travels readable. These are published product identifiers, and
// naming them is the entire point of a usage report: a ranking of
// "model-7f3a9b…" answers nothing. Values are therefore passed through whenever
// they have the shape of an identifier, and pseudonymized only when they look
// like local state a report has no business quoting.
//
// A fixed allowlist is deliberately not used here: model and tool catalogs
// change constantly, and an unknown-but-ordinary name like a new model release
// would otherwise be reported as an unreadable pseudonym.
func safeOutboundIdentifier(key []byte, prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isPublicIdentifier(value) {
		return value
	}
	return opaqueValueRef(key, prefix, value)
}

// isPublicIdentifier accepts the shape of a published identifier, such as
// "gpt-5.6-sol", "claude-opus-5", "anthropic/claude-opus-5", "MiniMax-M3", or
// "mcp__linear__create_issue". It rejects anything shaped like local state:
// filesystem paths, home directories, URLs, values containing whitespace or
// control characters, and oversized values.
func isPublicIdentifier(value string) bool {
	if len(value) == 0 || len(value) > maxPublicIdentifierBytes {
		return false
	}
	// A path, a URL, a relative reference, or a home directory is local
	// context, never a product name.
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.HasPrefix(value, ".") ||
		strings.Contains(value, "..") || strings.Contains(value, "://") || strings.Count(value, "/") > 2 {
		return false
	}
	for _, char := range value {
		alphaNumeric := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if !alphaNumeric && !strings.ContainsRune("._:@+-/", char) {
			return false
		}
	}
	return true
}

// safeProjectName exposes only the leaf name of a project, never the directory
// structure that led to it, so a report can say which project dominates without
// disclosing where it lives. An unusable or suspicious name is dropped, leaving
// the opaque reference as the only handle.
func safeProjectName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Local project identities are frequently paths; keep the leaf only.
	for _, separator := range []string{"/", "\\"} {
		if index := strings.LastIndex(value, separator); index >= 0 {
			value = value[index+1:]
		}
	}
	value = strings.TrimSpace(strings.Trim(value, "-_. "))
	if value == "" || len(value) > maxProjectNameBytes {
		return ""
	}
	for _, char := range value {
		alphaNumeric := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if !alphaNumeric && !strings.ContainsRune("._ +-", char) {
			return ""
		}
	}
	return value
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
