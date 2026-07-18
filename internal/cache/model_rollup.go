package cache

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"opencode-dashboard/internal/stats"
)

type cachedModelKey struct {
	modelID    string
	providerID string
}

func (s *Store) modelsFromRollups(ctx context.Context, sourceID string, startMs, endMs int64) (stats.ModelStats, error) {
	result := stats.ModelStats{SourceID: sourceID, Models: []stats.ModelEntry{}}
	if endMs <= startMs {
		return result, nil
	}
	parts := splitOverviewMillis(startMs, endMs)
	models, byKey, err := s.modelRollupTotals(ctx, sourceID, parts)
	if err != nil {
		return result, err
	}
	if err := s.attachModelRollupSessions(ctx, sourceID, parts, byKey); err != nil {
		return result, err
	}
	if err := s.attachModelRollupCosts(ctx, sourceID, parts, byKey); err != nil {
		return result, err
	}
	for i := range models {
		setModelAverages(models[i])
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost != models[j].Cost {
			return models[i].Cost > models[j].Cost
		}
		if models[i].Messages != models[j].Messages {
			return models[i].Messages > models[j].Messages
		}
		if models[i].ModelID != models[j].ModelID {
			return models[i].ModelID < models[j].ModelID
		}
		return models[i].ProviderID < models[j].ProviderID
	})
	status, provenance, err := s.overviewCostSummary(ctx, sourceID, parts)
	if err != nil {
		return result, err
	}
	result.Models = make([]stats.ModelEntry, len(models))
	for i := range models {
		result.Models[i] = *models[i]
	}
	result.CostStatus, result.CostProvenance = status, provenance
	return result, nil
}

func (s *Store) modelRollupTotals(ctx context.Context, sourceID string, parts overviewWindowParts) ([]*stats.ModelEntry, map[cachedModelKey]*stats.ModelEntry, error) {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, `
			SELECT
				model_id, provider_id, SUM(messages) AS messages, SUM(cost) AS cost,
				SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens,
				SUM(reasoning_tokens) AS reasoning_tokens,
				SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(cache_write_tokens) AS cache_write_tokens
			FROM hourly_usage
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
			  AND role = 'assistant' AND model_id != ''
			GROUP BY model_id, provider_id
		`)
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT
				COALESCE(model_id, '') AS model_id, COALESCE(provider_id, '') AS provider_id,
				COUNT(*) AS messages, COALESCE(SUM(cost), 0) AS cost,
				COALESCE(SUM(model_input_tokens), 0) AS input_tokens,
				COALESCE(SUM(model_output_tokens), 0) AS output_tokens,
				COALESCE(SUM(model_reasoning_tokens), 0) AS reasoning_tokens,
				COALESCE(SUM(model_cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(model_cache_write_tokens), 0) AS cache_write_tokens
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			  AND role = 'assistant' AND COALESCE(model_id, '') != ''
			GROUP BY COALESCE(model_id, ''), COALESCE(provider_id, '')
		`)
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return []*stats.ModelEntry{}, map[cachedModelKey]*stats.ModelEntry{}, nil
	}
	query := `
		SELECT
			model_id, provider_id, SUM(messages), COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY model_id, provider_id
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("read hourly model totals: %w", err)
	}
	defer rows.Close()
	models := make([]*stats.ModelEntry, 0)
	byKey := make(map[cachedModelKey]*stats.ModelEntry)
	for rows.Next() {
		entry := &stats.ModelEntry{SourceID: sourceID}
		if err := rows.Scan(
			&entry.ModelID, &entry.ProviderID, &entry.Messages, &entry.Cost,
			&entry.Tokens.Input, &entry.Tokens.Output, &entry.Tokens.Reasoning,
			&entry.Tokens.Cache.Read, &entry.Tokens.Cache.Write,
		); err != nil {
			return nil, nil, fmt.Errorf("scan hourly model totals: %w", err)
		}
		models = append(models, entry)
		byKey[cachedModelKey{modelID: entry.ModelID, providerID: entry.ProviderID}] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate hourly model totals: %w", err)
	}
	return models, byKey, nil
}

func (s *Store) attachModelRollupSessions(ctx context.Context, sourceID string, parts overviewWindowParts, byKey map[cachedModelKey]*stats.ModelEntry) error {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, `
			SELECT model_id, provider_id, session_id
			FROM hourly_model_sessions
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
		`)
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT COALESCE(model_id, ''), COALESCE(provider_id, ''), session_id
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			  AND role = 'assistant' AND COALESCE(model_id, '') != ''
		`)
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return nil
	}
	query := `
		SELECT model_id, provider_id, COUNT(DISTINCT session_id)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY model_id, provider_id
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read hourly model sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key cachedModelKey
		var sessions int64
		if err := rows.Scan(&key.modelID, &key.providerID, &sessions); err != nil {
			return fmt.Errorf("scan hourly model sessions: %w", err)
		}
		if entry := byKey[key]; entry != nil {
			entry.Sessions = sessions
		}
	}
	return rows.Err()
}

