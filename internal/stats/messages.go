package stats

import (
	"context"
	"database/sql"
	"encoding/json"
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
			Text: truncateContent(partText.String, 1000),
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

	// Query tool parts from part.data
	toolQuery := `
		SELECT data
		FROM part
		WHERE message_id = ?
			AND JSON_EXTRACT(data, '$.type') = 'tool'
		ORDER BY id
	`

	toolRows, err := db.QueryContext(ctx, toolQuery, id)
	if err != nil {
		return nil, err
	}
	defer toolRows.Close()

	var toolParts []ToolPart

	for toolRows.Next() {
		var dataJSON string
		if err := toolRows.Scan(&dataJSON); err != nil {
			return nil, err
		}

		toolPart, err := parseToolPart(dataJSON)
		if err != nil {
			continue
		}

		toolParts = append(toolParts, toolPart)
	}

	if err := toolRows.Err(); err != nil {
		return nil, err
	}

	// Ensure arrays are not nil for JSON serialization
	if textParts == nil {
		textParts = []MessagePart{}
	}
	if reasoningParts == nil {
		reasoningParts = []MessagePart{}
	}
	if toolParts == nil {
		toolParts = []ToolPart{}
	}

	return &MessageDetail{
		MessageEntry: entry,
		Content: MessageContent{
			TextParts:      textParts,
			ReasoningParts: reasoningParts,
			ToolParts:      toolParts,
		},
	}, nil
}

// truncateContent truncates content to approximately maxChars characters.
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "..."
}

const toolContentMaxChars = 2000

func parseToolPart(dataJSON string) (ToolPart, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &raw); err != nil {
		return ToolPart{}, err
	}

	callID, _ := raw["callID"].(string)
	tool, _ := raw["tool"].(string)

	state := ToolState{}
	if stateRaw, ok := raw["state"].(map[string]interface{}); ok {
		state.Status, _ = stateRaw["status"].(string)

		if input, ok := stateRaw["input"].(map[string]interface{}); ok {
			state.Input = truncateToolInput(input)
		}

		if output, ok := stateRaw["output"].(string); ok {
			state.Output = truncateContent(output, toolContentMaxChars)
		}

		state.Title, _ = stateRaw["title"].(string)
		state.Error, _ = stateRaw["error"].(string)

		if meta, ok := stateRaw["metadata"].(map[string]interface{}); ok {
			state.Metadata = meta
		}

		if timeRaw, ok := stateRaw["time"].(map[string]interface{}); ok {
			t := &ToolTime{}
			if start, ok := timeRaw["start"].(float64); ok {
				t.Start = int64(start)
			}
			if end, ok := timeRaw["end"].(float64); ok {
				t.End = int64(end)
			}
			if compacted, ok := timeRaw["compacted"].(float64); ok {
				t.Compacted = int64(compacted)
			}
			state.Time = t
		}
	}

	return ToolPart{
		Type:   "tool",
		CallID: callID,
		Tool:   tool,
		State:  state,
	}, nil
}

func truncateToolInput(input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range input {
		if str, ok := v.(string); ok && len(str) > toolContentMaxChars {
			result[k] = truncateContent(str, toolContentMaxChars)
		} else {
			result[k] = v
		}
	}
	return result
}
