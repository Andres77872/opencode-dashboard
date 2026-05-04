package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// useLegacyToolsPath returns true when OPCODE_TOOLS_LEGACY=true, forcing the old Go streaming path.
func useLegacyToolsPath() bool {
	return os.Getenv("OPCODE_TOOLS_LEGACY") == "true"
}

// ToolsString is a backward-compatible wrapper that accepts a string period.
func ToolsString(ctx context.Context, st *store.Store, period string) (ToolStats, error) {
	return Tools(ctx, st, PeriodQuery{Period: period})
}

func Tools(ctx context.Context, st *store.Store, pq PeriodQuery) (ToolStats, error) {
	if !st.IsValidSchema() {
		return ToolStats{}, store.ErrInvalidSchema
	}

	pw, err := ComputePeriodWindowFromQuery(ctx, st, pq)
	if err != nil {
		return ToolStats{}, err
	}

	startMs := pw.StartMs
	endMs := pw.EndMs

	if useLegacyToolsPath() {
		return toolsLegacy(ctx, st, startMs, endMs)
	}

	return toolsSQL(ctx, st, startMs, endMs)
}

// toolsSQL uses JSON_EXTRACT + GROUP BY in SQL to aggregate tool stats.
func toolsSQL(ctx context.Context, st *store.Store, startMs, endMs int64) (ToolStats, error) {
	db := st.DB()

	query := `
		SELECT
			JSON_EXTRACT(p.data, '$.tool') AS tool_name,
			COUNT(*) AS invocations,
			SUM(CASE WHEN JSON_EXTRACT(p.data, '$.state.status') = 'completed' THEN 1 ELSE 0 END) AS successes,
			SUM(CASE WHEN JSON_EXTRACT(p.data, '$.state.status') = 'error' THEN 1 ELSE 0 END) AS failures,
			COUNT(DISTINCT p.session_id) AS sessions
		FROM part p
		WHERE p.time_created >= ? AND p.time_created < ?
			AND JSON_EXTRACT(p.data, '$.type') = 'tool'
			AND JSON_EXTRACT(p.data, '$.tool') IS NOT NULL
			AND JSON_EXTRACT(p.data, '$.tool') != ''
		GROUP BY tool_name
		ORDER BY invocations DESC, tool_name ASC
	`

	rows, err := db.QueryContext(ctx, query, startMs, endMs)
	if err != nil {
		return ToolStats{}, fmt.Errorf("failed to query tool aggregates: %w", err)
	}
	defer rows.Close()

	tools := make([]ToolEntry, 0)
	for rows.Next() {
		var entry ToolEntry
		if err := rows.Scan(
			&entry.Name,
			&entry.Invocations,
			&entry.Successes,
			&entry.Failures,
			&entry.Sessions,
		); err != nil {
			return ToolStats{}, fmt.Errorf("failed to scan tool row: %w", err)
		}
		tools = append(tools, entry)
	}

	if err := rows.Err(); err != nil {
		return ToolStats{}, fmt.Errorf("error iterating tool rows: %w", err)
	}

	return ToolStats{Tools: tools}, nil
}

// toolsLegacy is the old Go streaming path, kept as fallback behind OPCODE_TOOLS_LEGACY=true.
func toolsLegacy(ctx context.Context, st *store.Store, startMs, endMs int64) (ToolStats, error) {
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
