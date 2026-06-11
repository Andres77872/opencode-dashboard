package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"opencode-dashboard/internal/stats"
)

// sourceStateMemo caches the per-source watermark row so the read hot path
// (staleness checks, window clamping) does not hit SQLite on every request.
// Entries are invalidated after every state-writing commit and re-seeded
// lazily; this process is the cache's only writer, so the memo cannot go
// stale silently.
type sourceStateMemo struct {
	lastSyncedMs int64
	cutoffMs     int64
}

func (s *Store) invalidateStateMemo(sourceID string) {
	if s == nil || sourceID == "" {
		return
	}
	s.memoMu.Lock()
	delete(s.stateMemo, sourceID)
	s.memoMu.Unlock()
}

func (s *Store) sourceMemo(ctx context.Context, sourceID string) (sourceStateMemo, error) {
	s.memoMu.Lock()
	memo, ok := s.stateMemo[sourceID]
	s.memoMu.Unlock()
	if ok {
		return memo, nil
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT last_synced_ms, last_safe_cutoff_ms FROM source_state WHERE source_id = ?
	`, sourceID).Scan(&memo.lastSyncedMs, &memo.cutoffMs)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sourceStateMemo{}, err
	}
	s.memoMu.Lock()
	if s.stateMemo == nil {
		s.stateMemo = make(map[string]sourceStateMemo)
	}
	s.stateMemo[sourceID] = memo
	s.memoMu.Unlock()
	return memo, nil
}

// LastSafeCutoff returns the source's finality boundary: every cached row is
// strictly older than it. Zero means the source has never been consolidated.
func (s *Store) LastSafeCutoff(ctx context.Context, sourceID string) (time.Time, error) {
	if s == nil || sourceID == "" {
		return time.Time{}, nil
	}
	memo, err := s.sourceMemo(ctx, sourceID)
	if err != nil {
		return time.Time{}, err
	}
	if memo.cutoffMs <= 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(memo.cutoffMs).UTC(), nil
}

// LastSyncedMS returns when the source last completed any consolidation
// write (0 = never). Drives the staleness-triggered background consolidation.
func (s *Store) LastSyncedMS(ctx context.Context, sourceID string) (int64, error) {
	if s == nil || sourceID == "" {
		return 0, nil
	}
	memo, err := s.sourceMemo(ctx, sourceID)
	if err != nil {
		return 0, err
	}
	return memo.lastSyncedMs, nil
}

// ---- dedup and list helpers for the gap-merge layer ----

// sessionIDChunk bounds IN-clause sizes. Each id appears in exactly one
// chunk, so per-chunk DISTINCT counts are additive.
const sessionIDChunk = 500

func chunkStrings(ids []string, size int) [][]string {
	if len(ids) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

func inPlaceholders(n int) string {
	return strings.Repeat(",?", n)[1:]
}

// distinctSessionOverlap counts how many of ids have at least one cached
// message inside [startMs, endMs) matching extraWhere. Used to dedup distinct
// session counts for sessions that span the finality cutoff.
func (s *Store) distinctSessionOverlap(ctx context.Context, sourceID string, startMs, endMs int64, ids []string, extraWhere string, extraArgs []any) (int64, error) {
	var total int64
	for _, chunk := range chunkStrings(ids, sessionIDChunk) {
		query := `SELECT COUNT(DISTINCT session_id) FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ? ` +
			extraWhere + ` AND session_id IN (` + inPlaceholders(len(chunk)) + `)`
		args := append([]any{sourceID, startMs, endMs}, extraArgs...)
		for _, id := range chunk {
			args = append(args, id)
		}
		var count int64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return 0, err
		}
		total += count
	}
	return total, nil
}

// hasActivity reports whether any cached message falls inside [startMs, endMs).
func (s *Store) hasActivity(ctx context.Context, sourceID string, startMs, endMs int64) (bool, error) {
	if endMs <= startMs {
		return false, nil
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ? LIMIT 1`, sourceID, startMs, endMs).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// messagesTotal counts cached messages in the resolved window of pq.
func (s *Store) messagesTotal(ctx context.Context, sourceID string, pq stats.PeriodQuery) (int64, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return 0, err
	}
	startMs, endMs := w.ms()
	var total int64
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`, sourceID, startMs, endMs).Scan(&total)
	return total, err
}

// messagesSlice returns cached messages in the resolved window at an
// arbitrary row offset (the page-based Messages API cannot express the
// offsets a merged gap+cache pagination needs).
func (s *Store) messagesSlice(ctx context.Context, sourceID string, pq stats.PeriodQuery, offset, limit int, sortSpec stats.MessageSort) ([]stats.MessageEntry, error) {
	if limit <= 0 {
		return []stats.MessageEntry{}, nil
	}
	if offset < 0 {
		offset = 0
	}
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return nil, err
	}
	startMs, endMs := w.ms()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			message_id, session_id, session_title, role, time_created_ms, cost,
			input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens,
			COALESCE(model_id, ''), COALESCE(provider_id, ''), COALESCE(agent, ''), is_subagent,
			folded_assistant_calls, folded_tool_calls, folded_token_updates, COALESCE(cost_status, ''), cost_provenance_json
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		ORDER BY `+messageOrderBy(sortSpec)+`
		LIMIT ? OFFSET ?
	`, sourceID, startMs, endMs, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]stats.MessageEntry, 0, limit)
	for rows.Next() {
		entry, err := scanMessageEntry(rows, sourceID)
		if err != nil {
			return nil, err
		}
		messages = append(messages, entry)
	}
	return messages, rows.Err()
}

