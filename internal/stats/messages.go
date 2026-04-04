package stats

import (
	"context"
	"database/sql"
	"time"

	"opencode-dashboard/internal/store"
)

// MessagesByPeriod returns a paginated list of messages across all sessions
// within the specified time period. Messages are ordered by creation time descending.
func MessagesByPeriod(ctx context.Context, s *store.Store, period string, page, limit int, sort MessageSort) (MessageList, error) {
	if !s.IsValidSchema() {
		return MessageList{}, store.ErrInvalidSchema
	}

	// Parse period using the same logic as Daily
	days, err := parsePeriod(period)
	if err != nil {
		return MessageList{}, err
	}

	// Calculate date range
	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endMs := endDate.AddDate(0, 0, 1).UnixMilli() // Include full last day

	var startMs int64
	if days == allHistoricPeriodDays {
		// Query earliest activity date for "all" period
		earliest, err := queryEarliestActivityDate(ctx, s)
		if err != nil {
			return MessageList{}, err
		}
		if earliest.IsZero() {
			startMs = endDate.UnixMilli()
		} else {
			startMs = earliest.UnixMilli()
		}
	} else {
		startDate := endDate.AddDate(0, 0, -days+1)
		startMs = startDate.UnixMilli()
	}

	// Validate pagination parameters
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	db := s.DB()

	// Get total count
	var total int64
	countQuery := `
		SELECT COUNT(*)
		FROM message m
		WHERE m.time_created >= ? AND m.time_created < ?
	`
	err = db.QueryRowContext(ctx, countQuery, startMs, endMs).Scan(&total)
	if err != nil {
		if err == sql.ErrNoRows {
			total = 0
		} else {
			return MessageList{}, err
		}
	}

	// Query messages with session join
	listQuery := `
		SELECT 
			m.id,
			m.session_id,
			s.title,
			JSON_EXTRACT(m.data, '$.role') AS role,
			m.time_created,
			COALESCE(JSON_EXTRACT(m.data, '$.cost'), 0) AS cost,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.input'), 0) AS input_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.output'), 0) AS output_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.reasoning'), 0) AS reasoning_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.read'), 0) AS cache_read,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.write'), 0) AS cache_write,
			JSON_EXTRACT(m.data, '$.modelID') AS model_id,
			JSON_EXTRACT(m.data, '$.providerID') AS provider_id
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		WHERE m.time_created >= ? AND m.time_created < ?
		ORDER BY ` + sort.OrderByClause() + `
		LIMIT ? OFFSET ?
	`

	rows, err := db.QueryContext(ctx, listQuery, startMs, endMs, limit, offset)
	if err != nil {
		return MessageList{}, err
	}
	defer rows.Close()

	var messages []MessageEntry
	for rows.Next() {
		var (
			id            string
			sessionID     string
			title         sql.NullString
			role          string
			timeCreatedMs int64
			cost          float64
			inputTokens   int64
			outputTokens  int64
			reasoning     int64
			cacheRead     int64
			cacheWrite    int64
			modelID       sql.NullString
			providerID    sql.NullString
		)

		if err := rows.Scan(
			&id,
			&sessionID,
			&title,
			&role,
			&timeCreatedMs,
			&cost,
			&inputTokens,
			&outputTokens,
			&reasoning,
			&cacheRead,
			&cacheWrite,
			&modelID,
			&providerID,
		); err != nil {
			return MessageList{}, err
		}

		entry := MessageEntry{
			ID:           id,
			SessionID:    sessionID,
			SessionTitle: title.String,
			Role:         role,
			TimeCreated:  time.UnixMilli(timeCreatedMs).UTC(),
		}

		// Only include cost/tokens/model info for assistant messages
		if role == "assistant" {
			entry.Cost = cost
			entry.Tokens = &TokenStats{
				Input:     inputTokens,
				Output:    outputTokens,
				Reasoning: reasoning,
				Cache: CacheStats{
					Read:  cacheRead,
					Write: cacheWrite,
				},
			}
			if modelID.Valid {
				entry.ModelID = modelID.String
			}
			if providerID.Valid {
				entry.ProviderID = providerID.String
			}
		}

		messages = append(messages, entry)
	}

	if err := rows.Err(); err != nil {
		return MessageList{}, err
	}

	if messages == nil {
		messages = []MessageEntry{}
	}

	return MessageList{
		Messages: messages,
		Total:    total,
		Page:     page,
		PageSize: limit,
	}, nil
}

