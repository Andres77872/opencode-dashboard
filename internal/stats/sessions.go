package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"opencode-dashboard/internal/store"
)

// Sessions returns a paginated list of sessions with summary information.
// Each entry includes the session's title, project, timestamps, message count, and total cost.
// Sessions are ordered by creation date descending (most recent first).
func Sessions(ctx context.Context, s *store.Store, page, limit int) (SessionList, error) {
	return SessionsWithQuery(ctx, s, SessionQuery{Page: page, PageSize: limit, Sort: SessionSortNewest})
}

// SessionsWithQuery returns a paginated list of sessions with optional filter and sort controls.
func SessionsWithQuery(ctx context.Context, s *store.Store, query SessionQuery) (SessionList, error) {
	if !s.IsValidSchema() {
		return SessionList{}, store.ErrInvalidSchema
	}

	db := s.DB()

	// Ensure valid pagination parameters
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.Sort == "" {
		query.Sort = SessionSortNewest
	}

	filter := strings.TrimSpace(query.Filter)
	filterLike := "%" + filter + "%"
	offset := (query.Page - 1) * query.PageSize
	orderBy := sessionOrderBy(query.Sort)

	// Compute period window if period is specified
	var startMs, endMs int64
	var hasPeriod bool
	if query.Period != "" {
		days, err := parsePeriod(query.Period)
		if err != nil {
			return SessionList{}, err
		}

		now := time.Now().UTC()
		endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		startDate := endDate

		if days == allHistoricPeriodDays {
			startDate, err = queryEarliestActivityDate(ctx, s)
			if err != nil {
				return SessionList{}, fmt.Errorf("query earliest activity date: %w", err)
			}
			if startDate.IsZero() {
				startDate = endDate
			}
		} else if days > 0 {
			startDate = endDate.AddDate(0, 0, -days+1)
		}

		startMs = startDate.UnixMilli()
		endMs = endDate.AddDate(0, 0, 1).UnixMilli()
		hasPeriod = true
	}

	// Get total count first
	countQuery := `
		SELECT COUNT(*)
		FROM session s
		LEFT JOIN project p ON p.id = s.project_id
		WHERE (? = '' OR LOWER(COALESCE(s.title, '')) LIKE LOWER(?) OR LOWER(COALESCE(p.name, p.worktree, '')) LIKE LOWER(?))
	`
	countArgs := []interface{}{filter, filterLike, filterLike}

	if hasPeriod {
		countQuery += ` AND EXISTS (
			SELECT 1 FROM message m
			WHERE m.session_id = s.id
				AND m.time_created >= ? AND m.time_created < ?
		)`
		countArgs = append(countArgs, startMs, endMs)
	}

	var total int64
	err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		if err == sql.ErrNoRows {
			total = 0
		} else {
			return SessionList{}, err
		}
	}

	// Query sessions with LEFT JOINs for project info and message aggregates
	listQuery := `
		SELECT 
			s.id,
			s.title,
			s.project_id,
			p.name,
			p.worktree,
			s.time_created,
			s.time_updated,
			COUNT(m.id) AS message_count,
			COALESCE(SUM(m.cost), 0) AS total_cost
		FROM session s
		LEFT JOIN project p ON p.id = s.project_id
		LEFT JOIN (
			SELECT 
				session_id,
				id,
				COALESCE(JSON_EXTRACT(data, '$.cost'), 0) AS cost
			FROM message
			WHERE JSON_EXTRACT(data, '$.role') = 'assistant'
				AND time_created >= ? AND time_created < ?
		) m ON m.session_id = s.id
		WHERE (? = '' OR LOWER(COALESCE(s.title, '')) LIKE LOWER(?) OR LOWER(COALESCE(p.name, p.worktree, '')) LIKE LOWER(?))
	`
	listArgs := []interface{}{startMs, endMs, filter, filterLike, filterLike}

	if hasPeriod {
		listQuery += ` AND EXISTS (
			SELECT 1 FROM message m2
			WHERE m2.session_id = s.id
				AND m2.time_created >= ? AND m2.time_created < ?
		)`
		listArgs = append(listArgs, startMs, endMs)
	}

	listQuery += `
		GROUP BY s.id
		ORDER BY ` + orderBy + `
		LIMIT ? OFFSET ?
	`
	listArgs = append(listArgs, query.PageSize, offset)

	rows, err := db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return SessionList{}, err
	}
	defer rows.Close()

	var sessions []SessionEntry
	for rows.Next() {
		var (
			id            string
			title         sql.NullString
			projectID     sql.NullString
			projectName   sql.NullString
			worktree      sql.NullString
			timeCreatedMs int64
			timeUpdatedMs int64
			messageCount  int64
			totalCost     float64
		)

		if err := rows.Scan(
			&id,
			&title,
			&projectID,
			&projectName,
			&worktree,
			&timeCreatedMs,
			&timeUpdatedMs,
			&messageCount,
			&totalCost,
		); err != nil {
			return SessionList{}, err
		}

		projectDisplayName := resolveProjectName(
			projectID.String,
			projectName.String,
			worktree.String,
		)

		sessions = append(sessions, SessionEntry{
			ID:           id,
			Title:        title.String,
			ProjectID:    projectID.String,
			ProjectName:  projectDisplayName,
			TimeCreated:  time.UnixMilli(timeCreatedMs).UTC(),
			TimeUpdated:  time.UnixMilli(timeUpdatedMs).UTC(),
			MessageCount: messageCount,
			Cost:         totalCost,
		})
	}

	if err := rows.Err(); err != nil {
		return SessionList{}, err
	}

	if sessions == nil {
		sessions = []SessionEntry{}
	}

	return SessionList{
		Sessions: sessions,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func sessionOrderBy(sort SessionSortMode) string {
	switch sort {
	case SessionSortOldest:
		return "s.time_created ASC"
	case SessionSortCost:
		return "total_cost DESC, s.time_created DESC"
	case SessionSortMessages:
		return "message_count DESC, s.time_created DESC"
	default:
		return "s.time_created DESC"
	}
}

// SessionByID returns detailed information about a specific session,
// including all messages with their role, model, cost, and token consumption.
// Returns nil if the session does not exist.
func SessionByID(ctx context.Context, s *store.Store, id string) (*SessionDetail, error) {
	if !s.IsValidSchema() {
		return nil, store.ErrInvalidSchema
	}

	if id == "" {
		return nil, sql.ErrNoRows
	}

	db := s.DB()

	// Query session metadata
	sessionQuery := `
		SELECT 
			s.id,
			s.title,
			s.project_id,
			p.name,
			p.worktree,
			s.directory,
			s.time_created,
			s.time_updated
		FROM session s
		LEFT JOIN project p ON p.id = s.project_id
		WHERE s.id = ?
	`

	var (
		sessionID     string
		title         sql.NullString
		projectID     sql.NullString
		projectName   sql.NullString
		worktree      sql.NullString
		directory     sql.NullString
		timeCreatedMs int64
		timeUpdatedMs int64
	)

	err := db.QueryRowContext(ctx, sessionQuery, id).Scan(
		&sessionID,
		&title,
		&projectID,
		&projectName,
		&worktree,
		&directory,
		&timeCreatedMs,
		&timeUpdatedMs,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	timeCreated := time.UnixMilli(timeCreatedMs).UTC()
	timeUpdated := time.UnixMilli(timeUpdatedMs).UTC()

	// Query messages for this session
	messageQuery := `
		SELECT 
			m.id,
			JSON_EXTRACT(m.data, '$.role') AS role,
			m.time_created,
			COALESCE(JSON_EXTRACT(m.data, '$.cost'), 0) AS cost,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.input'), 0) AS input_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.output'), 0) AS output_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.reasoning'), 0) AS reasoning_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.read'), 0) AS cache_read,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.write'), 0) AS cache_write,
			JSON_EXTRACT(m.data, '$.modelID') AS model_id,
			JSON_EXTRACT(m.data, '$.providerID') AS provider_id,
			JSON_EXTRACT(m.data, '$.agent') AS agent
		FROM message m
		WHERE m.session_id = ?
		ORDER BY m.time_created ASC
	`

	rows, err := db.QueryContext(ctx, messageQuery, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []SessionMessage
	var totalCost float64
	var totalInput, totalOutput, totalReasoning, totalCacheRead, totalCacheWrite int64

	for rows.Next() {
		var (
			msgID            string
			role             string
			msgTimeCreatedMs int64
			msgCost          float64
			inputTokens      int64
			outputTokens     int64
			reasoningTokens  int64
			cacheRead        int64
			cacheWrite       int64
			modelID          sql.NullString
			providerID       sql.NullString
			agent            sql.NullString
		)

		if err := rows.Scan(
			&msgID,
			&role,
			&msgTimeCreatedMs,
			&msgCost,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cacheRead,
			&cacheWrite,
			&modelID,
			&providerID,
			&agent,
		); err != nil {
			return nil, err
		}

		if role == "assistant" {
			totalCost += msgCost
			totalInput += inputTokens
			totalOutput += outputTokens
			totalReasoning += reasoningTokens
			totalCacheRead += cacheRead
			totalCacheWrite += cacheWrite
		}

		msg := SessionMessage{
			ID:          msgID,
			Role:        role,
			TimeCreated: time.UnixMilli(msgTimeCreatedMs).UTC(),
		}

		// Only include cost/tokens/model info for assistant messages
		if role == "assistant" {
			msg.Cost = msgCost
			msg.Tokens = &TokenStats{
				Input:     inputTokens,
				Output:    outputTokens,
				Reasoning: reasoningTokens,
				Cache: CacheStats{
					Read:  cacheRead,
					Write: cacheWrite,
				},
			}
			if modelID.Valid {
				msg.ModelID = modelID.String
			}
			if providerID.Valid {
				msg.ProviderID = providerID.String
			}
			if agent.Valid {
				msg.Agent = agent.String
			}
		}

		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if messages == nil {
		messages = []SessionMessage{}
	}

	// Resolve project name
	projectDisplayName := resolveProjectName(
		projectID.String,
		projectName.String,
		worktree.String,
	)

	return &SessionDetail{
		ID:          sessionID,
		Title:       title.String,
		ProjectID:   projectID.String,
		ProjectName: projectDisplayName,
		Directory:   directory.String,
		TimeCreated: timeCreated,
		TimeUpdated: timeUpdated,
		Messages:    messages,
		TotalCost:   totalCost,
		TotalTokens: TokenStats{
			Input:     totalInput,
			Output:    totalOutput,
			Reasoning: totalReasoning,
			Cache: CacheStats{
				Read:  totalCacheRead,
				Write: totalCacheWrite,
			},
		},
		MessageCount: int64(len(messages)),
	}, nil
}