func (s *Store) attachModelRollupCosts(ctx context.Context, sourceID string, parts overviewWindowParts, byKey map[cachedModelKey]*stats.ModelEntry) error {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, `
			SELECT model_id, provider_id, cost_status, messages
			FROM hourly_model_cost
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
		`)
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, `
			SELECT
				COALESCE(model_id, ''), COALESCE(provider_id, ''),
				COALESCE(cost_status, ''), COUNT(*)
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			  AND role = 'assistant' AND COALESCE(model_id, '') != ''
			GROUP BY COALESCE(model_id, ''), COALESCE(provider_id, ''), COALESCE(cost_status, '')
		`)
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return nil
	}
	query := `
		SELECT model_id, provider_id, cost_status, SUM(messages)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY model_id, provider_id, cost_status
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read hourly model cost status: %w", err)
	}
	defer rows.Close()
	countsByModel := make(map[cachedModelKey]*costCounts)
	for rows.Next() {
		var key cachedModelKey
		var statusText string
		var count int64
		if err := rows.Scan(&key.modelID, &key.providerID, &statusText, &count); err != nil {
			return fmt.Errorf("scan hourly model cost status: %w", err)
		}
		counts := countsByModel[key]
		if counts == nil {
			counts = &costCounts{}
			countsByModel[key] = counts
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hourly model cost status: %w", err)
	}
	for key, counts := range countsByModel {
		if entry := byKey[key]; entry != nil {
			entry.CostStatus, entry.CostProvenance = counts.result()
		}
	}
	return nil
}

type cachedModelDayKey struct {
	date    string
	modelID string
}

func (s *Store) dailyModelDimensionFromRollups(ctx context.Context, sourceID, period string, gran stats.Granularity, startMs, endMs int64) (stats.DailyDimensionStats, error) {
	result := stats.DailyDimensionStats{
		SourceID:    sourceID,
		Days:        []stats.DimensionDayStats{},
		Dimension:   "model",
		Period:      period,
		Granularity: gran,
	}
	if endMs <= startMs {
		return result, nil
	}
	parts := splitOverviewMillis(startMs, endMs)
	days, byKey, err := s.modelDayRollupTotals(ctx, sourceID, gran, parts)
	if err != nil {
		return result, err
	}
	if err := s.attachModelDayRollupSessions(ctx, sourceID, gran, parts, byKey); err != nil {
		return result, err
	}
	if err := s.attachModelDayRollupCosts(ctx, sourceID, gran, parts, byKey); err != nil {
		return result, err
	}
	sort.Slice(days, func(i, j int) bool {
		if days[i].Date != days[j].Date {
			return days[i].Date < days[j].Date
		}
		if days[i].Messages != days[j].Messages {
			return days[i].Messages > days[j].Messages
		}
		return days[i].Dimension < days[j].Dimension
	})
	status, provenance, err := s.overviewCostSummary(ctx, sourceID, parts)
	if err != nil {
		return result, err
	}
	result.Days, result.CostStatus, result.CostProvenance = days, status, provenance
	return result, nil
}

func (s *Store) modelDayRollupTotals(ctx context.Context, sourceID string, gran stats.Granularity, parts overviewWindowParts) ([]stats.DimensionDayStats, map[cachedModelDayKey]*stats.DimensionDayStats, error) {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, fmt.Sprintf(`
			SELECT
				%s AS day,
				model_id, SUM(messages) AS messages, SUM(cost) AS cost,
				SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens,
				SUM(reasoning_tokens) AS reasoning_tokens,
				SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(cache_write_tokens) AS cache_write_tokens
			FROM hourly_usage
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
			  AND role = 'assistant' AND model_id != ''
			GROUP BY day, model_id
		`, stats.TrendBucketSQL("bucket_start_ms", gran)))
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, fmt.Sprintf(`
			SELECT
				%s AS day,
				COALESCE(model_id, '') AS model_id,
				COUNT(*) AS messages, COALESCE(SUM(cost), 0) AS cost,
				COALESCE(SUM(model_input_tokens), 0) AS input_tokens,
				COALESCE(SUM(model_output_tokens), 0) AS output_tokens,
				COALESCE(SUM(model_reasoning_tokens), 0) AS reasoning_tokens,
				COALESCE(SUM(model_cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(model_cache_write_tokens), 0) AS cache_write_tokens
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			  AND role = 'assistant' AND COALESCE(model_id, '') != ''
			GROUP BY day, COALESCE(model_id, '')
		`, stats.TrendBucketSQL("time_created_ms", gran)))
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return []stats.DimensionDayStats{}, map[cachedModelDayKey]*stats.DimensionDayStats{}, nil
	}
	query := `
		SELECT
			day, model_id, SUM(messages), COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY day, model_id
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("read hourly model trend totals: %w", err)
	}
	defer rows.Close()
	days := make([]stats.DimensionDayStats, 0)
	byKey := make(map[cachedModelDayKey]*stats.DimensionDayStats)
	for rows.Next() {
		var day stats.DimensionDayStats
		day.SourceID = sourceID
		if err := rows.Scan(
			&day.Date, &day.Dimension, &day.Messages, &day.Cost,
			&day.Tokens.Input, &day.Tokens.Output, &day.Tokens.Reasoning,
			&day.Tokens.Cache.Read, &day.Tokens.Cache.Write,
		); err != nil {
			return nil, nil, fmt.Errorf("scan hourly model trend totals: %w", err)
		}
		days = append(days, day)
		byKey[cachedModelDayKey{date: day.Date, modelID: day.Dimension}] = &days[len(days)-1]
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate hourly model trend totals: %w", err)
	}
	// Appending can reallocate the slice and invalidate pointers captured above.
	// Rebuild the map once the final backing array is stable.
	clear(byKey)
	for i := range days {
		byKey[cachedModelDayKey{date: days[i].Date, modelID: days[i].Dimension}] = &days[i]
	}
	return days, byKey, nil
}

