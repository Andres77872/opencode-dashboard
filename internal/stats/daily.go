package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"opencode-dashboard/internal/store"
)

const allHistoricPeriodDays = -1

// Daily returns per-day stats for the given period query.
// When no granularity is passed, defaults: 1d + hour presets → hourly, 7d+ → daily.
// Explicit granularity=day disables auto-hour for presets.
// Explicit granularity=hour forces hourly regardless of period (multi-day supported).
// DailyString is a backward-compatible wrapper that accepts a string period.
// It constructs a PeriodQuery and delegates to Daily.
func DailyString(ctx context.Context, db *store.Store, period string, granularity ...Granularity) (DailyStats, error) {
	return Daily(ctx, db, PeriodQuery{Period: period}, granularity...)
}

func Daily(ctx context.Context, db *store.Store, pq PeriodQuery, granularity ...Granularity) (DailyStats, error) {
	// Determine if granularity was explicitly provided (vs. handler passing empty or not at all)
	var gran Granularity
	explicit := false
	if len(granularity) > 0 && granularity[0] != "" {
		gran = granularity[0]
		explicit = true
	}

	// Get the period string for the auto-hour heuristic. Only applies for preset mode.
	period := pq.Period
	if period == "" && pq.From != "" {
		period = "custom"
	}

	// Auto-hour for 1d by default (no explicit granularity override)
	if period == "1d" && !explicit {
		return dailyHourly(ctx, db, pq)
	}

	// Explicit hour override (for any period including 1d — multi-day hourly supported)
	if gran == GranularityHour {
		return dailyHourly(ctx, db, pq)
	}

	// Auto-hour for hour presets (1h, 6h, 12h, 24h, 72h) when no explicit granularity
	if _, ok := parseHourPreset(period); ok && !explicit {
		return dailyHourly(ctx, db, pq)
	}

	// Use the new dispatcher
	pw, err := ComputePeriodWindowFromQuery(ctx, db, pq)
	if err != nil {
		return DailyStats{}, err
	}

	startDate := pw.StartDate
	endDate := pw.EndDate

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

	return DailyStats{Days: result, Granularity: GranularityDay}, nil
}

func parsePeriod(period string) (int, error) {
	switch period {
	case "1d":
		return 1, nil
	case "7d":
		return 7, nil
	case "14d":
		return 14, nil
	case "30d":
		return 30, nil
	case "1y":
		return 365, nil
	case "all":
		return allHistoricPeriodDays, nil
	case "1h", "6h", "12h", "24h", "72h":
		return 0, fmt.Errorf("invalid period: %q is an hour preset and should be handled by presetPeriodWindow directly", period)
	default:
		return 0, fmt.Errorf("invalid period: %q (supported: 1d, 7d, 14d, 30d, 1y, all, plus hour presets 1h, 6h, 12h, 24h, 72h)", period)
	}
}

func queryEarliestActivityDate(ctx context.Context, db *store.Store) (time.Time, error) {
	query := `
		SELECT MIN(created_at)
		FROM (
			SELECT MIN(CAST(time_created AS INTEGER)) AS created_at FROM session
			UNION ALL
			SELECT MIN(CAST(time_created AS INTEGER)) AS created_at FROM message
		)
		WHERE created_at IS NOT NULL
	`

	var earliest sql.NullInt64
	if err := db.DB().QueryRowContext(ctx, query).Scan(&earliest); err != nil {
		return time.Time{}, err
	}

	if !earliest.Valid {
		return time.Time{}, nil
	}

	date := time.UnixMilli(earliest.Int64).In(time.UTC)
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC), nil
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

