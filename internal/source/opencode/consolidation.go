package opencode

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
)

var _ source.ConsolidationSource = (*Source)(nil)

// ConsolidationData exports cache-safe OpenCode metadata in one SQLite read
// transaction. The generic cache collector paginates messages and then calls
// MessageByID for every row; on a large database that repeats counts/sorts and
// executes two part queries per message. This path scans the requested message
// window once and loads all tool metadata in one indexed join, while keeping
// message text, reasoning, tool inputs, and tool outputs out of the cache.
func (s *Source) ConsolidationData(ctx context.Context, pq stats.PeriodQuery) (source.ConsolidationData, error) {
	if err := ctx.Err(); err != nil {
		return source.ConsolidationData{}, err
	}
	if s == nil || s.store == nil || !s.store.IsValidSchema() {
		return source.ConsolidationData{}, store.ErrInvalidSchema
	}

	window, err := stats.ComputePeriodWindowFromQuery(ctx, s.store, pq)
	if err != nil {
		return source.ConsolidationData{}, err
	}
	tx, err := s.store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return source.ConsolidationData{}, err
	}
	defer tx.Rollback()

	data, messageIndexes, err := consolidationMessages(ctx, tx, window.StartMs, window.EndMs)
	if err != nil {
		return source.ConsolidationData{}, err
	}
	if err := consolidationParts(ctx, tx, window.StartMs, window.EndMs, data.Messages, messageIndexes); err != nil {
		return source.ConsolidationData{}, err
	}
	if err := tx.Commit(); err != nil {
		return source.ConsolidationData{}, err
	}

	data.CostStatus = stats.CostReported
	data.CostProvenance = reportedCost()
	return data, nil
}