func (s *Store) attachModelDayRollupSessions(ctx context.Context, sourceID string, gran stats.Granularity, parts overviewWindowParts, byKey map[cachedModelDayKey]*stats.DimensionDayStats) error {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, fmt.Sprintf(`
			SELECT %s AS day, model_id, session_id
			FROM hourly_model_sessions
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
		`, stats.TrendBucketSQL("bucket_start_ms", gran)))
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		selects = append(selects, fmt.Sprintf(`
			SELECT %s, COALESCE(model_id, ''), session_id
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			  AND role = 'assistant' AND COALESCE(model_id, '') != ''
		`, stats.TrendBucketSQL("time_created_ms", gran)))
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return nil
	}
	query := `
		SELECT day, model_id, COUNT(DISTINCT session_id)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY day, model_id
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read hourly model trend sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key cachedModelDayKey
		var sessions int64
		if err := rows.Scan(&key.date, &key.modelID, &sessions); err != nil {
			return fmt.Errorf("scan hourly model trend sessions: %w", err)
		}
		if day := byKey[key]; day != nil {
			day.Sessions = sessions
		}
	}
	return rows.Err()
}

func (s *Store) attachModelDayRollupCosts(ctx context.Context, sourceID string, gran stats.Granularity, parts overviewWindowParts, byKey map[cachedModelDayKey]*stats.DimensionDayStats) error {
	selects := make([]string, 0, 1+len(parts.edges))
	args := make([]any, 0, 3*(1+len(parts.edges)))
	if parts.hasFullHours() {
		selects = append(selects, fmt.Sprintf(`
			SELECT %s AS day, model_id, cost_status, SUM(messages) AS messages
			FROM hourly_model_cost
			WHERE source_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?
			GROUP BY day, model_id, cost_status
		`, stats.TrendBucketSQL("bucket_start_ms", gran)))
		args = append(args, sourceID, parts.fullStart, parts.fullEnd)
	}
	for _, edge := range parts.edges {
		bucket := stats.TrendBucketSQL("time_created_ms", gran)
		selects = append(selects, fmt.Sprintf(`
			SELECT
				%[1]s, COALESCE(model_id, ''),
				COALESCE(cost_status, ''), COUNT(*)
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			  AND role = 'assistant' AND COALESCE(model_id, '') != ''
			GROUP BY %[1]s, COALESCE(model_id, ''), COALESCE(cost_status, '')
		`, bucket))
		args = append(args, sourceID, edge.start, edge.end)
	}
	if len(selects) == 0 {
		return nil
	}
	query := `
		SELECT day, model_id, cost_status, SUM(messages)
		FROM (` + strings.Join(selects, ` UNION ALL `) + `)
		GROUP BY day, model_id, cost_status
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read hourly model trend cost status: %w", err)
	}
	defer rows.Close()
	countsByDay := make(map[cachedModelDayKey]*costCounts)
	for rows.Next() {
		var key cachedModelDayKey
		var statusText string
		var count int64
		if err := rows.Scan(&key.date, &key.modelID, &statusText, &count); err != nil {
			return fmt.Errorf("scan hourly model trend cost status: %w", err)
		}
		counts := countsByDay[key]
		if counts == nil {
			counts = &costCounts{}
			countsByDay[key] = counts
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hourly model trend cost status: %w", err)
	}
	for key, counts := range countsByDay {
		if day := byKey[key]; day != nil {
			day.CostStatus, day.CostProvenance = counts.result()
		}
	}
	return nil
}
