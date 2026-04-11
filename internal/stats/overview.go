package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"opencode-dashboard/internal/store"
)

func Overview(ctx context.Context, store *store.Store, period string) (OverviewStats, error) {
	var result OverviewStats

	days, err := parsePeriod(period)
	if err != nil {
		return result, err
	}

	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDate := endDate

	if days == allHistoricPeriodDays {
		startDate, err = queryEarliestActivityDate(ctx, store)
		if err != nil {
			return result, fmt.Errorf("query earliest activity date: %w", err)
		}
		if startDate.IsZero() {
			startDate = endDate
		}
	} else if days > 0 {
		startDate = endDate.AddDate(0, 0, -days+1)
	}

	startMs := startDate.UnixMilli()
	endMs := endDate.AddDate(0, 0, 1).UnixMilli()

	db := store.DB()

	// Session count filtered by period
	var sessions int64
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM session WHERE time_created >= ? AND time_created < ?", startMs, endMs).Scan(&sessions)
	if err != nil {
		if err == sql.ErrNoRows {
			sessions = 0
		} else {
			return result, err
		}
	}
	result.Sessions = sessions

	// Message count filtered by period
	var messages int64
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM message WHERE time_created >= ? AND time_created < ?", startMs, endMs).Scan(&messages)
	if err != nil {
		if err == sql.ErrNoRows {
			messages = 0
		} else {
			return result, err
		}
	}
	result.Messages = messages

	// Token/cost sums filtered by period
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
			AND time_created >= ? AND time_created < ?
	`

	var cost float64
	var input, output, reasoning, cacheRead, cacheWrite int64
	err = db.QueryRowContext(ctx, query, startMs, endMs).Scan(&cost, &input, &output, &reasoning, &cacheRead, &cacheWrite)
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

	// Active days filtered by period
	var daysCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT DATE(time_created / 1000, 'unixepoch'))
		FROM session
		WHERE time_created >= ? AND time_created < ?
	`, startMs, endMs).Scan(&daysCount)
	if err != nil {
		if err == sql.ErrNoRows {
			daysCount = 0
		} else {
			return result, err
		}
	}
	result.Days = daysCount

	if daysCount > 0 {
		result.CostPerDay = cost / float64(daysCount)
	} else {
		result.CostPerDay = 0
	}

	return result, nil
}
