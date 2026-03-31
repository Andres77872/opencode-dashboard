package stats

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"

	"opencode-dashboard/internal/store"
)

func Projects(ctx context.Context, s *store.Store) (ProjectStats, error) {
	if !s.IsValidSchema() {
		return ProjectStats{}, store.ErrInvalidSchema
	}

	db := s.DB()

	query := `
		SELECT 
			p.id,
			p.worktree,
			p.name,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(m.id) AS message_count,
			COALESCE(SUM(m.cost), 0) AS total_cost,
			COALESCE(SUM(m.input_tokens), 0) AS total_input,
			COALESCE(SUM(m.output_tokens), 0) AS total_output,
			COALESCE(SUM(m.reasoning_tokens), 0) AS total_reasoning,
			COALESCE(SUM(m.cache_read), 0) AS total_cache_read,
			COALESCE(SUM(m.cache_write), 0) AS total_cache_write
		FROM project p
		LEFT JOIN session s ON s.project_id = p.id
		LEFT JOIN (
			SELECT 
				session_id,
				id,
				COALESCE(JSON_EXTRACT(data, '$.cost'), 0) AS cost,
				COALESCE(JSON_EXTRACT(data, '$.tokens.input'), 0) AS input_tokens,
				COALESCE(JSON_EXTRACT(data, '$.tokens.output'), 0) AS output_tokens,
				COALESCE(JSON_EXTRACT(data, '$.tokens.reasoning'), 0) AS reasoning_tokens,
				COALESCE(JSON_EXTRACT(data, '$.tokens.cache.read'), 0) AS cache_read,
				COALESCE(JSON_EXTRACT(data, '$.tokens.cache.write'), 0) AS cache_write
			FROM message
			WHERE JSON_EXTRACT(data, '$.role') = 'assistant'
		) m ON m.session_id = s.id
		GROUP BY p.id
		ORDER BY total_cost DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return ProjectStats{}, err
	}
	defer rows.Close()

	var projects []ProjectEntry
	for rows.Next() {
		var (
			projectID       string
			worktree        sql.NullString
			name            sql.NullString
			sessionCount    int64
			messageCount    int64
			totalCost       float64
			totalInput      int64
			totalOutput     int64
			totalReasoning  int64
			totalCacheRead  int64
			totalCacheWrite int64
		)

		if err := rows.Scan(
			&projectID,
			&worktree,
			&name,
			&sessionCount,
			&messageCount,
			&totalCost,
			&totalInput,
			&totalOutput,
			&totalReasoning,
			&totalCacheRead,
			&totalCacheWrite,
		); err != nil {
			return ProjectStats{}, err
		}

		projectName := resolveProjectName(projectID, name.String, worktree.String)

		projects = append(projects, ProjectEntry{
			ProjectID:   projectID,
			ProjectName: projectName,
			Sessions:    sessionCount,
			Messages:    messageCount,
			Cost:        totalCost,
			Tokens: TokenStats{
				Input:     totalInput,
				Output:    totalOutput,
				Reasoning: totalReasoning,
				Cache: CacheStats{
					Read:  totalCacheRead,
					Write: totalCacheWrite,
				},
			},
		})
	}

	if err := rows.Err(); err != nil {
		return ProjectStats{}, err
	}

	if projects == nil {
		projects = []ProjectEntry{}
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Cost > projects[j].Cost
	})

	return ProjectStats{Projects: projects}, nil
}

func resolveProjectName(projectID string, name string, worktree string) string {
	if name != "" {
		return name
	}

	if worktree != "" {
		return filepath.Base(worktree)
	}

	if len(projectID) > 8 {
		return projectID[:8]
	}
	return projectID
}
