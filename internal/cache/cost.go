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
	counts := costCounts{statuses: make(map[stats.CostStatus]int64)}
	for rows.Next() {
		var statusText string
		var count int64
		if err := rows.Scan(&statusText, &count); err != nil {
			return "", nil
		}
		if statusText == "" {
			continue
		}
		status := stats.CostStatus(statusText)
		counts.statuses[status] += count
		counts.total += count
		switch status {
		case stats.CostReported:
			counts.reported += count
		case stats.CostMissing:
			counts.missing += count
		default:
			counts.computed += count
		}
	}
	if rows.Err() != nil || counts.total == 0 {
		return "", nil
	}

	status := stats.CostMixed
	if len(counts.statuses) == 1 {
		for only := range counts.statuses {
			status = only
		}
	}
	return status, &stats.CostProvenance{
		Status:        status,
		Currency:      "USD",
		MissingCount:  counts.missing,
		ComputedCount: counts.computed,
		ReportedCount: counts.reported,
	}
}
