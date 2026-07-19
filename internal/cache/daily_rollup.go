package cache

import (
	"context"
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
)

// dailyBucketExprs pairs the SQL expressions that map an hourly rollup row
// and a partial-hour message_index row onto the same trend bucket key. Both
// grains are UTC, so whole UTC hours always compose exactly into UTC days.
type dailyBucketExprs struct {
	rollup string // over overview_hourly*.bucket_start_ms
	msg    string // over message_index.time_created_ms
}

var (
	dayBucketExprs = dailyBucketExprs{
		rollup: `DATE(bucket_start_ms / 1000, 'unixepoch')`,
		msg:    `DATE(time_created_ms / 1000, 'unixepoch')`,
	}
	hourBucketExprs = dailyBucketExprs{
		rollup: `bucket_start_ms`,
		msg:    `(time_created_ms / 3600000) * 3600000`,
	}
)

// dailyBucketTotals sums messages, cost, and tokens per trend bucket from the
// overview_hourly rollup, folding in at most two partial-hour edges from
// message_index. Replaces the per-message window scan.
func dailyBucketTotals[K comparable](ctx context.Context, s *Store, sourceID string, parts overviewWindowParts, exprs dailyBucketExprs, f modelFilter) (map[K]*stats.DayStats, error) {
	rollupWhere, rollupArgs := f.rollupWhere()
	msgWhere, msgArgs := f.messageWhere()
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		if f.active() {
			selects = append(selects, `
				SELECT `+exprs.rollup+` AS bucket, messages, cost, input_tokens, output_tokens,
					reasoning_tokens, cache_read_tokens, cache_write_tokens
				FROM hourly_usage
				WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`+rollupWhere)
			args = append(append(args, sourceID, parts.fullStart, parts.fullEnd), rollupArgs...)
		} else {
			selects = append(selects, `
				SELECT `+exprs.rollup+` AS bucket, messages, cost, input_tokens, output_tokens,
					reasoning_tokens, cache_read_tokens, cache_write_tokens
				FROM overview_hourly
				WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
			`)
			args = append(args, sourceID, parts.fullStart, parts.fullEnd)
		}
	}
	for _, edge := range parts.edges {
		if f.active() {
			selects = append(selects, `
				SELECT `+exprs.msg+` AS bucket, 1, COALESCE(cost, 0), COALESCE(model_input_tokens, 0),
					COALESCE(model_output_tokens, 0), COALESCE(model_reasoning_tokens, 0),
					COALESCE(model_cache_read_tokens, 0), COALESCE(model_cache_write_tokens, 0)
				FROM message_index
				WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere)
			args = append(append(args, sourceID, edge.start, edge.end), msgArgs...)
		} else {
			selects = append(selects, `
				SELECT `+exprs.msg+` AS bucket, 1, COALESCE(cost, 0), COALESCE(input_tokens, 0),
					COALESCE(output_tokens, 0), COALESCE(reasoning_tokens, 0),
					COALESCE(cache_read_tokens, 0), COALESCE(cache_write_tokens, 0)
				FROM message_index
				WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			`)
			args = append(args, sourceID, edge.start, edge.end)
		}
	}
	byBucket := make(map[K]*stats.DayStats)
	if len(selects) == 0 {
		return byBucket, nil
	}
	query := `
		SELECT bucket, SUM(messages), COALESCE(SUM(cost), 0), COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY bucket
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read daily bucket totals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket K
		d := &stats.DayStats{SourceID: sourceID}
		var cacheRead, cacheWrite int64
		if err := rows.Scan(&bucket, &d.Messages, &d.Cost, &d.Tokens.Input, &d.Tokens.Output, &d.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
			return nil, fmt.Errorf("scan daily bucket totals: %w", err)
		}
		d.Tokens.Cache.Read = cacheRead
		d.Tokens.Cache.Write = cacheWrite
		byBucket[bucket] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily bucket totals: %w", err)
	}
	return byBucket, nil
}

// dailyBucketSessions counts distinct sessions per trend bucket via the
// overview_hourly_sessions set table plus partial-hour edges. Buckets contain
// whole hours, so the union stays exact across the rollup/edge boundary.
func dailyBucketSessions[K comparable](ctx context.Context, s *Store, sourceID string, parts overviewWindowParts, exprs dailyBucketExprs, f modelFilter) (map[K]int64, error) {
	rollupWhere, rollupArgs := f.rollupWhere()
	msgWhere, msgArgs := f.messageWhere()
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		if f.active() {
			selects = append(selects, `
				SELECT `+exprs.rollup+` AS bucket, session_id
				FROM hourly_model_sessions
				WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`+rollupWhere)
			args = append(append(args, sourceID, parts.fullStart, parts.fullEnd), rollupArgs...)
		} else {
			selects = append(selects, `
				SELECT `+exprs.rollup+` AS bucket, session_id
				FROM overview_hourly_sessions
				WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
			`)
			args = append(args, sourceID, parts.fullStart, parts.fullEnd)
		}
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT `+exprs.msg+` AS bucket, session_id
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere)
		args = append(append(args, sourceID, edge.start, edge.end), msgArgs...)
	}
	byBucket := make(map[K]int64)
	if len(selects) == 0 {
		return byBucket, nil
	}
	query := `
		SELECT bucket, COUNT(DISTINCT session_id)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY bucket
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read daily bucket sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket K
		var count int64
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, fmt.Errorf("scan daily bucket sessions: %w", err)
		}
		byBucket[bucket] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily bucket sessions: %w", err)
	}
	return byBucket, nil
}

// dailyBucketCosts folds assistant cost-status counts per trend bucket from
// overview_hourly_cost plus partial-hour edges.
func dailyBucketCosts[K comparable](ctx context.Context, s *Store, sourceID string, parts overviewWindowParts, exprs dailyBucketExprs, f modelFilter) (map[K]*costCounts, error) {
	rollupWhere, rollupArgs := f.rollupWhere()
	msgWhere, msgArgs := f.messageWhere()
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		if f.active() {
			selects = append(selects, `
				SELECT `+exprs.rollup+` AS bucket, cost_status, messages
				FROM hourly_model_cost
				WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?`+rollupWhere)
			args = append(append(args, sourceID, parts.fullStart, parts.fullEnd), rollupArgs...)
		} else {
			selects = append(selects, `
				SELECT `+exprs.rollup+` AS bucket, cost_status, messages
				FROM overview_hourly_cost
				WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
			`)
			args = append(args, sourceID, parts.fullStart, parts.fullEnd)
		}
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT `+exprs.msg+` AS bucket, COALESCE(cost_status, '') AS cost_status, COUNT(*) AS messages
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ? AND role = 'assistant'`+msgWhere+`
			GROUP BY 1, 2
		`)
		args = append(append(args, sourceID, edge.start, edge.end), msgArgs...)
	}
	byBucket := make(map[K]*costCounts)
	if len(selects) == 0 {
		return byBucket, nil
	}
	query := `
		SELECT bucket, cost_status, SUM(messages)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY bucket, cost_status
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read daily bucket cost status: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket K
		var statusText string
		var count int64
		if err := rows.Scan(&bucket, &statusText, &count); err != nil {
			return nil, fmt.Errorf("scan daily bucket cost status: %w", err)
		}
		counts := byBucket[bucket]
		if counts == nil {
			counts = &costCounts{}
			byBucket[bucket] = counts
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily bucket cost status: %w", err)
	}
	return byBucket, nil
}

// dailyBucketStats runs the three per-bucket passes and stitches sessions and
// cost metadata onto the totals.
func dailyBucketStats[K comparable](ctx context.Context, s *Store, sourceID string, parts overviewWindowParts, exprs dailyBucketExprs, f modelFilter) (map[K]*stats.DayStats, error) {
	totals, err := dailyBucketTotals[K](ctx, s, sourceID, parts, exprs, f)
	if err != nil {
		return nil, err
	}
	sessions, err := dailyBucketSessions[K](ctx, s, sourceID, parts, exprs, f)
	if err != nil {
		return nil, err
	}
	costs, err := dailyBucketCosts[K](ctx, s, sourceID, parts, exprs, f)
	if err != nil {
		return nil, err
	}
	for bucket, d := range totals {
		d.Sessions = sessions[bucket]
		if counts := costs[bucket]; counts != nil {
			d.CostStatus, d.CostProvenance = counts.result()
		}
	}
	return totals, nil
}
