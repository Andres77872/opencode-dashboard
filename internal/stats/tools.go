package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/store"
)

type toolPartData struct {
	Type  string `json:"type"`
	Tool  string `json:"tool"`
	State struct {
		Status string `json:"status"`
	} `json:"state"`
}

func Tools(ctx context.Context, st *store.Store, period string) (ToolStats, error) {
	if !st.IsValidSchema() {
		return ToolStats{}, store.ErrInvalidSchema
	}

	days, err := parsePeriod(period)
	if err != nil {
		return ToolStats{}, err
	}

	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDate := endDate

	if days == allHistoricPeriodDays {
		startDate, err = queryEarliestActivityDate(ctx, st)
		if err != nil {
			return ToolStats{}, fmt.Errorf("query earliest activity date: %w", err)
		}
		if startDate.IsZero() {
			startDate = endDate
		}
	} else if days > 0 {
		startDate = endDate.AddDate(0, 0, -days+1)
	}

	startMs := startDate.UnixMilli()
	endMs := endDate.AddDate(0, 0, 1).UnixMilli()

	db := st.DB()

	query := `SELECT session_id, data FROM part WHERE time_created >= ? AND time_created < ?`
	rows, err := db.QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return ToolStats{}, fmt.Errorf("failed to query parts: %w", err)
	}
	defer rows.Close()

	aggregates := make(map[string]*toolAggregate)

	for rows.Next() {
		var sessionID string
		var dataJSON string

		if err := rows.Scan(&sessionID, &dataJSON); err != nil {
			return ToolStats{}, fmt.Errorf("failed to scan part row: %w", err)
		}

		data, err := parseToolPartData(dataJSON)
		if err != nil {
			return ToolStats{}, fmt.Errorf("failed to parse tool part for session %s: %w", sessionID, err)
		}

		if data.Type != "tool" || data.Tool == "" {
			continue
		}

		agg, exists := aggregates[data.Tool]
		if !exists {
			agg = &toolAggregate{
				name:     data.Tool,
				sessions: make(map[string]bool),
			}
			aggregates[data.Tool] = agg
		}

		agg.invocations++
		agg.sessions[sessionID] = true

		switch data.State.Status {
		case "completed":
			agg.successes++
		case "error":
			agg.failures++
		}
	}

	if err := rows.Err(); err != nil {
		return ToolStats{}, fmt.Errorf("error iterating parts: %w", err)
	}

	tools := make([]ToolEntry, 0, len(aggregates))
	for _, agg := range aggregates {
		tools = append(tools, ToolEntry{
			Name:        agg.name,
			Invocations: agg.invocations,
			Successes:   agg.successes,
			Failures:    agg.failures,
			Sessions:    int64(len(agg.sessions)),
		})
	}

	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Invocations != tools[j].Invocations {
			return tools[i].Invocations > tools[j].Invocations
		}
		return tools[i].Name < tools[j].Name
	})

	return ToolStats{Tools: tools}, nil
}

func parseToolPartData(dataJSON string) (toolPartData, error) {
	var data toolPartData

	trimmed := strings.TrimSpace(dataJSON)
	if trimmed == "" {
		return data, fmt.Errorf("empty part data")
	}

	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return data, fmt.Errorf("invalid JSON: %w", err)
	}

	if data.Type == "" {
		return data, fmt.Errorf("missing part type")
	}

	if data.Type == "tool" && data.Tool == "" {
		return data, fmt.Errorf("missing tool name")
	}

	return data, nil
}

type toolAggregate struct {
	name        string
	invocations int64
	successes   int64
	failures    int64
	sessions    map[string]bool
}
