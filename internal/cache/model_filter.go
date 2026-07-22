package cache

import (
	"context"
	"strings"

	"opencode-dashboard/internal/stats"
)

// modelFilter restricts Overview, Daily, and Messages to assistant messages
// attributed to one model (and optionally one provider). Only the cache-backed
// read path honors it: filtered totals come from the model rollups, so token
// sums are model-attributed tokens, and the live gap is filtered in Go after
// the fetch.
type modelFilter struct {
	model    string
	provider string
}

func filterFromPQ(pq stats.PeriodQuery) modelFilter {
	return modelFilter{model: strings.TrimSpace(pq.Model), provider: strings.TrimSpace(pq.Provider)}
}

func (f modelFilter) active() bool {
	return f.model != "" || f.provider != ""
}

// messageWhere is the predicate for message_index rows, matching the
// population the model rollups are built from. Empty when the filter is
// inactive so unfiltered paths keep their all-roles population.
func (f modelFilter) messageWhere() (string, []any) {
	if !f.active() {
		return "", nil
	}
	where := ` AND role = 'assistant' AND COALESCE(model_id, '') != ''`
	args := make([]any, 0, 2)
	if f.model != "" {
		where += ` AND COALESCE(model_id, '') = ?`
		args = append(args, f.model)
	}
	if f.provider != "" {
		where += ` AND COALESCE(provider_id, '') = ?`
		args = append(args, f.provider)
	}
	return where, args
}

// rollupWhere is the predicate for hourly_usage / hourly_model_* rows, which
// already contain only assistant messages with a model id.
func (f modelFilter) rollupWhere() (string, []any) {
	where := ``
	args := make([]any, 0, 2)
	if f.model != "" {
		where += ` AND model_id = ?`
		args = append(args, f.model)
	}
	if f.provider != "" {
		where += ` AND provider_id = ?`
		args = append(args, f.provider)
	}
	return where, args
}

func (f modelFilter) matches(entry stats.MessageEntry) bool {
	if !f.active() {
		return true
	}
	if entry.Role != "assistant" || entry.ModelID == "" {
		return false
	}
	if f.model != "" && entry.ModelID != f.model {
		return false
	}
	if f.provider != "" && entry.ProviderID != f.provider {
		return false
	}
	return true
}

