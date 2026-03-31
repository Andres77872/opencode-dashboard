package stats

import (
	"context"
	"database/sql"

	"opencode-dashboard/internal/store"
)

func Overview(ctx context.Context, store *store.Store) (OverviewStats, error) {
	var result OverviewStats

	db := store.DB()

	var sessions int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM session").Scan(&sessions)
	if err != nil {
		if err == sql.ErrNoRows {
			sessions = 0
		} else {
			return result, err
		}
	}
	result.Sessions = sessions

	var messages int64
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM message").Scan(&messages)
	if err != nil {
		if err == sql.ErrNoRows {
			messages = 0
		} else {
			return result, err
		}
	}
	result.Messages = messages

	query := `
		SELECT 
			COALESCE(SUM(json_extract(data, '$.cost')), 0) as total_cost,
			COALESCE(SUM(json_extract(data, '$.tokens.input')), 0) as total_input,
			COALESCE(SUM(json_extract(data, '$.tokens.output')), 0) as total_output,
			COALESCE(SUM(json_extract(data, '$.tokens.reasoning')), 0) as total_reasoning,
			COALESCE(SUM(json_extract(data, '$.tokens.cache.read')), 0) as total_cache_read,
			COALESCE(SUM(json_extract(data, '$.tokens.cache.write')), 0) as total_cache_write
		FROM message
		WHERE json_extract(data, '$.role') = 'assistant'
	`

	var cost float64
	var input, output, reasoning, cacheRead, cacheWrite int64
	err = db.QueryRowContext(ctx, query).Scan(&cost, &input, &output, &reasoning, &cacheRead, &cacheWrite)
	if err != nil {
		if err == sql.ErrNoRows {
			cost = 0
			input, output, reasoning, cacheRead, cacheWrite = 0, 0, 0, 0, 0
		} else {
			return result, err
		}
	}

	result.Cost = cost
	result.Tokens.Input = input
	result.Tokens.Output = output
	result.Tokens.Reasoning = reasoning
	result.Tokens.Cache.Read = cacheRead
	result.Tokens.Cache.Write = cacheWrite

	var days int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT DATE(time_created / 1000, 'unixepoch'))
		FROM session
	`).Scan(&days)
	if err != nil {
		if err == sql.ErrNoRows {
			days = 0
		} else {
			return result, err
		}
	}
	result.Days = days

	if days > 0 {
		result.CostPerDay = cost / float64(days)
	} else {
		result.CostPerDay = 0
	}

	return result, nil
}
