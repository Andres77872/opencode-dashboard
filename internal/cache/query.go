package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"opencode-dashboard/internal/stats"
)

type window struct {
	start time.Time
	end   time.Time
	all   bool
}

func (s *Store) Overview(ctx context.Context, sourceID string, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	startMs, endMs := w.ms()
	var result stats.OverviewStats
	if f := filterFromPQ(pq); f.active() {
		result, err = s.overviewFromModelRollups(ctx, sourceID, startMs, endMs, f)
	} else {
		result, err = s.overviewFromRollups(ctx, sourceID, startMs, endMs)
	}
	if err != nil {
		return result, err
	}
	if result.Days > 0 {
		result.CostPerDay = result.Cost / float64(result.Days)
	}
	policy := s.cachedCostPolicy(ctx, sourceID)
	result.CostProvenance = applyCachedCostPolicy(sourceID, result.CostStatus, result.CostProvenance, policy)
	result.RequestAccounting = requestAccountingForSource(sourceID, result.RequestAccounting)
	return result, nil
}

func (s *Store) Daily(ctx context.Context, sourceID string, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	gran := stats.ResolveGranularity(pq, granularity...)

	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	var result stats.DailyStats
	if gran == stats.GranularityHour {
		result, err = s.dailyHourly(ctx, sourceID, w, filterFromPQ(pq))
	} else {
		result, err = s.dailyDay(ctx, sourceID, w, filterFromPQ(pq))
	}
	if err == nil {
		policy := s.cachedCostPolicy(ctx, sourceID)
		result.CostProvenance = applyCachedCostPolicy(sourceID, result.CostStatus, result.CostProvenance, policy)
		for i := range result.Days {
			result.Days[i].CostProvenance = applyCachedCostPolicy(sourceID, result.Days[i].CostStatus, result.Days[i].CostProvenance, policy)
		}
		result.RequestAccounting = requestAccountingForSource(sourceID, result.RequestAccounting)
	}
	return result, err
}

func (s *Store) dailyDay(ctx context.Context, sourceID string, w window, f modelFilter) (stats.DailyStats, error) {
	startMs, endMs := w.ms()
	parts := splitOverviewMillis(startMs, endMs)
	byDay, err := dailyBucketStats[string](ctx, s, sourceID, parts, dayBucketExprs, f)
	if err != nil {
		return stats.DailyStats{}, err
	}

	days := make([]stats.DayStats, 0)
	for t := utcDay(w.start); t.Before(w.end); t = t.AddDate(0, 0, 1) {
		key := t.Format("2006-01-02")
		if d, ok := byDay[key]; ok {
			d.Date = key
			days = append(days, *d)
		} else {
			days = append(days, stats.DayStats{SourceID: sourceID, Date: key})
		}
	}
	status, provenance, err := s.dailyListCostSummary(ctx, sourceID, parts, f)
	if err != nil {
		return stats.DailyStats{}, err
	}
	return stats.DailyStats{SourceID: sourceID, Days: days, Granularity: stats.GranularityDay, CostStatus: status, CostProvenance: provenance, RequestAccounting: dailyRequestAccounting(sourceID, days)}, nil
}

func (s *Store) dailyListCostSummary(ctx context.Context, sourceID string, parts overviewWindowParts, f modelFilter) (stats.CostStatus, *stats.CostProvenance, error) {
	if f.active() {
		return s.modelCostSummary(ctx, sourceID, parts, f)
	}
	return s.overviewCostSummary(ctx, sourceID, parts)
}

func (s *Store) dailyHourly(ctx context.Context, sourceID string, w window, f modelFilter) (stats.DailyStats, error) {
	start := w.start.UTC().Truncate(time.Hour)
	end := w.end.UTC()
	if !end.After(start) {
		end = start.Add(time.Hour)
	}
	startMs, endMs := start.UnixMilli(), end.UnixMilli()
	parts := splitOverviewMillis(startMs, endMs)
	byHour, err := dailyBucketStats[int64](ctx, s, sourceID, parts, hourBucketExprs, f)
	if err != nil {
		return stats.DailyStats{}, err
	}

	days := make([]stats.DayStats, 0)
	for t := start; t.Before(end); t = t.Add(time.Hour) {
		bucket := t.UnixMilli()
		if d, ok := byHour[bucket]; ok {
			d.Date = t.Format("2006-01-02T15:04:05Z")
			days = append(days, *d)
		} else {
			days = append(days, stats.DayStats{SourceID: sourceID, Date: t.Format("2006-01-02T15:04:05Z")})
		}
	}
	status, provenance, err := s.dailyListCostSummary(ctx, sourceID, parts, f)
	if err != nil {
		return stats.DailyStats{}, err
	}
	return stats.DailyStats{SourceID: sourceID, Days: days, Granularity: stats.GranularityHour, CostStatus: status, CostProvenance: provenance, RequestAccounting: dailyRequestAccounting(sourceID, days)}, nil
}

