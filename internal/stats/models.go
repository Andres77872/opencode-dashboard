package stats

import (
	"context"
	"database/sql"
	"sort"

	"opencode-dashboard/internal/store"
)

const modelsQuery = `
	WITH filtered_messages AS MATERIALIZED (
		SELECT
			id,
			session_id,
			json_extract(data, '$.modelID') AS model_id,
			json_extract(data, '$.providerID') AS provider_id,
			COALESCE(json_extract(data, '$.cost'), 0) AS cost,
			COALESCE(json_extract(data, '$.tokens.input'), 0) AS input,
			COALESCE(json_extract(data, '$.tokens.output'), 0) AS output,
			COALESCE(json_extract(data, '$.tokens.reasoning'), 0) AS reasoning,
			COALESCE(json_extract(data, '$.tokens.cache.read'), 0) AS cache_read,
			COALESCE(json_extract(data, '$.tokens.cache.write'), 0) AS cache_write
		FROM message
		WHERE json_extract(data, '$.role') = 'assistant'
			AND json_extract(data, '$.modelID') IS NOT NULL
			AND json_extract(data, '$.modelID') != ''
			AND time_created >= ? AND time_created < ?
	),
	step_usage AS MATERIALIZED (
		SELECT
			p.message_id,
			SUM(COALESCE(json_extract(p.data, '$.tokens.input'), 0)) AS input,
			SUM(COALESCE(json_extract(p.data, '$.tokens.output'), 0)) AS output,
			SUM(COALESCE(json_extract(p.data, '$.tokens.reasoning'), 0)) AS reasoning,
			SUM(COALESCE(json_extract(p.data, '$.tokens.cache.read'), 0)) AS cache_read,
			SUM(COALESCE(json_extract(p.data, '$.tokens.cache.write'), 0)) AS cache_write
		FROM filtered_messages msg
		CROSS JOIN part p
		WHERE p.message_id = msg.id
			AND json_extract(p.data, '$.type') = 'step-finish'
		GROUP BY p.message_id
	)
	SELECT
		msg.model_id,
		msg.provider_id,
		COUNT(DISTINCT msg.session_id) AS sessions,
		COUNT(*) AS messages,
		SUM(msg.cost) AS total_cost,
		SUM(COALESCE(step.input, msg.input)) AS input_tokens,
		SUM(COALESCE(step.output, msg.output)) AS output_tokens,
		SUM(COALESCE(step.reasoning, msg.reasoning)) AS reasoning_tokens,
		SUM(COALESCE(step.cache_read, msg.cache_read)) AS cache_read,
		SUM(COALESCE(step.cache_write, msg.cache_write)) AS cache_write
	FROM filtered_messages msg
	LEFT JOIN step_usage step ON step.message_id = msg.id
	GROUP BY msg.model_id, msg.provider_id
`

// ModelsString is a backward-compatible wrapper that accepts a string period.
func ModelsString(ctx context.Context, s *store.Store, period string) (ModelStats, error) {
	return Models(ctx, s, PeriodQuery{Period: period})
}

func Models(ctx context.Context, s *store.Store, pq PeriodQuery) (ModelStats, error) {
	pw, err := ComputePeriodWindowFromQuery(ctx, s, pq)
	if err != nil {
		return ModelStats{}, err
	}

	startMs := pw.StartMs
	endMs := pw.EndMs

	// Materialize the small, requested message window first. The previous
	// shape aggregated every step-finish part in the database before joining
	// it to this window, so a 30-day model view still parsed years of (often
	// very large) part JSON. Joining parts through filtered_messages lets
	// SQLite use its message-id index and extracts each message field once.
	rows, err := s.DB().QueryContext(ctx, modelsQuery, startMs, endMs)
	if err != nil {
		return ModelStats{}, err
	}
	defer rows.Close()

	models := make([]ModelEntry, 0)
	for rows.Next() {
		var entry ModelEntry
		var modelID, providerID sql.NullString
		var cacheRead, cacheWrite sql.NullInt64

		err := rows.Scan(
			&modelID,
			&providerID,
			&entry.Sessions,
			&entry.Messages,
			&entry.Cost,
			&entry.Tokens.Input,
			&entry.Tokens.Output,
			&entry.Tokens.Reasoning,
			&cacheRead,
			&cacheWrite,
		)
		if err != nil {
			return ModelStats{}, err
		}

		entry.ModelID = modelID.String
		entry.ProviderID = providerID.String
		entry.Tokens.Cache.Read = cacheRead.Int64
		entry.Tokens.Cache.Write = cacheWrite.Int64

		models = append(models, entry)
	}

	if err := rows.Err(); err != nil {
		return ModelStats{}, err
	}

	// Compute per-type token averages per message and per session.
	for i := range models {
		entry := &models[i]
		if entry.Messages > 0 {
			entry.AvgTokensPerMessage = &AvgTokenStats{
				Input:      float64(entry.Tokens.Input) / float64(entry.Messages),
				Output:     float64(entry.Tokens.Output) / float64(entry.Messages),
				Reasoning:  float64(entry.Tokens.Reasoning) / float64(entry.Messages),
				CacheRead:  float64(entry.Tokens.Cache.Read) / float64(entry.Messages),
				CacheWrite: float64(entry.Tokens.Cache.Write) / float64(entry.Messages),
			}
		}
		if entry.Sessions > 0 {
			entry.AvgTokensPerSession = &AvgTokenStats{
				Input:      float64(entry.Tokens.Input) / float64(entry.Sessions),
				Output:     float64(entry.Tokens.Output) / float64(entry.Sessions),
				Reasoning:  float64(entry.Tokens.Reasoning) / float64(entry.Sessions),
				CacheRead:  float64(entry.Tokens.Cache.Read) / float64(entry.Sessions),
				CacheWrite: float64(entry.Tokens.Cache.Write) / float64(entry.Sessions),
			}
		}
	}

	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost != models[j].Cost {
			return models[i].Cost > models[j].Cost
		}
		if models[i].Messages != models[j].Messages {
			return models[i].Messages > models[j].Messages
		}
		return models[i].ModelID < models[j].ModelID
	})

	return ModelStats{Models: models}, nil
}
