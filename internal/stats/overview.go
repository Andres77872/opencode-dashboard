package stats

import (
	"context"

	"opencode-dashboard/internal/store"
)

// OverviewString is a backward-compatible wrapper that accepts a string period.
// It constructs a PeriodQuery and delegates to Overview.
func OverviewString(ctx context.Context, store *store.Store, period string) (OverviewStats, error) {
	return Overview(ctx, store, PeriodQuery{Period: period})
}

func Overview(ctx context.Context, store *store.Store, pq PeriodQuery) (OverviewStats, error) {
	var result OverviewStats

	pw, err := ComputePeriodWindowFromQuery(ctx, store, pq)
	if err != nil {
		return result, err
	}

	startMs := pw.StartMs
	endMs := pw.EndMs

	db := store.DB()

	// Count active sessions, messages, active days, and assistant usage in one
	// range scan. Sessions/days are activity-based so live OpenCode results use
	// the same semantics as the cache and the other source adapters: a session
	// created before the range still counts when it has a message in the range.
	// The old path also scanned message once for COUNT(*) and again for usage,
	// doubling I/O on the largest table during a cold overview load.
	err = db.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(CASE WHEN json_extract(data, '$.role') = 'assistant' THEN COALESCE(json_extract(data, '$.cost'), 0) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN json_extract(data, '$.role') = 'assistant' THEN COALESCE(json_extract(data, '$.tokens.input'), 0) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN json_extract(data, '$.role') = 'assistant' THEN COALESCE(json_extract(data, '$.tokens.output'), 0) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN json_extract(data, '$.role') = 'assistant' THEN COALESCE(json_extract(data, '$.tokens.reasoning'), 0) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN json_extract(data, '$.role') = 'assistant' THEN COALESCE(json_extract(data, '$.tokens.cache.read'), 0) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN json_extract(data, '$.role') = 'assistant' THEN COALESCE(json_extract(data, '$.tokens.cache.write'), 0) ELSE 0 END), 0),
			COUNT(DISTINCT DATE(time_created / 1000, 'unixepoch'))
		FROM message
		WHERE time_created >= ? AND time_created < ?
	`, startMs, endMs).Scan(
		&result.Sessions,
		&result.Messages,
		&result.Cost,
		&result.Tokens.Input,
		&result.Tokens.Output,
		&result.Tokens.Reasoning,
		&result.Tokens.Cache.Read,
		&result.Tokens.Cache.Write,
		&result.Days,
	)
	if err != nil {
		return result, err
	}

	if result.Days > 0 {
		result.CostPerDay = result.Cost / float64(result.Days)
	} else {
		result.CostPerDay = 0
	}

	return result, nil
}
