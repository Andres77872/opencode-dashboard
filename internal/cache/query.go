package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	result.SourceID = sourceID
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0),
			COUNT(DISTINCT DATE(time_created_ms / 1000, 'unixepoch'))
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
	`, sourceID, startMs, endMs).Scan(
		&result.Sessions,
		&result.Messages,
		&result.Cost,
		&result.Tokens.Input,
		&result.Tokens.Output,
		&result.Tokens.Reasoning,
		&result.Tokens.Cache.Read,
		&result.Tokens.Cache.Write,
		&result.Days,
	)
	if err != nil {
		return result, err
	}
	result.CostStatus, result.CostProvenance = s.costSummary(ctx, sourceID, startMs, endMs)
	if result.Days > 0 {
		result.CostPerDay = result.Cost / float64(result.Days)
	}
	return result, nil
}

func (s *Store) Daily(ctx context.Context, sourceID string, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	gran := stats.GranularityDay
	explicit := len(granularity) > 0 && granularity[0] != ""
	if explicit {
		gran = granularity[0]
	} else if pq.Period == "1d" || isHourPeriod(pq.Period) {
		gran = stats.GranularityHour
	}

	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	if gran == stats.GranularityHour {
		return s.dailyHourly(ctx, sourceID, w)
	}
	return s.dailyDay(ctx, sourceID, w)
}

func (s *Store) dailyDay(ctx context.Context, sourceID string, w window) (stats.DailyStats, error) {
	startMs, endMs := w.ms()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			DATE(time_created_ms / 1000, 'unixepoch') AS day,
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY day
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyStats{}, err
	}
	defer rows.Close()

	byDay := make(map[string]stats.DayStats)
	for rows.Next() {
		var d stats.DayStats
		d.SourceID = sourceID
		var cacheRead, cacheWrite int64
		if err := rows.Scan(&d.Date, &d.Sessions, &d.Messages, &d.Cost, &d.Tokens.Input, &d.Tokens.Output, &d.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
			return stats.DailyStats{}, err
		}
		d.Tokens.Cache.Read = cacheRead
		d.Tokens.Cache.Write = cacheWrite
		d.CostStatus, d.CostProvenance = s.costSummary(ctx, sourceID, dayStartMs(d.Date), dayStartMs(d.Date)+int64(24*time.Hour/time.Millisecond))
		byDay[d.Date] = d
	}
	if err := rows.Err(); err != nil {
		return stats.DailyStats{}, err
	}

	days := make([]stats.DayStats, 0)
	for t := utcDay(w.start); t.Before(w.end); t = t.AddDate(0, 0, 1) {
		key := t.Format("2006-01-02")
		if d, ok := byDay[key]; ok {
			days = append(days, d)
		} else {
			days = append(days, stats.DayStats{SourceID: sourceID, Date: key})
		}
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	return stats.DailyStats{SourceID: sourceID, Days: days, Granularity: stats.GranularityDay, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *Store) dailyHourly(ctx context.Context, sourceID string, w window) (stats.DailyStats, error) {
	start := w.start.UTC().Truncate(time.Hour)
	end := w.end.UTC()
	if !end.After(start) {
		end = start.Add(time.Hour)
	}
	startMs, endMs := start.UnixMilli(), end.UnixMilli()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			(time_created_ms / 3600000) * 3600000 AS bucket,
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY bucket
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyStats{}, err
	}
	defer rows.Close()

	byHour := make(map[int64]stats.DayStats)
	for rows.Next() {
		var bucket int64
		var d stats.DayStats
		d.SourceID = sourceID
		var cacheRead, cacheWrite int64
		if err := rows.Scan(&bucket, &d.Sessions, &d.Messages, &d.Cost, &d.Tokens.Input, &d.Tokens.Output, &d.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
			return stats.DailyStats{}, err
		}
		d.Date = time.UnixMilli(bucket).UTC().Format("2006-01-02T15:04:05Z")
		d.Tokens.Cache.Read = cacheRead
		d.Tokens.Cache.Write = cacheWrite
		d.CostStatus, d.CostProvenance = s.costSummary(ctx, sourceID, bucket, bucket+int64(time.Hour/time.Millisecond))
		byHour[bucket] = d
	}
	if err := rows.Err(); err != nil {
		return stats.DailyStats{}, err
	}

	days := make([]stats.DayStats, 0)
	for t := start; t.Before(end); t = t.Add(time.Hour) {
		bucket := t.UnixMilli()
		if d, ok := byHour[bucket]; ok {
			days = append(days, d)
		} else {
			days = append(days, stats.DayStats{SourceID: sourceID, Date: t.Format("2006-01-02T15:04:05Z")})
		}
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	return stats.DailyStats{SourceID: sourceID, Days: days, Granularity: stats.GranularityHour, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *Store) DailyDimension(ctx context.Context, sourceID, dimension string, pq stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	w, err := s.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	startMs, endMs := w.ms()
	label := periodLabel(pq)
	switch dimension {
	case "model":
		return s.dailyMessageDimension(ctx, sourceID, dimension, "COALESCE(model_id, '')", label, startMs, endMs)
	case "project":
		return s.dailyMessageDimension(ctx, sourceID, dimension, "COALESCE(project_id, '')", label, startMs, endMs)
	case "tool":
		return s.dailyToolDimension(ctx, sourceID, dimension, label, startMs, endMs)
	default:
		return stats.DailyDimensionStats{}, fmt.Errorf("invalid dimension %q: supported values are model, tool, project", dimension)
	}
}

func (s *Store) dailyMessageDimension(ctx context.Context, sourceID, dimension, expr, period string, startMs, endMs int64) (stats.DailyDimensionStats, error) {
	query := fmt.Sprintf(`
		SELECT
			DATE(time_created_ms / 1000, 'unixepoch') AS day,
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
	`, expr, expr)
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
	return stats.DailyDimensionStats{SourceID: sourceID, Days: days, Dimension: dimension, Period: period, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *Store) dailyToolDimension(ctx context.Context, sourceID, dimension, period string, startMs, endMs int64) (stats.DailyDimensionStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			DATE(time_created_ms / 1000, 'unixepoch') AS day,
			tool_name,
			COUNT(DISTINCT session_id),
			COUNT(*),
			0.0,
			0, 0, 0, 0, 0
		FROM tool_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY day, tool_name
		ORDER BY day ASC, COUNT(*) DESC
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	defer rows.Close()
	days, err := scanDimensionRows(rows, sourceID)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	return stats.DailyDimensionStats{SourceID: sourceID, Days: days, Dimension: dimension, Period: period, CostStatus: status, CostProvenance: provenance}, nil
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(model_id, ''),
			COALESCE(provider_id, ''),
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND COALESCE(model_id, '') != '' AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY model_id, provider_id
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.ModelStats{}, err
	}
	defer rows.Close()

	models := make([]stats.ModelEntry, 0)
	for rows.Next() {
		var entry stats.ModelEntry
		entry.SourceID = sourceID
		var cacheRead, cacheWrite int64
		if err := rows.Scan(&entry.ModelID, &entry.ProviderID, &entry.Sessions, &entry.Messages, &entry.Cost, &entry.Tokens.Input, &entry.Tokens.Output, &entry.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
			return stats.ModelStats{}, err
		}
		entry.Tokens.Cache.Read = cacheRead
		entry.Tokens.Cache.Write = cacheWrite
		entry.CostStatus, entry.CostProvenance = s.costSummaryForModel(ctx, sourceID, entry.ModelID, entry.ProviderID, startMs, endMs)
		setModelAverages(&entry)
		models = append(models, entry)
	}
	if err := rows.Err(); err != nil {
		return stats.ModelStats{}, err
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost != models[j].Cost {
			return models[i].Cost > models[j].Cost
		}
		if models[i].Messages != models[j].Messages {
			return models[i].Messages > models[j].Messages
		}
		return models[i].ModelID < models[j].ModelID
	})
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
	return stats.ModelStats{SourceID: sourceID, Models: models, CostStatus: status, CostProvenance: provenance}, nil
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
			COALESCE(MAX(m.project_name), ''),
			COUNT(DISTINCT m.session_id),
			COUNT(*),
			COALESCE(SUM(m.cost), 0),
			COALESCE(SUM(m.input_tokens), 0),
			COALESCE(SUM(m.output_tokens), 0),
			COALESCE(SUM(m.reasoning_tokens), 0),
			COALESCE(SUM(m.cache_read_tokens), 0),
			COALESCE(SUM(m.cache_write_tokens), 0)
		FROM message_index m
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
		entry.CostStatus, entry.CostProvenance = s.costSummaryForProject(ctx, sourceID, entry.ProjectID, startMs, endMs)
		projects = append(projects, entry)
	}
	if err := rows.Err(); err != nil {
		return stats.ProjectStats{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
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
	detail.CostStatus, detail.CostProvenance = s.costSummaryForProject(ctx, sourceID, id, startMs, endMs)
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
		return stats.SessionList{}, err
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
	listQuery := `
		SELECT
			ss.session_id, ss.title, COALESCE(ss.project_id, ''), COALESCE(ss.project_name, ''),
			MIN(ss.time_created_ms), MAX(ss.time_updated_ms), COUNT(m.message_id), COALESCE(SUM(m.cost), 0)
		FROM sessions ss
		JOIN message_index m ON m.source_id = ss.source_id AND m.session_id = ss.session_id
		WHERE ` + where + `
		GROUP BY ss.session_id
		ORDER BY ` + order + `
		LIMIT ? OFFSET ?
	`
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, query.PageSize, (query.Page-1)*query.PageSize)
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
		entry.CostStatus, entry.CostProvenance = s.costSummaryForSession(ctx, sourceID, entry.ID, startMs, endMs)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return stats.SessionList{}, err
	}
	status, provenance := s.costSummary(ctx, sourceID, startMs, endMs)
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
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_index WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?`, sourceID, startMs, endMs).Scan(&total); err != nil {
		return stats.MessageList{}, err
	}
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
	`, sourceID, startMs, endMs, limit, (page-1)*limit)
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
		SELECT message_id, role, time_created_ms, cost, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens, COALESCE(model_id, ''), COALESCE(provider_id, ''), COALESCE(agent, ''), is_subagent, COALESCE(cost_status, ''), cost_provenance_json
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
		if err := rows.Scan(&msg.ID, &msg.Role, &msgMs, &msg.Cost, &input, &output, &reasoning, &cacheRead, &cacheWrite, &msg.ModelID, &msg.ProviderID, &msg.Agent, &isSubagent, &msg.CostStatus, &msgProv); err != nil {
			return nil, err
		}
		msg.TimeCreated = time.UnixMilli(msgMs).UTC()
		msg.IsSubagent = isSubagent == 1
		if msg.Role == "assistant" || input+output+reasoning+cacheRead+cacheWrite > 0 {
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
		&entry.ID, &entry.SessionID, &entry.SessionTitle, &entry.Role, &createdMs, &entry.Cost,
		&input, &output, &reasoning, &cacheRead, &cacheWrite,
		&entry.ModelID, &entry.ProviderID, &entry.Agent, &isSubagent,
		&entry.FoldedAssistantCalls, &entry.FoldedToolCalls, &entry.FoldedTokenUpdates, &entry.CostStatus, &prov,
	); err != nil {
		return entry, err
	}
	entry.TimeCreated = time.UnixMilli(createdMs).UTC()
	entry.IsSubagent = isSubagent == 1
	if entry.Role == "assistant" || input+output+reasoning+cacheRead+cacheWrite > 0 {
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
			message_id, session_id, session_title, role, time_created_ms, cost,
			input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens,
			COALESCE(model_id, ''), COALESCE(provider_id, ''), COALESCE(agent, ''), is_subagent,
			folded_assistant_calls, folded_tool_calls, folded_token_updates, COALESCE(cost_status, ''), cost_provenance_json
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

func isHourPeriod(period string) bool {
	_, ok := parseHourPeriod(period)
	return ok
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
