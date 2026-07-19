package cache

import (
	"context"

	"opencode-dashboard/internal/stats"
)

type costCounts struct {
	total    int64
	reported int64
	computed int64
	missing  int64
	statuses map[stats.CostStatus]int64
}

func (c *costCounts) add(status stats.CostStatus, count int64) {
	if status == "" || count <= 0 {
		return
	}
	if c.statuses == nil {
		c.statuses = make(map[stats.CostStatus]int64)
	}
	c.statuses[status] += count
	c.total += count
	switch status {
	case stats.CostReported:
		c.reported += count
	case stats.CostMissing:
		c.missing += count
	default:
		c.computed += count
	}
}

func (c costCounts) result() (stats.CostStatus, *stats.CostProvenance) {
	if c.total == 0 {
		return "", nil
	}
	status := stats.CostMixed
	if len(c.statuses) == 1 {
		for only := range c.statuses {
			status = only
		}
	}
	return status, &stats.CostProvenance{
		Status:        status,
		Currency:      "USD",
		MissingCount:  c.missing,
		ComputedCount: c.computed,
		ReportedCount: c.reported,
	}
}

func (s *Store) costSummary(ctx context.Context, sourceID string, startMs, endMs int64) (stats.CostStatus, *stats.CostProvenance) {
	return s.costSummaryWhere(ctx, sourceID, startMs, endMs, "", nil)
}

// costSummaryByKey aggregates assistant cost-status counts grouped by keyExpr
// in a single pass, replacing per-row costSummaryWhere round-trips.
func (s *Store) costSummaryByKey(ctx context.Context, sourceID, keyExpr string, startMs, endMs int64, extra string, extraArgs []any) (map[string]*costCounts, error) {
	query := `
		SELECT ` + keyExpr + `, COALESCE(cost_status, ''), COUNT(*)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND time_created_ms >= ? AND time_created_ms < ? ` + extra + `
		GROUP BY 1, 2
	`
	args := append([]any{sourceID, startMs, endMs}, extraArgs...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := make(map[string]*costCounts)
	for rows.Next() {
		var key, statusText string
		var count int64
		if err := rows.Scan(&key, &statusText, &count); err != nil {
			return nil, err
		}
		counts := byKey[key]
		if counts == nil {
			counts = &costCounts{}
			byKey[key] = counts
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return byKey, nil
}

// costSummariesForSessions batches per-session cost summaries for ids,
// chunking the IN clause like the other id-list helpers.
func (s *Store) costSummariesForSessions(ctx context.Context, sourceID string, startMs, endMs int64, ids []string) (map[string]*costCounts, error) {
	result := make(map[string]*costCounts)
	for _, chunk := range chunkStrings(ids, sessionIDChunk) {
		extra := ` AND session_id IN (` + inPlaceholders(len(chunk)) + `)`
		extraArgs := make([]any, 0, len(chunk))
		for _, id := range chunk {
			extraArgs = append(extraArgs, id)
		}
		byKey, err := s.costSummaryByKey(ctx, sourceID, "session_id", startMs, endMs, extra, extraArgs)
		if err != nil {
			return nil, err
		}
		for id, counts := range byKey {
			result[id] = counts
		}
	}
	return result, nil
}

// attachSessionCostSummaries fills per-session cost metadata for entries in
// one batched pass over the window.
func (s *Store) attachSessionCostSummaries(ctx context.Context, sourceID string, startMs, endMs int64, entries []stats.SessionEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	summaries, err := s.costSummariesForSessions(ctx, sourceID, startMs, endMs, ids)
	if err != nil {
		return err
	}
	for i := range entries {
		if counts := summaries[entries[i].ID]; counts != nil {
			entries[i].CostStatus, entries[i].CostProvenance = counts.result()
		}
	}
	return nil
}

func (s *Store) costSummaryWhere(ctx context.Context, sourceID string, startMs, endMs int64, extra string, extraArgs []any) (stats.CostStatus, *stats.CostProvenance) {
	query := `
		SELECT COALESCE(cost_status, ''), COUNT(*)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND time_created_ms >= ? AND time_created_ms < ? ` + extra + `
		GROUP BY COALESCE(cost_status, '')
	`
	args := []any{sourceID, startMs, endMs}
	args = append(args, extraArgs...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", nil
	}
	defer rows.Close()
	var counts costCounts
	for rows.Next() {
		var statusText string
		var count int64
		if err := rows.Scan(&statusText, &count); err != nil {
			return "", nil
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if rows.Err() != nil {
		return "", nil
	}
	return counts.result()
}