// costSummaryForPQ resolves pq's cache window and summarizes cost provenance.
func (s *Store) costSummaryForPQ(ctx context.Context, sourceID string, pq stats.PeriodQuery) (stats.CostStatus, *stats.CostProvenance, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return "", nil, err
	}
	startMs, endMs := w.ms()
	status, prov := s.costSummary(ctx, sourceID, startMs, endMs)
	return status, prov, nil
}

// sessionWindowRows runs the cache sessions aggregation (same semantics as
// Store.Sessions) returning the window total, the top `limit` rows in the
// requested order, and — regardless of rank — the rows for every id in ids
// (sessions that gained gap activity can climb the merged order from
// arbitrarily deep in the cache ranking).
func (s *Store) sessionWindowRows(ctx context.Context, sourceID string, query stats.SessionQuery, limit int, ids []string) (int64, []stats.SessionEntry, map[string]stats.SessionEntry, error) {
	pq := stats.PeriodQuery{Period: query.Period, From: query.From, To: query.To, FromTime: query.FromTime, ToTime: query.ToTime}
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return 0, nil, nil, err
	}
	startMs, endMs := w.ms()
	filter := strings.ToLower(strings.TrimSpace(query.Filter))
	filterLike := "%" + filter + "%"
	args := []any{sourceID, startMs, endMs, filter, filterLike, filterLike}
	where := `
		m.source_id = ? AND m.time_created_ms >= ? AND m.time_created_ms < ?
		AND (? = '' OR LOWER(COALESCE(ss.title, '')) LIKE ? OR LOWER(COALESCE(ss.project_name, '')) LIKE ?)
	`
	if query.ProjectID != "" {
		where += ` AND ss.project_id = ?`
		args = append(args, query.ProjectID)
	}

	countQuery := `SELECT COUNT(*) FROM (SELECT ss.session_id FROM sessions ss JOIN message_index m ON m.source_id = ss.source_id AND m.session_id = ss.session_id WHERE ` + where + ` GROUP BY ss.session_id)`
	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return 0, nil, nil, err
	}

	order := "MIN(ss.time_created_ms) DESC"
	switch query.Sort {
	case stats.SessionSortOldest:
		order = "MIN(ss.time_created_ms) ASC"
	case stats.SessionSortCost:
		order = "SUM(m.cost) DESC, MIN(ss.time_created_ms) DESC"
	case stats.SessionSortMessages:
		order = "COUNT(m.message_id) DESC, MIN(ss.time_created_ms) DESC"
	}
	selectRows := func(extra string, extraArgs []any, rowLimit int) ([]stats.SessionEntry, error) {
		listQuery := `
			SELECT
				ss.session_id, ss.title, COALESCE(ss.project_id, ''), COALESCE(ss.project_name, ''),
				MIN(ss.time_created_ms), MAX(ss.time_updated_ms), COUNT(m.message_id), COALESCE(SUM(m.cost), 0)
			FROM sessions ss
			JOIN message_index m ON m.source_id = ss.source_id AND m.session_id = ss.session_id
			WHERE ` + where + extra + `
			GROUP BY ss.session_id
			ORDER BY ` + order
		listArgs := append(append([]any{}, args...), extraArgs...)
		if rowLimit > 0 {
			listQuery += ` LIMIT ?`
			listArgs = append(listArgs, rowLimit)
		}
		rows, err := s.db.QueryContext(ctx, listQuery, listArgs...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		entries := make([]stats.SessionEntry, 0)
		for rows.Next() {
			var entry stats.SessionEntry
			var createdMs, updatedMs int64
			entry.SourceID = sourceID
			if err := rows.Scan(&entry.ID, &entry.Title, &entry.ProjectID, &entry.ProjectName, &createdMs, &updatedMs, &entry.MessageCount, &entry.Cost); err != nil {
				return nil, err
			}
			entry.TimeCreated = time.UnixMilli(createdMs).UTC()
			entry.TimeUpdated = time.UnixMilli(updatedMs).UTC()
			entry.CostStatus, entry.CostProvenance = s.costSummaryForSession(ctx, sourceID, entry.ID, startMs, endMs)
			entries = append(entries, entry)
		}
		return entries, rows.Err()
	}

	ranked, err := selectRows("", nil, limit)
	if err != nil {
		return 0, nil, nil, err
	}
	byID := make(map[string]stats.SessionEntry)
	for _, chunk := range chunkStrings(ids, sessionIDChunk) {
		extra := ` AND ss.session_id IN (` + inPlaceholders(len(chunk)) + `)`
		extraArgs := make([]any, 0, len(chunk))
		for _, id := range chunk {
			extraArgs = append(extraArgs, id)
		}
		entries, err := selectRows(extra, extraArgs, 0)
		if err != nil {
			return 0, nil, nil, err
		}
		for _, entry := range entries {
			byID[entry.ID] = entry
		}
	}
	return total, ranked, byID, nil
}