func dailyRequestAccounting(sourceID string, days []stats.DayStats) *stats.RequestAccounting {
	parts := make([]*stats.RequestAccounting, 0, len(days))
	for i := range days {
		parts = append(parts, days[i].RequestAccounting)
	}
	return requestAccountingForSource(sourceID, stats.MergeRequestAccounting(parts...))
}

func requestAccountingForSource(sourceID string, accounting *stats.RequestAccounting) *stats.RequestAccounting {
	if accounting == nil && sourceID == "kimi_code" {
		return &stats.RequestAccounting{TraceCoverage: stats.TraceCoverageUnknown}
	}
	return accounting
}

func (s *Store) DailyDimension(ctx context.Context, sourceID, dimension string, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyDimensionStats, error) {
	gran := stats.ResolveGranularity(pq, granularity...)
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	startMs, endMs := w.ms()
	label := periodLabel(pq)
	var result stats.DailyDimensionStats
	switch dimension {
	case "model":
		result, err = s.dailyModelDimensionFromRollups(ctx, sourceID, label, gran, startMs, endMs)
	case "project":
		result, err = s.dailyMessageDimension(ctx, sourceID, dimension, "COALESCE(project_id, '')", label, gran, startMs, endMs)
	case "processing_mode":
		if sourceID != "codex" {
			return stats.DailyDimensionStats{}, fmt.Errorf("invalid dimension %q for source %q: supported only for codex", dimension, sourceID)
		}
		result, err = s.dailyMessageDimension(ctx, sourceID, dimension, "COALESCE(NULLIF(processing_mode, ''), 'unknown')", label, gran, startMs, endMs)
	case "tool":
		result, err = s.dailyToolDimension(ctx, sourceID, dimension, label, gran, startMs, endMs)
	default:
		return stats.DailyDimensionStats{}, fmt.Errorf("invalid dimension %q: supported values are model, tool, project, processing_mode", dimension)
	}
	if err != nil {
		return result, err
	}
	policy := s.cachedCostPolicy(ctx, sourceID)
	result.CostProvenance = applyCachedCostPolicy(sourceID, result.CostStatus, result.CostProvenance, policy)
	for i := range result.Days {
		result.Days[i].CostProvenance = applyCachedCostPolicy(sourceID, result.Days[i].CostStatus, result.Days[i].CostProvenance, policy)
	}
	return result, nil
}