// MessageByID returns detailed information about a specific message,
// including its text and reasoning content from the part table.
// Returns nil if the message does not exist.
func MessageByID(ctx context.Context, s *store.Store, id string) (*MessageDetail, error) {
	if !s.IsValidSchema() {
		return nil, store.ErrInvalidSchema
	}

	if id == "" {
		return nil, nil
	}

	db := s.DB()

	// Query message metadata
	messageQuery := `
		SELECT 
			m.id,
			m.session_id,
			s.title,
			JSON_EXTRACT(m.data, '$.role') AS role,
			m.time_created,
			COALESCE(JSON_EXTRACT(m.data, '$.cost'), 0) AS cost,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.input'), 0) AS input_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.output'), 0) AS output_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.reasoning'), 0) AS reasoning_tokens,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.read'), 0) AS cache_read,
			COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.write'), 0) AS cache_write,
			JSON_EXTRACT(m.data, '$.modelID') AS model_id,
			JSON_EXTRACT(m.data, '$.providerID') AS provider_id
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		WHERE m.id = ?
	`

	var (
		msgID         string
		sessionID     string
		title         sql.NullString
		role          string
		timeCreatedMs int64
		cost          float64
		inputTokens   int64
		outputTokens  int64
		reasoning     int64
		cacheRead     int64
		cacheWrite    int64
		modelID       sql.NullString
		providerID    sql.NullString
	)

	err := db.QueryRowContext(ctx, messageQuery, id).Scan(
		&msgID,
		&sessionID,
		&title,
		&role,
		&timeCreatedMs,
		&cost,
		&inputTokens,
		&outputTokens,
		&reasoning,
		&cacheRead,
		&cacheWrite,
		&modelID,
		&providerID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	entry := MessageEntry{
		ID:           msgID,
		SessionID:    sessionID,
		SessionTitle: title.String,
		Role:         role,
		TimeCreated:  time.UnixMilli(timeCreatedMs).UTC(),
	}

	if role == "assistant" {
		entry.Cost = cost
		entry.Tokens = &TokenStats{
			Input:     inputTokens,
			Output:    outputTokens,
			Reasoning: reasoning,
			Cache: CacheStats{
				Read:  cacheRead,
				Write: cacheWrite,
			},
		}
		if modelID.Valid {
			entry.ModelID = modelID.String
		}
		if providerID.Valid {
			entry.ProviderID = providerID.String
		}
	}

	// Query part table for text/reasoning content
	contentQuery := `
		SELECT 
			JSON_EXTRACT(data, '$.type') AS type,
			JSON_EXTRACT(data, '$.text') AS text
		FROM part
		WHERE message_id = ?
			AND JSON_EXTRACT(data, '$.type') IN ('text', 'reasoning')
		ORDER BY id
	`

	contentRows, err := db.QueryContext(ctx, contentQuery, id)
	if err != nil {
		return nil, err
	}
	defer contentRows.Close()

	var textParts []MessagePart
	var reasoningParts []MessagePart

	for contentRows.Next() {
		var partType sql.NullString
		var partText sql.NullString

		if err := contentRows.Scan(&partType, &partText); err != nil {
			return nil, err
		}

		if !partType.Valid || !partText.Valid {
			continue
		}

		part := MessagePart{
			Type: partType.String,
			Text: truncateContent(partText.String, 500),
		}

		if partType.String == "text" {
			textParts = append(textParts, part)
		} else if partType.String == "reasoning" {
			reasoningParts = append(reasoningParts, part)
		}
	}

	if err := contentRows.Err(); err != nil {
		return nil, err
	}

	// Ensure arrays are not nil for JSON serialization
	if textParts == nil {
		textParts = []MessagePart{}
	}
	if reasoningParts == nil {
		reasoningParts = []MessagePart{}
	}

	return &MessageDetail{
		MessageEntry: entry,
		Content: MessageContent{
			TextParts:      textParts,
			ReasoningParts: reasoningParts,
		},
	}, nil
}

// truncateContent truncates content to approximately maxChars characters.
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	// Truncate and add ellipsis indicator
	return content[:maxChars] + "..."
}