// projectSessionRows mirrors recentProjectSessions (whole-session rollups
// from the sessions table, newest first) returning the top `limit` rows plus
// the rows for ids regardless of rank, and the project's total session count.
func (s *Store) projectSessionRows(ctx context.Context, sourceID, projectID string, limit int, ids []string) (int64, []stats.SessionEntry, map[string]stats.SessionEntry, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE source_id = ? AND project_id = ?`, sourceID, projectID).Scan(&total); err != nil {
		return 0, nil, nil, err
	}
	selectRows := func(extra string, extraArgs []any, rowLimit int) ([]stats.SessionEntry, error) {
		query := `
			SELECT session_id, title, COALESCE(project_id, ''), COALESCE(project_name, ''), time_created_ms, time_updated_ms, message_count, cost, COALESCE(cost_status, ''), cost_provenance_json
			FROM sessions
			WHERE source_id = ? AND project_id = ?` + extra + `
			ORDER BY time_created_ms DESC`
		queryArgs := append([]any{sourceID, projectID}, extraArgs...)
		if rowLimit > 0 {
			query += ` LIMIT ?`
			queryArgs = append(queryArgs, rowLimit)
		}
		rows, err := s.db.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		entries := make([]stats.SessionEntry, 0)
		for rows.Next() {
			var entry stats.SessionEntry
			var createdMs, updatedMs int64
			var prov sql.NullString
			entry.SourceID = sourceID
			if err := rows.Scan(&entry.ID, &entry.Title, &entry.ProjectID, &entry.ProjectName, &createdMs, &updatedMs, &entry.MessageCount, &entry.Cost, &entry.CostStatus, &prov); err != nil {
				return nil, err
			}
			entry.TimeCreated = time.UnixMilli(createdMs).UTC()
			entry.TimeUpdated = time.UnixMilli(updatedMs).UTC()
			if prov.Valid && prov.String != "" {
				var cp stats.CostProvenance
				if err := json.Unmarshal([]byte(prov.String), &cp); err == nil {
					entry.CostProvenance = &cp
				}
			}
			entries = append(entries, entry)
		}
		return entries, rows.Err()
	}
	ranked, err := selectRows("", nil, limit)
	if err != nil {
		return 0, nil, nil, err
	}
	byID := make(map[string]stats.SessionEntry)
	for _, chunk := range chunkStrings(ids, sessionIDChunk) {
		extra := ` AND session_id IN (` + inPlaceholders(len(chunk)) + `)`
		extraArgs := make([]any, 0, len(chunk))
		for _, id := range chunk {
			extraArgs = append(extraArgs, id)
		}
		entries, err := selectRows(extra, extraArgs, 0)
		if err != nil {
			return 0, nil, nil, err
		}
		for _, entry := range entries {
			byID[entry.ID] = entry
		}
	}
	return total, ranked, byID, nil
}