func consolidationMessages(ctx context.Context, tx *sql.Tx, startMs, endMs int64) (source.ConsolidationData, map[string]int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			m.id,
			m.session_id,
			m.time_created,
			json_extract(m.data, '$.role'),
			COALESCE(json_extract(m.data, '$.cost'), 0),
			COALESCE(json_extract(m.data, '$.tokens.input'), 0),
			COALESCE(json_extract(m.data, '$.tokens.output'), 0),
			COALESCE(json_extract(m.data, '$.tokens.reasoning'), 0),
			COALESCE(json_extract(m.data, '$.tokens.cache.read'), 0),
			COALESCE(json_extract(m.data, '$.tokens.cache.write'), 0),
			json_extract(m.data, '$.modelID'),
			json_extract(m.data, '$.providerID'),
			COALESCE(s.project_id, ''),
			COALESCE(p.name, ''),
			COALESCE(p.worktree, ''),
			COALESCE(s.time_created, m.time_created),
			COALESCE(s.time_updated, m.time_created)
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		LEFT JOIN project p ON p.id = s.project_id
		WHERE m.time_created >= ? AND m.time_created < ?
	`, startMs, endMs)
	if err != nil {
		return source.ConsolidationData{}, nil, err
	}
	defer rows.Close()

	data := source.ConsolidationData{
		Sessions: make([]stats.SessionEntry, 0),
		Messages: make([]source.ConsolidationMessage, 0),
	}
	messageIndexes := make(map[string]int)
	sessionIndexes := make(map[string]int)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return source.ConsolidationData{}, nil, err
		}
		var (
			messageID        string
			sessionID        string
			messageCreatedMS int64
			role             string
			cost             float64
			input            int64
			output           int64
			reasoning        int64
			cacheRead        int64
			cacheWrite       int64
			modelID          sql.NullString
			providerID       sql.NullString
			projectID        string
			projectName      string
			worktree         string
			sessionCreatedMS int64
			sessionUpdatedMS int64
		)
		if err := rows.Scan(
			&messageID, &sessionID, &messageCreatedMS, &role, &cost,
			&input, &output, &reasoning, &cacheRead, &cacheWrite,
			&modelID, &providerID, &projectID, &projectName, &worktree,
			&sessionCreatedMS, &sessionUpdatedMS,
		); err != nil {
			return source.ConsolidationData{}, nil, err
		}

		entry := stats.MessageEntry{
			ID:          messageID,
			SessionID:   sessionID,
			Role:        role,
			TimeCreated: time.UnixMilli(messageCreatedMS).UTC(),
		}
		if role == "assistant" {
			entry.Cost = cost
			entry.Tokens = &stats.TokenStats{
				Input:     input,
				Output:    output,
				Reasoning: reasoning,
				Cache: stats.CacheStats{
					Read:  cacheRead,
					Write: cacheWrite,
				},
			}
			entry.ModelID = modelID.String
			entry.ProviderID = providerID.String
			entry.CostStatus = stats.CostReported
			entry.CostProvenance = reportedCost()
		}
		messageIndexes[messageID] = len(data.Messages)
		data.Messages = append(data.Messages, source.ConsolidationMessage{Entry: entry})

		sessionIndex, ok := sessionIndexes[sessionID]
		if !ok {
			sessionIndex = len(data.Sessions)
			sessionIndexes[sessionID] = sessionIndex
			data.Sessions = append(data.Sessions, stats.SessionEntry{
				ID:             sessionID,
				ProjectID:      projectID,
				ProjectName:    consolidationProjectName(projectID, projectName, worktree),
				TimeCreated:    time.UnixMilli(sessionCreatedMS).UTC(),
				TimeUpdated:    time.UnixMilli(sessionUpdatedMS).UTC(),
				CostStatus:     stats.CostReported,
				CostProvenance: reportedCost(),
			})
		}
		session := &data.Sessions[sessionIndex]
		session.MessageCount++
		if role == "assistant" {
			session.Cost += cost
		}
	}
	if err := rows.Err(); err != nil {
		return source.ConsolidationData{}, nil, err
	}
	return data, messageIndexes, nil
}

func consolidationParts(ctx context.Context, tx *sql.Tx, startMs, endMs int64, messages []source.ConsolidationMessage, messageIndexes map[string]int) error {
	// CROSS JOIN fixes filtered_messages as the outer loop. With OpenCode's
	// part(message_id, id) index this performs targeted lookups for the requested
	// window instead of scanning/parsing unrelated historical part payloads.
	// Tool metadata and step-finish token totals share this scan so a large
	// initial cache load does not traverse the part index twice.
	rows, err := tx.QueryContext(ctx, `
		WITH filtered_messages AS MATERIALIZED (
			SELECT id
			FROM message
			WHERE time_created >= ? AND time_created < ?
		)
		SELECT
			p.message_id,
			json_extract(p.data, '$.type'),
			COALESCE(json_extract(p.data, '$.tool'), ''),
			COALESCE(json_extract(p.data, '$.state.status'), ''),
			COALESCE(json_extract(p.data, '$.tokens.input'), 0),
			COALESCE(json_extract(p.data, '$.tokens.output'), 0),
			COALESCE(json_extract(p.data, '$.tokens.reasoning'), 0),
			COALESCE(json_extract(p.data, '$.tokens.cache.read'), 0),
			COALESCE(json_extract(p.data, '$.tokens.cache.write'), 0)
		FROM filtered_messages m
		CROSS JOIN part p
		WHERE p.message_id = m.id
			AND json_extract(p.data, '$.type') IN ('tool', 'step-finish')
	`, startMs, endMs)
	if err != nil {
		return err
	}
	defer rows.Close()
	modelTokens := make(map[int]stats.TokenStats)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var messageID, partType, name, status string
		var input, output, reasoning, cacheRead, cacheWrite int64
		if err := rows.Scan(
			&messageID, &partType, &name, &status,
			&input, &output, &reasoning, &cacheRead, &cacheWrite,
		); err != nil {
			return err
		}
		index, ok := messageIndexes[messageID]
		if !ok {
			continue
		}
		switch partType {
		case "tool":
			if name != "" {
				messages[index].Tools = append(messages[index].Tools, source.ConsolidationTool{
					Name:   name,
					Status: status,
				})
			}
		case "step-finish":
			if messages[index].Entry.Role != "assistant" || messages[index].Entry.ModelID == "" {
				continue
			}
			tokens := modelTokens[index]
			tokens.Input += input
			tokens.Output += output
			tokens.Reasoning += reasoning
			tokens.Cache.Read += cacheRead
			tokens.Cache.Write += cacheWrite
			modelTokens[index] = tokens
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index, tokens := range modelTokens {
		// Keep Entry.Tokens as the message/overview basis. The cache stores this
		// override separately so model parity does not change Overview totals.
		value := tokens
		messages[index].ModelTokens = &value
	}
	return nil
}

func consolidationProjectName(projectID, name, worktree string) string {
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
