package cache

import (
	"strings"

	"opencode-dashboard/internal/stats"
)

// sessionListSpec builds the cached session-list queries shared by
// Store.Sessions and Store.sessionWindowRows so the two paths cannot drift.
// The list aggregates message_index per session first and joins the
// single-row sessions metadata afterwards, keeping the join off the
// per-message rows.
type sessionListSpec struct {
	sourceID  string
	startMs   int64
	endMs     int64
	filter    string
	projectID string
	sort      stats.SessionSortMode
}

func newSessionListSpec(sourceID string, startMs, endMs int64, query stats.SessionQuery) sessionListSpec {
	return sessionListSpec{
		sourceID:  sourceID,
		startMs:   startMs,
		endMs:     endMs,
		filter:    strings.ToLower(strings.TrimSpace(query.Filter)),
		projectID: query.ProjectID,
		sort:      query.Sort,
	}
}

// core returns the shared FROM/WHERE clause. Cached titles are synthesized
// ("Session <id>"), so the text filter matches session ids and project names;
// real title text is only searchable in the live gap.
func (spec sessionListSpec) core(extra string, extraArgs []any) (string, []any) {
	filterLike := "%" + spec.filter + "%"
	args := []any{spec.sourceID, spec.startMs, spec.endMs, spec.sourceID, spec.filter, filterLike, filterLike}
	clause := `
		FROM (
			SELECT session_id, COUNT(*) AS msg_count, COALESCE(SUM(cost), 0) AS win_cost
			FROM message_index
			WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
			GROUP BY session_id
		) agg
		JOIN sessions ss ON ss.source_id = ? AND ss.session_id = agg.session_id
		WHERE (? = '' OR LOWER(ss.session_id) LIKE ? OR LOWER(COALESCE(ss.project_name, '')) LIKE ?)
	`
	if spec.projectID != "" {
		clause += ` AND ss.project_id = ?`
		args = append(args, spec.projectID)
	}
	clause += extra
	args = append(args, extraArgs...)
	return clause, args
}

func (spec sessionListSpec) countQuery() (string, []any) {
	clause, args := spec.core("", nil)
	return `SELECT COUNT(*)` + clause, args
}

func (spec sessionListSpec) orderBy() string {
	switch spec.sort {
	case stats.SessionSortOldest:
		return "ss.time_created_ms ASC"
	case stats.SessionSortCost:
		return "agg.win_cost DESC, ss.time_created_ms DESC"
	case stats.SessionSortMessages:
		return "agg.msg_count DESC, ss.time_created_ms DESC"
	default:
		return "ss.time_created_ms DESC"
	}
}

func (spec sessionListSpec) listQuery(extra string, extraArgs []any, limit, offset int) (string, []any) {
	clause, args := spec.core(extra, extraArgs)
	query := `
		SELECT
			ss.session_id, ss.title, COALESCE(ss.project_id, ''), COALESCE(ss.project_name, ''),
			ss.time_created_ms, ss.time_updated_ms, agg.msg_count, agg.win_cost` + clause + `
		ORDER BY ` + spec.orderBy()
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}
	return query, args
}
