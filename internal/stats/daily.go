package stats

import (
	"context"
	"fmt"
	"time"

	"opencode-dashboard/internal/store"
)

func Daily(ctx context.Context, db *store.Store, period string) (DailyStats, error) {
	days, err := parsePeriod(period)
	if err != nil {
		return DailyStats{}, err
	}

	now := time.Now()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDate := endDate.AddDate(0, 0, -days+1)

	dayMap := make(map[string]DayStats)
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		dayMap[key] = DayStats{
			Date:     key,
			Sessions: 0,
			Messages: 0,
			Cost:     0,
			Tokens:   TokenStats{},
		}
	}

	sessionCounts, err := querySessionCountsByDay(ctx, db, startDate, endDate)
	if err != nil {
		return DailyStats{}, fmt.Errorf("query session counts: %w", err)
	}

	for date, count := range sessionCounts {
		if entry, ok := dayMap[date]; ok {
			entry.Sessions = count
			dayMap[date] = entry
		}
	}

	messageStats, err := queryMessageStatsByDay(ctx, db, startDate, endDate)
	if err != nil {
		return DailyStats{}, fmt.Errorf("query message stats: %w", err)
	}

	for date, stats := range messageStats {
		if entry, ok := dayMap[date]; ok {
			entry.Messages = stats.Messages
			entry.Cost = stats.Cost
			entry.Tokens = stats.Tokens
			dayMap[date] = entry
		}
	}

	result := make([]DayStats, 0, len(dayMap))
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		result = append(result, dayMap[key])
	}

	return DailyStats{Days: result}, nil
}

func parsePeriod(period string) (int, error) {
	switch period {
	case "7d":
		return 7, nil
	case "30d":
		return 30, nil
	default:
		return 0, fmt.Errorf("invalid period: %q (supported: 7d, 30d)", period)
	}
}

type dayMessageStats struct {
	Messages int64
	Cost     float64
	Tokens   TokenStats
}

func querySessionCountsByDay(ctx context.Context, db *store.Store, startDate, endDate time.Time) (map[string]int64, error) {
	query := `
		SELECT DATE(time_created / 1000, 'unixepoch') as day, COUNT(*) as count
		FROM session
		WHERE time_created >= ? AND time_created < ?
		GROUP BY day
	`

	startMs := startDate.UnixMilli()
	endMs := endDate.AddDate(0, 0, 1).UnixMilli()

	rows, err := db.DB().QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var day string
		var count int64
		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		result[day] = count
	}

	return result, rows.Err()
}

func queryMessageStatsByDay(ctx context.Context, db *store.Store, startDate, endDate time.Time) (map[string]dayMessageStats, error) {
	query := `
		SELECT 
			DATE(m.time_created / 1000, 'unixepoch') as day,
			COUNT(*) as message_count,
			COALESCE(SUM(
				CASE 
					WHEN JSON_EXTRACT(m.data, '$.role') = 'assistant' 
					THEN CAST(JSON_EXTRACT(m.data, '$.cost') AS REAL)
					ELSE 0 
				END
			), 0) as total_cost,
			COALESCE(SUM(
				CASE 
					WHEN JSON_EXTRACT(m.data, '$.role') = 'assistant' 
					THEN CAST(JSON_EXTRACT(m.data, '$.tokens.input') AS INTEGER)
					ELSE 0 
				END
			), 0) as input_tokens,
			COALESCE(SUM(
				CASE 
					WHEN JSON_EXTRACT(m.data, '$.role') = 'assistant' 
					THEN CAST(JSON_EXTRACT(m.data, '$.tokens.output') AS INTEGER)
					ELSE 0 
				END
			), 0) as output_tokens,
			COALESCE(SUM(
				CASE 
					WHEN JSON_EXTRACT(m.data, '$.role') = 'assistant' 
					THEN CAST(JSON_EXTRACT(m.data, '$.tokens.reasoning') AS INTEGER)
					ELSE 0 
				END
			), 0) as reasoning_tokens,
			COALESCE(SUM(
				CASE 
					WHEN JSON_EXTRACT(m.data, '$.role') = 'assistant' 
					THEN CAST(JSON_EXTRACT(m.data, '$.tokens.cache.read') AS INTEGER)
					ELSE 0 
				END
			), 0) as cache_read_tokens,
			COALESCE(SUM(
				CASE 
					WHEN JSON_EXTRACT(m.data, '$.role') = 'assistant' 
					THEN CAST(JSON_EXTRACT(m.data, '$.tokens.cache.write') AS INTEGER)
					ELSE 0 
				END
			), 0) as cache_write_tokens
		FROM message m
		WHERE m.time_created >= ? AND m.time_created < ?
		GROUP BY day
	`

	startMs := startDate.UnixMilli()
	endMs := endDate.AddDate(0, 0, 1).UnixMilli()

	rows, err := db.DB().QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]dayMessageStats)
	for rows.Next() {
		var day string
		var stats dayMessageStats
		var cacheRead, cacheWrite int64

		if err := rows.Scan(
			&day,
			&stats.Messages,
			&stats.Cost,
			&stats.Tokens.Input,
			&stats.Tokens.Output,
			&stats.Tokens.Reasoning,
			&cacheRead,
			&cacheWrite,
		); err != nil {
			return nil, err
		}

		stats.Tokens.Cache = CacheStats{
			Read:  cacheRead,
			Write: cacheWrite,
		}
		result[day] = stats
	}

	return result, rows.Err()
}
