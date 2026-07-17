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

func (s *Store) costSummaryForModel(ctx context.Context, sourceID, modelID, providerID string, startMs, endMs int64) (stats.CostStatus, *stats.CostProvenance) {
	return s.costSummaryWhere(ctx, sourceID, startMs, endMs, "AND COALESCE(model_id, '') = ? AND COALESCE(provider_id, '') = ?", []any{modelID, providerID})
}

func (s *Store) costSummaryForProject(ctx context.Context, sourceID, projectID string, startMs, endMs int64) (stats.CostStatus, *stats.CostProvenance) {
	return s.costSummaryWhere(ctx, sourceID, startMs, endMs, "AND COALESCE(project_id, '') = ?", []any{projectID})
}

func (s *Store) costSummaryForSession(ctx context.Context, sourceID, sessionID string, startMs, endMs int64) (stats.CostStatus, *stats.CostProvenance) {
	return s.costSummaryWhere(ctx, sourceID, startMs, endMs, "AND session_id = ?", []any{sessionID})
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