// overviewFromModelRollups is the model-filtered analog of overviewFromRollups:
// totals from hourly_usage, distinct sessions from hourly_model_sessions, cost
// status from hourly_model_cost, partial-hour edges from message_index using
// the model-attributed token columns.
func (s *Store) overviewFromModelRollups(ctx context.Context, sourceID string, startMs, endMs int64, f modelFilter) (stats.OverviewStats, error) {
	result := stats.OverviewStats{SourceID: sourceID}
	if endMs <= startMs {
		return result, nil
	}
	parts := splitOverviewMillis(startMs, endMs)
	rollupWhere, rollupArgs := f.rollupWhere()
	msgWhere, msgArgs := f.messageWhere()
	var usageRecorded, usageRecovered, usageUnavailable int64
	var traceObserved, traceInferred int64

	scanTotals := func(query string, args ...any) error {
		var messages, requests, recorded, recovered, unavailable, observed, inferred int64
		var input, output, reasoning, cacheRead, cacheWrite int64
		var cost float64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(
			&messages, &requests, &recorded, &recovered, &unavailable, &observed, &inferred,
			&cost, &input, &output, &reasoning, &cacheRead, &cacheWrite,
		); err != nil {
			return err
		}
		result.Messages += messages
		result.Requests += requests
		usageRecorded += recorded
		usageRecovered += recovered
		usageUnavailable += unavailable
		traceObserved += observed
		traceInferred += inferred
		result.Cost += cost
		result.Tokens.Input += input
		result.Tokens.Output += output
		result.Tokens.Reasoning += reasoning
		result.Tokens.Cache.Read += cacheRead
		result.Tokens.Cache.Write += cacheWrite
		return nil
	}
	if parts.hasFullHours() {
		if err := scanTotals(`
			SELECT
				COALESCE(SUM(messages), 0), COALESCE(SUM(requests), 0),
				COALESCE(SUM(usage_recorded), 0), COALESCE(SUM(usage_recovered), 0),
				COALESCE(SUM(usage_unavailable), 0), COALESCE(SUM(trace_observed), 0),
				COALESCE(SUM(trace_inferred), 0), COALESCE(SUM(cost), 0),
				COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
				COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(SUM(cache_write_tokens), 0)
			FROM hourly_usage
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`+rollupWhere,
			append([]any{sourceID, parts.fullStart, parts.fullEnd}, rollupArgs...)...); err != nil {
			return result, err
		}
	}
	for _, edge := range parts.edges {
		if err := scanTotals(`
			SELECT
				COUNT(*), COUNT(*),
				COALESCE(SUM(CASE WHEN usage_status = 'recorded' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN usage_status = 'recovered' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN usage_status = 'unavailable' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN request_trace = 'observed' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN request_trace = 'inferred' THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(cost), 0),
				COALESCE(SUM(model_input_tokens), 0), COALESCE(SUM(model_output_tokens), 0),
				COALESCE(SUM(model_reasoning_tokens), 0), COALESCE(SUM(model_cache_read_tokens), 0),
				COALESCE(SUM(model_cache_write_tokens), 0)
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere,
			append([]any{sourceID, edge.start, edge.end}, msgArgs...)...); err != nil {
			return result, err
		}
	}
	unknownTrace := result.Requests - traceObserved - traceInferred
	if unknownTrace < 0 {
		unknownTrace = 0
	}
	result.RequestAccounting = stats.NewRequestAccounting(
		usageRecorded, usageRecovered, usageUnavailable,
		traceObserved, traceInferred, unknownTrace,
	)

	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0)
	if parts.hasFullHours() {
		selects = append(selects, `SELECT bucket_start_ms / 86400000 AS day_key, session_id FROM hourly_model_sessions WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`+rollupWhere)
		args = append(append(args, sourceID, parts.fullStart, parts.fullEnd), rollupArgs...)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `SELECT time_created_ms / 86400000 AS day_key, session_id FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere)
		args = append(append(args, sourceID, edge.start, edge.end), msgArgs...)
	}
	if len(selects) > 0 {
		query := `SELECT COUNT(DISTINCT session_id), COUNT(DISTINCT day_key) FROM (` + strings.Join(selects, ` UNION ALL `) + `)`
		var days int
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&result.Sessions, &days); err != nil {
			return result, err
		}
		result.Days = days
	}

	status, provenance, err := s.modelCostSummary(ctx, sourceID, parts, f)
	if err != nil {
		return result, err
	}
	result.CostStatus, result.CostProvenance = status, provenance
	return result, nil
}

// modelCostSummary folds assistant cost-status counts for the filtered
// population from hourly_model_cost plus partial-hour edges.
func (s *Store) modelCostSummary(ctx context.Context, sourceID string, parts overviewWindowParts, f modelFilter) (stats.CostStatus, *stats.CostProvenance, error) {
	rollupWhere, rollupArgs := f.rollupWhere()
	msgWhere, msgArgs := f.messageWhere()
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0)
	if parts.hasFullHours() {
		selects = append(selects, `SELECT cost_status, messages FROM hourly_model_cost WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`+rollupWhere)
		args = append(append(args, sourceID, parts.fullStart, parts.fullEnd), rollupArgs...)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT COALESCE(cost_status, '') AS cost_status, COUNT(*) AS messages
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere+`
			GROUP BY COALESCE(cost_status, '')
		`)
		args = append(append(args, sourceID, edge.start, edge.end), msgArgs...)
	}
	if len(selects) == 0 {
		return "", nil, nil
	}
	query := `
		SELECT cost_status, SUM(messages)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY cost_status
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var counts costCounts
	for rows.Next() {
		var statusText string
		var count int64
		if err := rows.Scan(&statusText, &count); err != nil {
			return "", nil, err
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	status, provenance := counts.result()
	return status, provenance, nil
}