func (s *Store) dailyMessageDimension(ctx context.Context, sourceID, dimension, expr, period string, gran stats.Granularity, startMs, endMs int64) (stats.DailyDimensionStats, error) {
	query := fmt.Sprintf(`
		SELECT
			%s AS day,
			%s AS dim,
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND time_created_ms >= ? AND time_created_ms < ? AND %s != ''
		GROUP BY day, dim
		ORDER BY day ASC, COUNT(*) DESC
	`, stats.TrendBucketSQL("time_created_ms", gran), expr, expr)
	rows, err := s.db.QueryContext(ctx, query, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	days, err := scanDimensionRows(rows, sourceID)
	if err != nil {
		rows.Close()
		return stats.DailyDimensionStats{}, err
	}
	if err := rows.Close(); err != nil {
		return stats.DailyDimensionStats{}, err
	}
	if err := s.attachDimensionCostSummaries(ctx, sourceID, expr, gran, startMs, endMs, days); err != nil {
		return stats.DailyDimensionStats{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	return stats.DailyDimensionStats{SourceID: sourceID, Days: days, Dimension: dimension, Period: period, Granularity: gran, CostStatus: status, CostProvenance: provenance}, nil
}

// attachDimensionCostSummaries restores the cost metadata that SQL's numeric
// aggregation cannot carry on its own. Keeping this metadata per (day,
// dimension) is essential for requested processing modes: a zero cost with a
// missing catalog entry is unknown, not a real $0.00 estimate.
func (s *Store) attachDimensionCostSummaries(ctx context.Context, sourceID, expr string, gran stats.Granularity, startMs, endMs int64, days []stats.DimensionDayStats) error {
	query := fmt.Sprintf(`
		SELECT
			%s AS day,
			%s AS dim,
			COALESCE(cost_status, ''),
			COUNT(*)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND time_created_ms >= ? AND time_created_ms < ? AND %s != ''
		GROUP BY day, dim, COALESCE(cost_status, '')
	`, stats.TrendBucketSQL("time_created_ms", gran), expr, expr)
	rows, err := s.db.QueryContext(ctx, query, sourceID, startMs, endMs)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rowKey struct{ date, dim string }
	countsByRow := make(map[rowKey]*costCounts)
	for rows.Next() {
		var date, dim, statusText string
		var count int64
		if err := rows.Scan(&date, &dim, &statusText, &count); err != nil {
			return err
		}
		key := rowKey{date: date, dim: dim}
		counts := countsByRow[key]
		if counts == nil {
			counts = &costCounts{}
			countsByRow[key] = counts
		}
		counts.add(stats.CostStatus(statusText), count)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range days {
		if counts := countsByRow[rowKey{date: days[i].Date, dim: days[i].Dimension}]; counts != nil {
			days[i].CostStatus, days[i].CostProvenance = counts.result()
		}
	}
	return nil
}

func (s *Store) dailyToolDimension(ctx context.Context, sourceID, dimension, period string, gran stats.Granularity, startMs, endMs int64) (stats.DailyDimensionStats, error) {
	query := fmt.Sprintf(`
		SELECT
			%s AS day,
			tool_name,
			COUNT(DISTINCT session_id),
			COUNT(*),
			0.0,
			0, 0, 0, 0, 0
		FROM tool_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY day, tool_name
		ORDER BY day ASC, COUNT(*) DESC
	`, stats.TrendBucketSQL("time_created_ms", gran))
	rows, err := s.db.QueryContext(ctx, query, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	defer rows.Close()
	days, err := scanDimensionRows(rows, sourceID)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	return stats.DailyDimensionStats{SourceID: sourceID, Days: days, Dimension: dimension, Period: period, Granularity: gran, CostStatus: status, CostProvenance: provenance}, nil
}

func scanDimensionRows(rows *sql.Rows, sourceID string) ([]stats.DimensionDayStats, error) {
	days := make([]stats.DimensionDayStats, 0)
	for rows.Next() {
		var d stats.DimensionDayStats
		var cacheRead, cacheWrite int64
		d.SourceID = sourceID
		if err := rows.Scan(&d.Date, &d.Dimension, &d.Sessions, &d.Messages, &d.Cost, &d.Tokens.Input, &d.Tokens.Output, &d.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
			return nil, err
		}
		d.Tokens.Cache.Read = cacheRead
		d.Tokens.Cache.Write = cacheWrite
		days = append(days, d)
	}
	return days, rows.Err()
}

func (s *Store) Models(ctx context.Context, sourceID string, pq stats.PeriodQuery) (stats.ModelStats, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.ModelStats{}, err
	}
	startMs, endMs := w.ms()
	result, err := s.modelsFromRollups(ctx, sourceID, startMs, endMs)
	if err != nil {
		return result, err
	}
	policy := s.cachedCostPolicy(ctx, sourceID)
	result.CostProvenance = applyCachedCostPolicy(sourceID, result.CostStatus, result.CostProvenance, policy)
	for i := range result.Models {
		result.Models[i].CostProvenance = applyCachedCostPolicy(sourceID, result.Models[i].CostStatus, result.Models[i].CostProvenance, policy)
	}
	return result, nil
}

func (s *Store) Tools(ctx context.Context, sourceID string, pq stats.PeriodQuery) (stats.ToolStats, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.ToolStats{}, err
	}
	startMs, endMs := w.ms()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			tool_name,
			COUNT(*),
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),
			COUNT(DISTINCT session_id)
		FROM tool_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY tool_name
		ORDER BY COUNT(*) DESC, tool_name ASC
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.ToolStats{}, err
	}
	defer rows.Close()
	tools := make([]stats.ToolEntry, 0)
	for rows.Next() {
		var entry stats.ToolEntry
		entry.SourceID = sourceID
		if err := rows.Scan(&entry.Name, &entry.Invocations, &entry.Successes, &entry.Failures, &entry.Sessions); err != nil {
			return stats.ToolStats{}, err
		}
		tools = append(tools, entry)
	}
	return stats.ToolStats{SourceID: sourceID, Tools: tools}, rows.Err()
}

func (s *Store) Projects(ctx context.Context, sourceID string, pq stats.PeriodQuery) (stats.ProjectStats, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	startMs, endMs := w.ms()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(m.project_id, ''),
			COALESCE(MAX(p.project_name), ''),
			COUNT(DISTINCT m.session_id),
			COUNT(*),
			COALESCE(SUM(m.cost), 0),
			COALESCE(SUM(m.input_tokens), 0),
			COALESCE(SUM(m.output_tokens), 0),
			COALESCE(SUM(m.reasoning_tokens), 0),
			COALESCE(SUM(m.cache_read_tokens), 0),
			COALESCE(SUM(m.cache_write_tokens), 0)
		FROM message_index m
		LEFT JOIN projects p ON p.source_id = m.source_id AND p.project_id = m.project_id
		WHERE m.source_id = ? AND m.time_created_ms >= ? AND m.time_created_ms < ?
		GROUP BY m.project_id
		ORDER BY COALESCE(SUM(m.cost), 0) DESC
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	defer rows.Close()
	projects := make([]stats.ProjectEntry, 0)
	for rows.Next() {
		var entry stats.ProjectEntry
		var cacheRead, cacheWrite int64
		entry.SourceID = sourceID
		if err := rows.Scan(&entry.ProjectID, &entry.ProjectName, &entry.Sessions, &entry.Messages, &entry.Cost, &entry.Tokens.Input, &entry.Tokens.Output, &entry.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
			return stats.ProjectStats{}, err
		}
		if entry.ProjectName == "" {
			entry.ProjectName = entry.ProjectID
		}
		entry.Tokens.Cache.Read = cacheRead
		entry.Tokens.Cache.Write = cacheWrite
		projects = append(projects, entry)
	}
	if err := rows.Err(); err != nil {
		return stats.ProjectStats{}, err
	}
	summaries, err := s.costSummaryByKey(ctx, sourceID, "COALESCE(project_id, '')", startMs, endMs, "", nil)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	for i := range projects {
		if counts := summaries[projects[i].ProjectID]; counts != nil {
			projects[i].CostStatus, projects[i].CostProvenance = counts.result()
		}
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	policy := s.cachedCostPolicy(ctx, sourceID)
	provenance = applyCachedCostPolicy(sourceID, status, provenance, policy)
	for i := range projects {
		projects[i].CostProvenance = applyCachedCostPolicy(sourceID, projects[i].CostStatus, projects[i].CostProvenance, policy)
	}
	return stats.ProjectStats{SourceID: sourceID, Projects: projects, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *Store) ProjectByID(ctx context.Context, sourceID, id string, pq stats.PeriodQuery, page, limit int) (*stats.ProjectDetail, error) {
	if id == "" {
		return nil, nil
	}
	var name, worktree sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT project_name, worktree FROM projects WHERE source_id = ? AND project_id = ?`, sourceID, id).Scan(&name, &worktree)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return nil, err
	}
	startMs, endMs := w.ms()
	var detail stats.ProjectDetail
	detail.SourceID = sourceID
	detail.ProjectID = id
	detail.ProjectName = name.String
	detail.Worktree = worktree.String
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND project_id = ? AND time_created_ms >= ? AND time_created_ms < ?
	`, sourceID, id, startMs, endMs).Scan(&detail.Sessions, &detail.Messages, &detail.Cost, &detail.Tokens.Input, &detail.Tokens.Output, &detail.Tokens.Reasoning, &detail.Tokens.Cache.Read, &detail.Tokens.Cache.Write)
	if err != nil {
		return nil, err
	}
	summaries, err := s.costSummaryByKey(ctx, sourceID, "COALESCE(project_id, '')", startMs, endMs, "AND COALESCE(project_id, '') = ?", []any{id})
	if err != nil {
		return nil, err
	}
	if counts := summaries[id]; counts != nil {
		detail.CostStatus, detail.CostProvenance = counts.result()
	}
	detail.CostProvenance = applyCachedCostPolicy(sourceID, detail.CostStatus, detail.CostProvenance, s.cachedCostPolicy(ctx, sourceID))
	detail.TotalSessions, detail.RecentSessions, err = s.recentProjectSessions(ctx, sourceID, id, page, limit)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func (s *Store) Sessions(ctx context.Context, sourceID string, query stats.SessionQuery) (stats.SessionList, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	if query.Sort == "" {
		query.Sort = stats.SessionSortNewest
	}
	pq := stats.PeriodQuery{Period: query.Period, From: query.From, To: query.To, FromTime: query.FromTime, ToTime: query.ToTime}
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.SessionList{}, err
	}
	startMs, endMs := w.ms()
	spec := newSessionListSpec(sourceID, startMs, endMs, query)

	countQuery, countArgs := spec.countQuery()
	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return stats.SessionList{}, err
	}

	listQuery, listArgs := spec.listQuery("", nil, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := s.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return stats.SessionList{}, err
	}
	defer rows.Close()
	entries := make([]stats.SessionEntry, 0)
	for rows.Next() {
		var entry stats.SessionEntry
		var createdMs, updatedMs int64
		entry.SourceID = sourceID
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.ProjectID, &entry.ProjectName, &createdMs, &updatedMs, &entry.MessageCount, &entry.Cost); err != nil {
			return stats.SessionList{}, err
		}
		entry.TimeCreated = time.UnixMilli(createdMs).UTC()
		entry.TimeUpdated = time.UnixMilli(updatedMs).UTC()
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return stats.SessionList{}, err
	}
	if err := s.attachSessionCostSummaries(ctx, sourceID, startMs, endMs, entries); err != nil {
		return stats.SessionList{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	policy := s.cachedCostPolicy(ctx, sourceID)
	provenance = applyCachedCostPolicy(sourceID, status, provenance, policy)
	for i := range entries {
		entries[i].CostProvenance = applyCachedCostPolicy(sourceID, entries[i].CostStatus, entries[i].CostProvenance, policy)
	}
	return stats.SessionList{SourceID: sourceID, Sessions: entries, Total: total, Page: query.Page, PageSize: query.PageSize, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *Store) Messages(ctx context.Context, sourceID string, pq stats.PeriodQuery, page, limit int, sortSpec stats.MessageSort) (stats.MessageList, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.MessageList{}, err
	}
	startMs, endMs := w.ms()
	msgWhere, msgArgs := filterFromPQ(pq).messageWhere()
	var total int64
	countArgs := append([]any{sourceID, startMs, endMs}, msgArgs...)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere, countArgs...).Scan(&total); err != nil {
		return stats.MessageList{}, err
	}
	listArgs := append(append([]any{sourceID, startMs, endMs}, msgArgs...), limit, (page-1)*limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			message_id, session_id, role, time_created_ms, cost,
			input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens,
			COALESCE(model_id, ''), COALESCE(provider_id, ''),
			COALESCE(service_tier, ''), COALESCE(processing_mode, ''), COALESCE(agent, ''), is_subagent,
			folded_assistant_calls, folded_tool_calls, folded_token_updates, COALESCE(cost_status, ''), cost_provenance_json,
			COALESCE(request_trace, ''), COALESCE(usage_status, '')
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`+msgWhere+`
		ORDER BY `+messageOrderBy(sortSpec)+`
		LIMIT ? OFFSET ?
	`, listArgs...)
	if err != nil {
		return stats.MessageList{}, err
	}
	defer rows.Close()
	messages := make([]stats.MessageEntry, 0)
	for rows.Next() {
		entry, err := scanMessageEntry(rows, sourceID)
		if err != nil {
			return stats.MessageList{}, err
		}
		messages = append(messages, entry)
	}
	if err := rows.Err(); err != nil {
		return stats.MessageList{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	provenance = applyCachedCostPolicy(sourceID, status, provenance, s.cachedCostPolicy(ctx, sourceID))
	return stats.MessageList{SourceID: sourceID, Messages: messages, Total: total, Page: page, PageSize: limit, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *Store) SessionByID(ctx context.Context, sourceID, id string) (*stats.SessionDetail, error) {
	if id == "" {
		return nil, nil
	}
	var detail stats.SessionDetail
	var createdMs, updatedMs int64
	var costStatus sql.NullString
	var prov sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT session_id, title, COALESCE(project_id, ''), COALESCE(project_name, ''), time_created_ms, time_updated_ms, message_count, cost, COALESCE(cost_status, ''), cost_provenance_json
		FROM sessions
		WHERE source_id = ? AND session_id = ?
	`, sourceID, id).Scan(&detail.ID, &detail.Title, &detail.ProjectID, &detail.ProjectName, &createdMs, &updatedMs, &detail.MessageCount, &detail.TotalCost, &costStatus, &prov)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	detail.SourceID = sourceID
	detail.TimeCreated = time.UnixMilli(createdMs).UTC()
	detail.TimeUpdated = time.UnixMilli(updatedMs).UTC()
	if costStatus.Valid {
		detail.CostStatus = stats.CostStatus(costStatus.String)
	}
	if prov.Valid && prov.String != "" {
		var cp stats.CostProvenance
		if err := json.Unmarshal([]byte(prov.String), &cp); err == nil {
			detail.CostProvenance = &cp
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, role, time_created_ms, cost, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens, COALESCE(model_id, ''), COALESCE(provider_id, ''), COALESCE(service_tier, ''), COALESCE(processing_mode, ''), COALESCE(agent, ''), is_subagent, COALESCE(cost_status, ''), cost_provenance_json, COALESCE(request_trace, ''), COALESCE(usage_status, '')
		FROM message_index
		WHERE source_id = ? AND session_id = ?
		ORDER BY time_created_ms ASC, message_id ASC
	`, sourceID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	detail.Messages = make([]stats.SessionMessage, 0)
	for rows.Next() {
		var msg stats.SessionMessage
		var msgMs int64
		var input, output, reasoning, cacheRead, cacheWrite int64
		var isSubagent int
		var msgProv sql.NullString
		msg.SourceID = sourceID
		if err := rows.Scan(&msg.ID, &msg.Role, &msgMs, &msg.Cost, &input, &output, &reasoning, &cacheRead, &cacheWrite, &msg.ModelID, &msg.ProviderID, &msg.ServiceTier, &msg.ProcessingMode, &msg.Agent, &isSubagent, &msg.CostStatus, &msgProv, &msg.RequestTrace, &msg.UsageStatus); err != nil {
			return nil, err
		}
		msg.TimeCreated = time.UnixMilli(msgMs).UTC()
		msg.IsSubagent = isSubagent == 1
		if msg.UsageStatus != stats.UsageStatusUnavailable && (msg.Role == "assistant" || input+output+reasoning+cacheRead+cacheWrite > 0) {
			msg.Tokens = &stats.TokenStats{Input: input, Output: output, Reasoning: reasoning, Cache: stats.CacheStats{Read: cacheRead, Write: cacheWrite}}
		}
		if msgProv.Valid && msgProv.String != "" {
			var cp stats.CostProvenance
			if err := json.Unmarshal([]byte(msgProv.String), &cp); err == nil {
				msg.CostProvenance = &cp
			}
		}
		detail.TotalTokens.Input += input
		detail.TotalTokens.Output += output
		detail.TotalTokens.Reasoning += reasoning
		detail.TotalTokens.Cache.Read += cacheRead
		detail.TotalTokens.Cache.Write += cacheWrite
		detail.Messages = append(detail.Messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	detail.MessageCount = int64(len(detail.Messages))
	detail.CostProvenance = applyCachedCostPolicy(sourceID, detail.CostStatus, detail.CostProvenance, s.cachedCostPolicy(ctx, sourceID))
	return &detail, nil
}

func scanMessageEntry(rows interface {
	Scan(dest ...any) error
}, sourceID string) (stats.MessageEntry, error) {
	var entry stats.MessageEntry
	var createdMs int64
	var input, output, reasoning, cacheRead, cacheWrite int64
	var isSubagent int
	var prov sql.NullString
	entry.SourceID = sourceID
	if err := rows.Scan(
		&entry.ID, &entry.SessionID, &entry.Role, &createdMs, &entry.Cost,
		&input, &output, &reasoning, &cacheRead, &cacheWrite,
		&entry.ModelID, &entry.ProviderID, &entry.ServiceTier, &entry.ProcessingMode, &entry.Agent, &isSubagent,
		&entry.FoldedAssistantCalls, &entry.FoldedToolCalls, &entry.FoldedTokenUpdates, &entry.CostStatus, &prov,
		&entry.RequestTrace, &entry.UsageStatus,
	); err != nil {
		return entry, err
	}
	// The cache never stores titles; the same synthesized value the writer
	// used to store is derived at scan time instead.
	entry.SessionTitle = safeSessionTitle(sourceID, entry.SessionID)
	entry.TimeCreated = time.UnixMilli(createdMs).UTC()
	entry.IsSubagent = isSubagent == 1
	if entry.UsageStatus != stats.UsageStatusUnavailable && (entry.Role == "assistant" || input+output+reasoning+cacheRead+cacheWrite > 0) {
		entry.Tokens = &stats.TokenStats{
			Input:     input,
			Output:    output,
			Reasoning: reasoning,
			Cache:     stats.CacheStats{Read: cacheRead, Write: cacheWrite},
		}
	}
	if prov.Valid && prov.String != "" {
		var cp stats.CostProvenance
		if err := json.Unmarshal([]byte(prov.String), &cp); err == nil {
			entry.CostProvenance = &cp
		}
	}
	return entry, nil
}

func (s *Store) MessageByID(ctx context.Context, sourceID, id string) (*stats.MessageEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			message_id, session_id, role, time_created_ms, cost,
			input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens,
			COALESCE(model_id, ''), COALESCE(provider_id, ''),
			COALESCE(service_tier, ''), COALESCE(processing_mode, ''), COALESCE(agent, ''), is_subagent,
			folded_assistant_calls, folded_tool_calls, folded_token_updates, COALESCE(cost_status, ''), cost_provenance_json,
			COALESCE(request_trace, ''), COALESCE(usage_status, '')
		FROM message_index
		WHERE source_id = ? AND message_id = ?
	`, sourceID, id)
	entry, err := scanMessageEntry(row, sourceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &entry, nil
}

func (s *Store) recentProjectSessions(ctx context.Context, sourceID, projectID string, page, limit int) (int64, []stats.SessionEntry, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE source_id = ? AND project_id = ?`, sourceID, projectID).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, title, COALESCE(project_id, ''), COALESCE(project_name, ''), time_created_ms, time_updated_ms, message_count, cost, COALESCE(cost_status, ''), cost_provenance_json
		FROM sessions
		WHERE source_id = ? AND project_id = ?
		ORDER BY time_created_ms DESC
		LIMIT ? OFFSET ?
	`, sourceID, projectID, limit, (page-1)*limit)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	entries := make([]stats.SessionEntry, 0)
	for rows.Next() {
		var entry stats.SessionEntry
		var createdMs, updatedMs int64
		var prov sql.NullString
		entry.SourceID = sourceID
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.ProjectID, &entry.ProjectName, &createdMs, &updatedMs, &entry.MessageCount, &entry.Cost, &entry.CostStatus, &prov); err != nil {
			return 0, nil, err
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
	return total, entries, rows.Err()
}

// periodWindow resolves pq and clamps the end at the finality cutoff: cache
// reads never serve rows at/after it. Legacy caches may still hold mirrored
// un-finalized rows until their first new-style consolidation prunes them;
// the recent gap is read live by the merge layer instead.
func (s *Store) periodWindow(ctx context.Context, sourceID string, pq stats.PeriodQuery) (window, error) {
	w, err := s.resolveWindow(ctx, sourceID, pq)
	if err != nil {
		return window{}, err
	}
	cutoff, err := s.LastSafeCutoff(ctx, sourceID)
	if err != nil {
		return window{}, err
	}
	if !cutoff.IsZero() && cutoff.Before(w.end) {
		w.end = cutoff
	}
	return w, nil
}

func (s *Store) resolveWindow(ctx context.Context, sourceID string, pq stats.PeriodQuery) (window, error) {
	if from, to, ok := pq.TimeBounds(); ok {
		return window{start: from, end: to}, nil
	}
	w, err := s.presetOrExplicitWindow(ctx, sourceID, pq)
	if err != nil {
		return window{}, err
	}
	if !pq.ToTime.IsZero() {
		if capped := pq.ToTime.UTC(); capped.Before(w.end) {
			w.end = capped
		}
	}
	return w, nil
}

func (s *Store) presetOrExplicitWindow(ctx context.Context, sourceID string, pq stats.PeriodQuery) (window, error) {
	if pq.From != "" {
		from, err := time.ParseInLocation("2006-01-02", pq.From, time.UTC)
		if err != nil {
			return window{}, fmt.Errorf("invalid from date %q: expected YYYY-MM-DD format", pq.From)
		}
		to := time.Now().UTC()
		if pq.To != "" {
			parsed, err := time.ParseInLocation("2006-01-02", pq.To, time.UTC)
			if err != nil {
				return window{}, fmt.Errorf("invalid to date %q: expected YYYY-MM-DD format", pq.To)
			}
			to = parsed.AddDate(0, 0, 1)
		}
		return window{start: from, end: to}, nil
	}
	period := pq.Period
	if period == "" {
		period = "7d"
	}
	if period == "all" {
		start, end, err := s.observedWindow(ctx, sourceID)
		if err != nil {
			return window{}, err
		}
		return window{start: start, end: end, all: true}, nil
	}
	if hours, ok := parseHourPeriod(period); ok {
		now := time.Now().UTC()
		return window{start: now.Add(-time.Duration(hours) * time.Hour), end: now}, nil
	}
	days := map[string]int{"1d": 1, "7d": 7, "14d": 14, "30d": 30, "1y": 365}
	n, ok := days[period]
	if !ok {
		return window{}, fmt.Errorf("invalid period: %q (supported: 1d, 7d, 14d, 30d, 1y, all, plus hour presets 1h, 6h, 12h, 24h, 72h)", period)
	}
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	return window{start: end.AddDate(0, 0, -n), end: end}, nil
}

func (s *Store) observedWindow(ctx context.Context, sourceID string) (time.Time, time.Time, error) {
	var minMs, maxMs sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(time_created_ms), MAX(time_created_ms) FROM message_index WHERE source_id = ?`, sourceID).Scan(&minMs, &maxMs); err != nil {
		return time.Time{}, time.Time{}, err
	}
	now := time.Now().UTC()
	if !minMs.Valid || !maxMs.Valid {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), nil
	}
	start := utcDay(time.UnixMilli(minMs.Int64))
	end := utcDay(time.UnixMilli(maxMs.Int64)).AddDate(0, 0, 1)
	return start, end, nil
}

func (w window) ms() (int64, int64) {
	return w.start.UTC().UnixMilli(), w.end.UTC().UnixMilli()
}

func periodLabel(pq stats.PeriodQuery) string {
	if pq.Period != "" {
		return pq.Period
	}
	if pq.From != "" {
		return "from_" + pq.From
	}
	return ""
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func dayStartMs(day string) int64 {
	t, err := time.ParseInLocation("2006-01-02", day, time.UTC)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func parseHourPeriod(period string) (int, bool) {
	switch period {
	case "1h":
		return 1, true
	case "6h":
		return 6, true
	case "12h":
		return 12, true
	case "24h":
		return 24, true
	case "72h":
		return 72, true
	default:
		return 0, false
	}
}

func messageOrderBy(sortSpec stats.MessageSort) string {
	dir := "DESC"
	if sortSpec.Direction == stats.MessageSortAsc {
		dir = "ASC"
	}
	switch sortSpec.Field {
	case stats.MessageSortCost:
		return "cost " + dir + ", message_id ASC"
	case stats.MessageSortTokens:
		return "(input_tokens + output_tokens + reasoning_tokens + cache_read_tokens + cache_write_tokens) " + dir + ", message_id ASC"
	case stats.MessageSortModel:
		return "model_id " + dir + ", message_id ASC"
	case stats.MessageSortRole:
		return "role " + dir + ", message_id ASC"
	default:
		return "time_created_ms " + dir + ", message_id ASC"
	}
}

func setModelAverages(entry *stats.ModelEntry) {
	if entry.Messages > 0 {
		entry.AvgTokensPerMessage = &stats.AvgTokenStats{
			Input:      float64(entry.Tokens.Input) / float64(entry.Messages),
			Output:     float64(entry.Tokens.Output) / float64(entry.Messages),
			Reasoning:  float64(entry.Tokens.Reasoning) / float64(entry.Messages),
			CacheRead:  float64(entry.Tokens.Cache.Read) / float64(entry.Messages),
			CacheWrite: float64(entry.Tokens.Cache.Write) / float64(entry.Messages),
		}
	}
	if entry.Sessions > 0 {
		entry.AvgTokensPerSession = &stats.AvgTokenStats{
			Input:      float64(entry.Tokens.Input) / float64(entry.Sessions),
			Output:     float64(entry.Tokens.Output) / float64(entry.Sessions),
			Reasoning:  float64(entry.Tokens.Reasoning) / float64(entry.Sessions),
			CacheRead:  float64(entry.Tokens.Cache.Read) / float64(entry.Sessions),
			CacheWrite: float64(entry.Tokens.Cache.Write) / float64(entry.Sessions),
		}
	}
}
