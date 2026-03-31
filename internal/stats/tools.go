package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

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
			continue
		}

		var data toolPartData
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			continue
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

type toolAggregate struct {
	name        string
	invocations int64
	successes   int64
	failures    int64
	sessions    map[string]bool
}
