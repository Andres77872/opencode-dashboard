package cache

import (
	"context"
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
)

const overviewHourMs = int64(60 * 60 * 1000)

type overviewMillisRange struct {
	start int64
	end   int64
}

// overviewWindowParts identifies complete clock hours that can be answered by
// the materialized overview tables and, at most, two partial-hour edges that
// must still be read from message_index. Cache cutoffs are hour-aligned, so
// normal day/all requests have no edges and rolling-hour requests have only a
// small leading fragment.
type overviewWindowParts struct {
	fullStart int64
	fullEnd   int64
	edges     []overviewMillisRange
}

func splitOverviewMillis(startMs, endMs int64) overviewWindowParts {
	var parts overviewWindowParts
	if endMs <= startMs {
		return parts
	}
	firstFull := hourBucketStartMs(startMs)
	if firstFull < startMs {
		firstFull += overviewHourMs
	}
	lastFullEnd := hourBucketStartMs(endMs)
	if firstFull >= lastFullEnd {
		parts.edges = append(parts.edges, overviewMillisRange{start: startMs, end: endMs})
		return parts
	}
	parts.fullStart, parts.fullEnd = firstFull, lastFullEnd
	if startMs < firstFull {
		parts.edges = append(parts.edges, overviewMillisRange{start: startMs, end: firstFull})
	}
	if lastFullEnd < endMs {
		parts.edges = append(parts.edges, overviewMillisRange{start: lastFullEnd, end: endMs})
	}
	return parts
}

func (p overviewWindowParts) hasFullHours() bool {
	return p.fullEnd > p.fullStart
}

func (s *Store) overviewFromRollups(ctx context.Context, sourceID string, startMs, endMs int64) (stats.OverviewStats, error) {
	result := stats.OverviewStats{SourceID: sourceID}
	if endMs <= startMs {
		return result, nil
	}
	parts := splitOverviewMillis(startMs, endMs)
	if err := s.addOverviewTotals(ctx, sourceID, parts, &result); err != nil {
		return result, err
	}
	sessions, err := s.overviewDistinctSessions(ctx, sourceID, parts)
	if err != nil {
		return result, err
	}
	result.Sessions = sessions
	days, err := s.overviewDistinctDays(ctx, sourceID, parts)
	if err != nil {
		return result, err
	}
	result.Days = days
	status, provenance, err := s.overviewCostSummary(ctx, sourceID, parts)
	if err != nil {
		return result, err
	}
	result.CostStatus, result.CostProvenance = status, provenance
	return result, nil
}

func (s *Store) addOverviewTotals(ctx context.Context, sourceID string, parts overviewWindowParts, result *stats.OverviewStats) error {
	type totals struct {
		messages                                        int64
		cost                                            float64
		input, output, reasoning, cacheRead, cacheWrite int64
	}
	add := func(row totals) {
		result.Messages += row.messages
		result.Cost += row.cost
		result.Tokens.Input += row.input
		result.Tokens.Output += row.output
		result.Tokens.Reasoning += row.reasoning
		result.Tokens.Cache.Read += row.cacheRead
		result.Tokens.Cache.Write += row.cacheWrite
	}
	scan := func(query string, args ...any) error {
		var row totals
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(
			&row.messages, &row.cost, &row.input, &row.output, &row.reasoning,
			&row.cacheRead, &row.cacheWrite,
		); err != nil {
			return err
		}
		add(row)
		return nil
	}
	if parts.hasFullHours() {
		if err := scan(`
			SELECT
				COALESCE(SUM(messages), 0), COALESCE(SUM(cost), 0),
				COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
				COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(SUM(cache_write_tokens), 0)
			FROM overview_hourly
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
		`, sourceID, parts.fullStart, parts.fullEnd); err != nil {
			return fmt.Errorf("read overview hourly totals: %w", err)
		}
	}
	for _, edge := range parts.edges {
		if err := scan(`
			SELECT
				COUNT(*), COALESCE(SUM(cost), 0), COALESCE(SUM(input_tokens), 0),
				COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
				COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0)
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		`, sourceID, edge.start, edge.end); err != nil {
			return fmt.Errorf("read overview partial-hour totals: %w", err)
		}
	}
	return nil
}

func (s *Store) overviewDistinctSessions(ctx context.Context, sourceID string, parts overviewWindowParts) (int64, error) {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, `SELECT session_id FROM overview_hourly_sessions WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`)
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `SELECT session_id FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`)
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return 0, nil
	}
	query := `SELECT COUNT(DISTINCT session_id) FROM (` + strings.Join(selects, ` UNION ALL `) + `)`
	var count int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("read overview distinct sessions: %w", err)
	}
	return count, nil
}

func (s *Store) overviewDistinctDays(ctx context.Context, sourceID string, parts overviewWindowParts) (int, error) {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, `SELECT bucket_start_ms / 86400000 AS day_key FROM overview_hourly WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ? AND messages > 0`)
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `SELECT time_created_ms / 86400000 AS day_key FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`)
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return 0, nil
	}
	query := `SELECT COUNT(DISTINCT day_key) FROM (` + strings.Join(selects, ` UNION ALL `) + `)`
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("read overview active days: %w", err)
	}
	return count, nil
}

func (s *Store) overviewCostSummary(ctx context.Context, sourceID string, parts overviewWindowParts) (stats.CostStatus, *stats.CostProvenance, error) {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, `SELECT cost_status, messages FROM overview_hourly_cost WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`)
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT COALESCE(cost_status, '') AS cost_status, COUNT(*) AS messages
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ? AND role = 'assistant'
			GROUP BY COALESCE(cost_status, '')
		`)
		args = append(args, sourceID, edge.start, edge.end)
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
		return "", nil, fmt.Errorf("read overview cost status: %w", err)
	}
	defer rows.Close()
	var counts costCounts
	for rows.Next() {
		var statusText string
		var count int64
		if err := rows.Scan(&statusText, &count); err != nil {
			return "", nil, fmt.Errorf("scan overview cost status: %w", err)
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("iterate overview cost status: %w", err)
	}
	status, provenance := counts.result()
	return status, provenance, nil
}
