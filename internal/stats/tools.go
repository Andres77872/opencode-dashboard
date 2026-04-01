package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"opencode-dashboard/internal/store"
)

type toolPartData struct {
	Type  string `json:"type"`
	Tool  string `json:"tool"`
	State struct {
		Status string `json:"status"`
	} `json:"state"`
}

func Tools(ctx context.Context, st *store.Store) (ToolStats, error) {
	if !st.IsValidSchema() {
		return ToolStats{}, store.ErrInvalidSchema
	}

	db := st.DB()

	query := `SELECT session_id, data FROM part`
	rows, err := db.QueryContext(ctx, query)
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
