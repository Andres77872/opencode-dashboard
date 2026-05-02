package stats

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"time"

	"opencode-dashboard/internal/store"
)

func Projects(ctx context.Context, s *store.Store, period string) (ProjectStats, error) {
	if !s.IsValidSchema() {
		return ProjectStats{}, store.ErrInvalidSchema
	}

	pw, err := ComputePeriodWindow(ctx, s, period)
	if err != nil {
		return ProjectStats{}, err
	}

	startMs := pw.StartMs
	endMs := pw.EndMs

	db := s.DB()

	query := `
		SELECT 
			p.id,
			p.worktree,
			p.name,
			COUNT(DISTINCT CASE WHEN m.id IS NOT NULL THEN s.id END) AS session_count,
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
				AND time_created >= ? AND time_created < ?
		) m ON m.session_id = s.id
		GROUP BY p.id
		ORDER BY total_cost DESC
	`

	rows, err := db.QueryContext(ctx, query, startMs, endMs)
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

// ProjectByID returns aggregate stats and recent sessions for a specific project.
// Returns nil if the project does not exist.
func ProjectByID(ctx context.Context, s *store.Store, id int64, period string, page, limit int) (*ProjectDetail, error) {
	if !s.IsValidSchema() {
		return nil, store.ErrInvalidSchema
	}

	db := s.DB()

	// Verify project exists and get metadata
	var projectID, worktree string
	var name sql.NullString
	err := db.QueryRowContext(ctx, "SELECT id, name, worktree FROM project WHERE id = ?", id).Scan(&projectID, &name, &worktree)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	pw, err := ComputePeriodWindow(ctx, s, period)
	if err != nil {
		return nil, err
	}

	// Aggregate stats for this project
	aggQuery := `
		SELECT
			COUNT(DISTINCT s.id) AS sessions,
			COUNT(m.id) AS messages,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.cost') AS REAL)), 0) AS total_cost,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.input') AS INTEGER)), 0) AS input_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.output') AS INTEGER)), 0) AS output_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.reasoning') AS INTEGER)), 0) AS reasoning_tokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.read') AS INTEGER)), 0) AS cache_read,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.write') AS INTEGER)), 0) AS cache_write
		FROM session s
		LEFT JOIN message m ON m.session_id = s.id
			AND JSON_EXTRACT(m.data, '$.role') = 'assistant'
			AND m.time_created >= ? AND m.time_created < ?
		WHERE s.project_id = ?
	`
	var sessions, messages int64
	var cost float64
	var input, output, reasoning, cacheRead, cacheWrite int64
	err = db.QueryRowContext(ctx, aggQuery, pw.StartMs, pw.EndMs, projectID).Scan(
		&sessions, &messages, &cost,
		&input, &output, &reasoning, &cacheRead, &cacheWrite,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			sessions, messages = 0, 0
			cost = 0
			input, output, reasoning, cacheRead, cacheWrite = 0, 0, 0, 0, 0
		} else {
			return nil, err
		}
	}

	// Validate pagination
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	// Total session count for pagination
	var totalSessions int64
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM session WHERE project_id = ?", projectID,
	).Scan(&totalSessions)
	if err != nil {
		if err == sql.ErrNoRows {
			totalSessions = 0
		} else {
			return nil, err
		}
	}

	// Recent sessions paginated
	recentQuery := `
		SELECT
			s.id,
			s.title,
			s.project_id,
			p.name,
			p.worktree,
			s.time_created,
			s.time_updated,
			(SELECT COUNT(*) FROM message m2 WHERE m2.session_id = s.id) AS message_count,
			COALESCE((SELECT SUM(CAST(JSON_EXTRACT(m3.data, '$.cost') AS REAL)) FROM message m3 WHERE m3.session_id = s.id AND JSON_EXTRACT(m3.data, '$.role') = 'assistant'), 0) AS total_cost
		FROM session s
		LEFT JOIN project p ON p.id = s.project_id
		WHERE s.project_id = ?
		ORDER BY s.time_created DESC
		LIMIT ? OFFSET ?
	`

	recentRows, err := db.QueryContext(ctx, recentQuery, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer recentRows.Close()

	recentSessions := make([]SessionEntry, 0)
	for recentRows.Next() {
		var (
			sessionID     string
			title         sql.NullString
			pid           sql.NullString
			pname         sql.NullString
			wtree         sql.NullString
			timeCreatedMs int64
			timeUpdatedMs int64
			msgCount      int64
			totalCost     float64
		)

		if err := recentRows.Scan(&sessionID, &title, &pid, &pname, &wtree, &timeCreatedMs, &timeUpdatedMs, &msgCount, &totalCost); err != nil {
			return nil, err
		}

		displayName := resolveProjectName(
			pid.String,
			pname.String,
			wtree.String,
		)

		recentSessions = append(recentSessions, SessionEntry{
			ID:           sessionID,
			Title:        title.String,
			ProjectID:    pid.String,
			ProjectName:  displayName,
			TimeCreated:  time.UnixMilli(timeCreatedMs).UTC(),
			TimeUpdated:  time.UnixMilli(timeUpdatedMs).UTC(),
			MessageCount: msgCount,
			Cost:         totalCost,
		})
	}

	if err := recentRows.Err(); err != nil {
		return nil, err
	}

	projectName := resolveProjectName(projectID, name.String, worktree)

	return &ProjectDetail{
		ProjectID:   projectID,
		ProjectName: projectName,
		Worktree:    worktree,
		Sessions:    sessions,
		Messages:    messages,
		Cost:        cost,
		Tokens: TokenStats{
			Input:     input,
			Output:    output,
			Reasoning: reasoning,
			Cache: CacheStats{
				Read:  cacheRead,
				Write: cacheWrite,
			},
		},
		RecentSessions: recentSessions,
		TotalSessions:  totalSessions,
	}, nil
}
