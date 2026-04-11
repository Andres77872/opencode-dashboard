package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"opencode-dashboard/internal/store"
)

func Models(ctx context.Context, s *store.Store, period string) (ModelStats, error) {
	days, err := parsePeriod(period)
	if err != nil {
		return ModelStats{}, err
	}

	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDate := endDate

	if days == allHistoricPeriodDays {
		startDate, err = queryEarliestActivityDate(ctx, s)
		if err != nil {
			return ModelStats{}, fmt.Errorf("query earliest activity date: %w", err)
		}
		if startDate.IsZero() {
			startDate = endDate
		}
	} else if days > 0 {
		startDate = endDate.AddDate(0, 0, -days+1)
	}

	startMs := startDate.UnixMilli()
	endMs := endDate.AddDate(0, 0, 1).UnixMilli()

	query := `
		SELECT
			json_extract(data, '$.modelID') as model_id,
			json_extract(data, '$.providerID') as provider_id,
			COUNT(DISTINCT session_id) as sessions,
			COUNT(*) as messages,
			SUM(COALESCE(json_extract(data, '$.cost'), 0)) as total_cost,
			SUM(COALESCE(json_extract(data, '$.tokens.input'), 0)) as input_tokens,
			SUM(COALESCE(json_extract(data, '$.tokens.output'), 0)) as output_tokens,
			SUM(COALESCE(json_extract(data, '$.tokens.reasoning'), 0)) as reasoning_tokens,
			SUM(COALESCE(json_extract(data, '$.tokens.cache.read'), 0)) as cache_read,
			SUM(COALESCE(json_extract(data, '$.tokens.cache.write'), 0)) as cache_write
		FROM message
		WHERE json_extract(data, '$.role') = 'assistant'
			AND json_extract(data, '$.modelID') IS NOT NULL
			AND time_created >= ? AND time_created < ?
		GROUP BY model_id, provider_id
	`

	rows, err := s.DB().QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return ModelStats{}, err
	}
	defer rows.Close()

	var models []ModelEntry
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
