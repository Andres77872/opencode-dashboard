package stats

import (
	"context"
	"database/sql"
	"sort"

	"opencode-dashboard/internal/store"
)

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

	query := `
		SELECT
			json_extract(msg.data, '$.modelID') as model_id,
			json_extract(msg.data, '$.providerID') as provider_id,
			COUNT(DISTINCT msg.session_id) as sessions,
			COUNT(*) as messages,
			SUM(COALESCE(json_extract(msg.data, '$.cost'), 0)) as total_cost,
			SUM(COALESCE(step.input, json_extract(msg.data, '$.tokens.input'), 0)) as input_tokens,
			SUM(COALESCE(step.output, json_extract(msg.data, '$.tokens.output'), 0)) as output_tokens,
			SUM(COALESCE(step.reasoning, json_extract(msg.data, '$.tokens.reasoning'), 0)) as reasoning_tokens,
			SUM(COALESCE(step.cache_read, json_extract(msg.data, '$.tokens.cache.read'), 0)) as cache_read,
			SUM(COALESCE(step.cache_write, json_extract(msg.data, '$.tokens.cache.write'), 0)) as cache_write
		FROM (
			SELECT id, session_id, data, time_created
			FROM message
			WHERE json_extract(data, '$.role') = 'assistant'
				AND json_extract(data, '$.modelID') IS NOT NULL
				AND time_created >= ? AND time_created < ?
		) msg
		LEFT JOIN (
			SELECT
				p.message_id,
				SUM(COALESCE(json_extract(p.data, '$.tokens.input'), 0)) as input,
				SUM(COALESCE(json_extract(p.data, '$.tokens.output'), 0)) as output,
				SUM(COALESCE(json_extract(p.data, '$.tokens.reasoning'), 0)) as reasoning,
				SUM(COALESCE(json_extract(p.data, '$.tokens.cache.read'), 0)) as cache_read,
				SUM(COALESCE(json_extract(p.data, '$.tokens.cache.write'), 0)) as cache_write
			FROM part p
			WHERE json_extract(p.data, '$.type') = 'step-finish'
			GROUP BY p.message_id
		) step ON step.message_id = msg.id
		GROUP BY model_id, provider_id
	`

	rows, err := s.DB().QueryContext(ctx, query, startMs, endMs)
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