// dailyHourly returns per-hour stats across the given period query.
// When period is "1d" (or empty), returns 24 hourly buckets for today (UTC midnight to midnight).
// When period is broader (e.g. "7d", "30d"), generates hourly buckets across the full window
// using ComputePeriodWindowFromQuery. Example: "7d" → 168 hourly buckets (7 × 24).
func dailyHourly(ctx context.Context, db *store.Store, pq PeriodQuery) (DailyStats, error) {
	var startTime, endTime time.Time

	period := pq.Period
	if period == "" {
		period = "custom"
	}

	if period == "1d" {
		now := time.Now().UTC()
		startTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		endTime = startTime.Add(24 * time.Hour)
	} else if pq.From != "" {
		pw, err := ComputePeriodWindowFromQuery(ctx, db, pq)
		if err != nil {
			return DailyStats{}, err
		}
		startTime = pw.StartDate
		endTime = pw.EndDate
	} else {
		pw, err := ComputePeriodWindowFromQuery(ctx, db, pq)
		if err != nil {
			return DailyStats{}, err
		}
		startTime = pw.StartDate
		endTime = pw.EndDate
		// Extend by 24h for day-aligned windows (not rolling presets) so the last day's hours are covered
		if _, ok := parseHourPreset(period); !ok {
			endTime = endTime.Add(24 * time.Hour)
		}
	}

	totalHours := int(endTime.Sub(startTime).Hours())
	if totalHours <= 0 {
		totalHours = 24 // safety fallback
	}

	hourMap := make(map[string]DayStats)
	for h := 0; h < totalHours; h++ {
		hourTime := startTime.Add(time.Duration(h) * time.Hour)
		key := hourTime.Format("2006-01-02T15:04:05Z")
		hourMap[key] = DayStats{
			Date:     key,
			Sessions: 0,
			Messages: 0,
			Cost:     0,
			Tokens:   TokenStats{},
		}
	}

	sessionCounts, err := querySessionCountsByHour(ctx, db, startTime, endTime)
	if err != nil {
		return DailyStats{}, fmt.Errorf("query session counts by hour: %w", err)
	}

	for hour, count := range sessionCounts {
		if entry, ok := hourMap[hour]; ok {
			entry.Sessions = count
			hourMap[hour] = entry
		}
	}

	messageStats, err := queryMessageStatsByHour(ctx, db, startTime, endTime)
	if err != nil {
		return DailyStats{}, fmt.Errorf("query message stats by hour: %w", err)
	}

	for hour, stats := range messageStats {
		if entry, ok := hourMap[hour]; ok {
			entry.Messages = stats.Messages
			entry.Cost = stats.Cost
			entry.Tokens = stats.Tokens
			hourMap[hour] = entry
		}
	}

	result := make([]DayStats, 0, totalHours)
	for h := 0; h < totalHours; h++ {
		hourTime := startTime.Add(time.Duration(h) * time.Hour)
		key := hourTime.Format("2006-01-02T15:04:05Z")
		result = append(result, hourMap[key])
	}

	return DailyStats{Days: result, Granularity: GranularityHour}, nil
}

func querySessionCountsByHour(ctx context.Context, db *store.Store, startTime, endTime time.Time) (map[string]int64, error) {
	query := `
		SELECT 
			STRFTIME('%Y-%m-%dT%H:00:00Z', DATETIME(time_created / 1000, 'unixepoch')) as hour,
			COUNT(*) as count
		FROM session
		WHERE time_created >= ? AND time_created < ?
		GROUP BY hour
	`

	startMs := startTime.UnixMilli()
	endMs := endTime.UnixMilli()

	rows, err := db.DB().QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var hour string
		var count int64
		if err := rows.Scan(&hour, &count); err != nil {
			return nil, err
		}
		result[hour] = count
	}

	return result, rows.Err()
}

func queryMessageStatsByHour(ctx context.Context, db *store.Store, startTime, endTime time.Time) (map[string]dayMessageStats, error) {
	query := `
		SELECT 
			STRFTIME('%Y-%m-%dT%H:00:00Z', DATETIME(m.time_created / 1000, 'unixepoch')) as hour,
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
		GROUP BY hour
	`

	startMs := startTime.UnixMilli()
	endMs := endTime.UnixMilli()

	rows, err := db.DB().QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]dayMessageStats)
	for rows.Next() {
		var hour string
		var stats dayMessageStats
		var cacheRead, cacheWrite int64

		if err := rows.Scan(
			&hour,
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
		result[hour] = stats
	}

	return result, rows.Err()
}

// validDimensions contains the supported dimension values for DailyDimension.
var validDimensions = map[string]string{
	"model":   "$.modelID",
	"tool":    "$.tool",
	"project": "$.projectID",
}

// DailyDimension returns per-day, per-dimension stats grouped by the given dimension field.
// Supported dimensions: "model" (JSON_EXTRACT $.modelID), "tool" ($.tool), "project" ($.projectID).
func DailyDimension(ctx context.Context, db *store.Store, dimension string, pq PeriodQuery) (DailyDimensionStats, error) {
	path, ok := validDimensions[dimension]
	if !ok {
		return DailyDimensionStats{}, fmt.Errorf("invalid dimension %q: supported values are model, tool, project", dimension)
	}

	pw, err := ComputePeriodWindowFromQuery(ctx, db, pq)
	if err != nil {
		return DailyDimensionStats{}, err
	}

	query := fmt.Sprintf(`
		SELECT
			DATE(m.time_created / 1000, 'unixepoch') AS day,
			JSON_EXTRACT(m.data, '%s') AS dim,
			COUNT(DISTINCT m.session_id) AS sessions,
			COUNT(*) AS messages,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.cost') AS REAL)), 0) AS total_cost,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.input') AS INTEGER)), 0) AS input_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.output') AS INTEGER)), 0) AS output_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.reasoning') AS INTEGER)), 0) AS reasoning_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.read') AS INTEGER)), 0) AS cache_read_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.write') AS INTEGER)), 0) AS cache_write_tokens
		FROM message m
		WHERE JSON_EXTRACT(m.data, '$.role') = 'assistant'
			AND JSON_EXTRACT(m.data, '%[1]s') IS NOT NULL
			AND JSON_EXTRACT(m.data, '%[1]s') != ''
			AND m.time_created >= ? AND m.time_created < ?
		GROUP BY day, dim
		ORDER BY day ASC, total_cost DESC
	`, path)

	rows, err := db.DB().QueryContext(ctx, query, pw.StartMs, pw.EndMs)
	if err != nil {
		return DailyDimensionStats{}, fmt.Errorf("query dimension stats: %w", err)
	}
	defer rows.Close()

	days := make([]DimensionDayStats, 0)
	for rows.Next() {
		var (
			day        string
			dim        sql.NullString
			sessions   int64
			messages   int64
			cost       float64
			input      int64
			output     int64
			reasoning  int64
			cacheRead  int64
			cacheWrite int64
		)

		if err := rows.Scan(
			&day, &dim,
			&sessions, &messages,
			&cost,
			&input, &output, &reasoning,
			&cacheRead, &cacheWrite,
		); err != nil {
			return DailyDimensionStats{}, fmt.Errorf("scan dimension row: %w", err)
		}

		dimKey := dim.String
		if !dim.Valid {
			dimKey = "unknown"
		}

		days = append(days, DimensionDayStats{
			Date:      day,
			Dimension: dimKey,
			Sessions:  sessions,
			Messages:  messages,
			Cost:      cost,
			Tokens: TokenStats{
				Input:     input,
				Output:    output,
				Reasoning: reasoning,
				Cache: CacheStats{
					Read:  cacheRead,
					Write: cacheWrite,
				},
			},
		})
	}

	if err := rows.Err(); err != nil {
		return DailyDimensionStats{}, fmt.Errorf("iterate dimension rows: %w", err)
	}

	periodLabel := pq.Period
	if periodLabel == "" && pq.From != "" {
		periodLabel = "from_" + pq.From
	}
	return DailyDimensionStats{
		Days:      days,
		Dimension: dimension,
		Period:    periodLabel,
	}, nil
}
