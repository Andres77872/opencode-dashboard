package stats

// OpenCode 2.x storage.
//
// OpenCode 2 replaced the 1.x session/message/part tables with session_v2 and
// session_message. Every session_message row is one projected transcript item
// whose JSON payload lives in data, discriminated by the type column:
//
//   - user: a prompt ({text, files, agents, ...}).
//   - assistant: ONE model step (one outbound request). data.cost and
//     data.tokens hold that step's own usage, data.model is {id, providerID,
//     variant}, and data.content embeds the step's text, reasoning, and tool
//     calls ({type:"tool", id, name, state:{status, input, content, error}}).
//   - compaction: a context compaction request. Once it is no longer running
//     it carries the usage of its model calls (cost, tokens, optional model).
//   - synthetic, system, skill, shell, idle, agent-switched, model-switched,
//     location-switched: transcript bookkeeping with no model usage. Shell
//     rows are commands the user ran, not model tool calls.
//
// The dashboard maps this onto its 1.x message model: user rows are user
// messages, assistant steps and settled compactions are assistant messages
// (requests), and everything else is ignored. Unlike 1.x, step usage is
// already additive per row, so no step-finish reconstruction is needed.
//
// A forked session copies its parent's settled rows with their original
// time_created, so copies are recognised by predating the fork session itself
// and are excluded exactly as OpenCode's own usage statistics do
// (packages/core/src/session/stats.ts). Without this every fork would count
// its parent's history twice.

import "opencode-dashboard/internal/store"

func isV2(s *store.Store) bool {
	return s.Version() == store.SchemaV2
}

// v2Message returns the predicate selecting session_message rows (alias m)
// that the dashboard counts as messages.
func v2Message(m string) string {
	return `(` + m + `.type IN ('user', 'assistant') OR (` + m + `.type = 'compaction' AND json_extract(` + m + `.data, '$.status') IS NOT 'running'))`
}

// v2Request returns the predicate selecting session_message rows (alias m)
// that are outbound model requests.
func v2Request(m string) string {
	return `(` + m + `.type = 'assistant' OR (` + m + `.type = 'compaction' AND json_extract(` + m + `.data, '$.status') IS NOT 'running'))`
}

// v2Owned excludes rows a fork (session alias s) copied from its parent.
func v2Owned(m, s string) string {
	return `(` + s + `.fork_session_id IS NULL OR ` + m + `.time_created >= ` + s + `.time_created)`
}

// V2MessagesSQL is the dashboard message relation of an OpenCode 2 database,
// for use as a FROM-clause subquery. SQLite flattens it into the outer query,
// so range predicates on time_created still use session_message's index.
//
// Columns: id, session_id, time_created, seq, type, data, role ("user" or
// "assistant"), agent, and usage_state (NULL for user rows, "recorded" when
// the request persisted token usage, otherwise the UsageUnavailableReason
// explaining why it did not).
var V2MessagesSQL = `(
	SELECT
		m.id AS id,
		m.session_id AS session_id,
		m.time_created AS time_created,
		m.seq AS seq,
		m.type AS type,
		m.data AS data,
		CASE m.type WHEN 'user' THEN 'user' ELSE 'assistant' END AS role,
		CASE m.type WHEN 'compaction' THEN 'compaction' ELSE json_extract(m.data, '$.agent') END AS agent,
		` + v2UsageStateSQL("m") + ` AS usage_state
	FROM session_message m
	LEFT JOIN session_v2 s ON s.id = m.session_id
	WHERE ` + v2Message("m") + `
		AND ` + v2Owned("m", "s") + `
)`

func v2UsageStateSQL(m string) string {
	return `CASE
			WHEN ` + m + `.type = 'user' THEN NULL
			WHEN json_type(` + m + `.data, '$.tokens') = 'object' THEN 'recorded'
			WHEN json_extract(` + m + `.data, '$.error.type') = 'aborted' THEN '` + string(UsageUnavailableCancelled) + `'
			WHEN json_type(` + m + `.data, '$.error') IS NOT NULL THEN '` + string(UsageUnavailableFailed) + `'
			ELSE '` + string(UsageUnavailableUnknown) + `'
		END`
}

// ApplyV2UsageState records an OpenCode 2 request's usage evidence on entry.
// Rows without persisted usage keep nil tokens so they stay distinct from a
// measured zero, mirroring how the cache partitions requests.
func ApplyV2UsageState(entry *MessageEntry, state string) {
	if entry.Role != "assistant" {
		return
	}
	if state == "" || state == "recorded" {
		entry.UsageStatus = UsageStatusRecorded
		return
	}
	entry.Tokens = nil
	entry.UsageStatus = UsageStatusUnavailable
	entry.UsageUnavailableReason = UsageUnavailableReason(state)
}

func applyV2SessionUsageState(msg *SessionMessage, state string) {
	entry := MessageEntry{Role: msg.Role, Tokens: msg.Tokens}
	ApplyV2UsageState(&entry, state)
	msg.Tokens = entry.Tokens
	msg.UsageStatus = entry.UsageStatus
	msg.UsageUnavailableReason = entry.UsageUnavailableReason
}

const (
	v2ModelIDSQL    = `json_extract(m.data, '$.model.id')`
	v2ProviderIDSQL = `json_extract(m.data, '$.model.providerID')`
)

var overviewV2Query = `
	SELECT
		COUNT(DISTINCT m.session_id),
		COUNT(*),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN COALESCE(json_extract(m.data, '$.cost'), 0) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN COALESCE(json_extract(m.data, '$.tokens.input'), 0) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN COALESCE(json_extract(m.data, '$.tokens.output'), 0) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN COALESCE(json_extract(m.data, '$.tokens.reasoning'), 0) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN COALESCE(json_extract(m.data, '$.tokens.cache.read'), 0) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN COALESCE(json_extract(m.data, '$.tokens.cache.write'), 0) ELSE 0 END), 0),
		COUNT(DISTINCT DATE(m.time_created / 1000, 'unixepoch'))
	FROM ` + V2MessagesSQL + ` m
	WHERE m.time_created >= ? AND m.time_created < ?
`

// session_message.time_created is indexed and always an integer, so MIN uses
// the index instead of scanning (and parsing) every large row.
const earliestActivityV2Query = `
	SELECT MIN(created_at)
	FROM (
		SELECT MIN(time_created) AS created_at FROM session_v2
		UNION ALL
		SELECT MIN(time_created) AS created_at FROM session_message
	)
	WHERE created_at IS NOT NULL
`

func sessionCountsByBucketV2Query(bucket string) string {
	return `
		SELECT ` + bucket + ` AS bucket, COUNT(*) AS count
		FROM session_v2
		WHERE time_created >= ? AND time_created < ?
		GROUP BY bucket
	`
}

func messageStatsByBucketV2Query(bucket string) string {
	return `
		SELECT
			` + bucket + ` AS bucket,
			COUNT(*) AS message_count,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN 1 ELSE 0 END), 0) AS request_count,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN CAST(json_extract(m.data, '$.cost') AS REAL) ELSE 0 END), 0) AS total_cost,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN CAST(json_extract(m.data, '$.tokens.input') AS INTEGER) ELSE 0 END), 0) AS input_tokens,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN CAST(json_extract(m.data, '$.tokens.output') AS INTEGER) ELSE 0 END), 0) AS output_tokens,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN CAST(json_extract(m.data, '$.tokens.reasoning') AS INTEGER) ELSE 0 END), 0) AS reasoning_tokens,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN CAST(json_extract(m.data, '$.tokens.cache.read') AS INTEGER) ELSE 0 END), 0) AS cache_read_tokens,
			COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN CAST(json_extract(m.data, '$.tokens.cache.write') AS INTEGER) ELSE 0 END), 0) AS cache_write_tokens
		FROM ` + V2MessagesSQL + ` m
		WHERE m.time_created >= ? AND m.time_created < ?
		GROUP BY bucket
	`
}

// dailyDimensionV2Query groups requests by dim. Model uses the 2.x model
// reference; the other dimensions keep the 1.x JSON paths, which OpenCode
// message payloads do not carry in either version.
func dailyDimensionV2Query(dimension, path, bucket string) string {
	dim := `json_extract(m.data, '` + path + `')`
	if dimension == "model" {
		dim = v2ModelIDSQL
	}
	return `
		SELECT
			` + bucket + ` AS day,
			` + dim + ` AS dim,
			COUNT(DISTINCT m.session_id) AS sessions,
			COUNT(*) AS messages,
			COALESCE(SUM(CAST(json_extract(m.data, '$.cost') AS REAL)), 0) AS total_cost,
			COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.input') AS INTEGER)), 0) AS input_tokens,
			COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.output') AS INTEGER)), 0) AS output_tokens,
			COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.reasoning') AS INTEGER)), 0) AS reasoning_tokens,
			COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.cache.read') AS INTEGER)), 0) AS cache_read_tokens,
			COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.cache.write') AS INTEGER)), 0) AS cache_write_tokens
		FROM ` + V2MessagesSQL + ` m
		WHERE m.role = 'assistant'
			AND ` + dim + ` IS NOT NULL
			AND ` + dim + ` != ''
			AND m.time_created >= ? AND m.time_created < ?
		GROUP BY day, dim
		ORDER BY day ASC, total_cost DESC
	`
}

var modelsV2Query = `
	SELECT
		` + v2ModelIDSQL + ` AS model_id,
		` + v2ProviderIDSQL + ` AS provider_id,
		COUNT(DISTINCT m.session_id) AS sessions,
		COUNT(*) AS messages,
		SUM(COALESCE(json_extract(m.data, '$.cost'), 0)) AS total_cost,
		SUM(COALESCE(json_extract(m.data, '$.tokens.input'), 0)) AS input_tokens,
		SUM(COALESCE(json_extract(m.data, '$.tokens.output'), 0)) AS output_tokens,
		SUM(COALESCE(json_extract(m.data, '$.tokens.reasoning'), 0)) AS reasoning_tokens,
		SUM(COALESCE(json_extract(m.data, '$.tokens.cache.read'), 0)) AS cache_read,
		SUM(COALESCE(json_extract(m.data, '$.tokens.cache.write'), 0)) AS cache_write
	FROM ` + V2MessagesSQL + ` m
	WHERE m.role = 'assistant'
		AND ` + v2ModelIDSQL + ` IS NOT NULL
		AND ` + v2ModelIDSQL + ` != ''
		AND m.time_created >= ? AND m.time_created < ?
	GROUP BY model_id, provider_id
`

// V2ToolCallsSQL lists the tool calls embedded in assistant steps, for use as
// a FROM-clause subquery. Columns: message_id, session_id, time_created,
// tool, status. Status is OpenCode's tool state: streaming, running,
// completed, or error.
var V2ToolCallsSQL = `(
	SELECT
		m.id AS message_id,
		m.session_id AS session_id,
		m.time_created AS time_created,
		json_extract(c.value, '$.name') AS tool,
		json_extract(c.value, '$.state.status') AS status
	FROM ` + V2MessagesSQL + ` m, json_each(m.data, '$.content') c
	WHERE m.type = 'assistant'
		AND json_extract(c.value, '$.type') = 'tool'
)`

var toolsV2Query = `
	SELECT
		t.tool AS tool_name,
		COUNT(*) AS invocations,
		SUM(CASE WHEN t.status = 'completed' THEN 1 ELSE 0 END) AS successes,
		SUM(CASE WHEN t.status = 'error' THEN 1 ELSE 0 END) AS failures,
		COUNT(DISTINCT t.session_id) AS sessions
	FROM ` + V2ToolCallsSQL + ` t
	WHERE t.time_created >= ? AND t.time_created < ?
		AND t.tool IS NOT NULL
		AND t.tool != ''
	GROUP BY tool_name
	ORDER BY invocations DESC, tool_name ASC
`

// projectsV2Query aggregates requests per session first: the message relation
// is a join, so SQLite could not flatten it as the right side of the project
// LEFT JOINs the 1.x query uses.
var projectsV2Query = `
	WITH session_usage AS MATERIALIZED (
		SELECT
			m.session_id,
			COUNT(*) AS messages,
			SUM(COALESCE(json_extract(m.data, '$.cost'), 0)) AS cost,
			SUM(COALESCE(json_extract(m.data, '$.tokens.input'), 0)) AS input_tokens,
			SUM(COALESCE(json_extract(m.data, '$.tokens.output'), 0)) AS output_tokens,
			SUM(COALESCE(json_extract(m.data, '$.tokens.reasoning'), 0)) AS reasoning_tokens,
			SUM(COALESCE(json_extract(m.data, '$.tokens.cache.read'), 0)) AS cache_read,
			SUM(COALESCE(json_extract(m.data, '$.tokens.cache.write'), 0)) AS cache_write
		FROM ` + V2MessagesSQL + ` m
		WHERE m.role = 'assistant'
			AND m.time_created >= ? AND m.time_created < ?
		GROUP BY m.session_id
	)
	SELECT
		p.id,
		p.worktree,
		p.name,
		COUNT(u.session_id) AS session_count,
		COALESCE(SUM(u.messages), 0) AS message_count,
		COALESCE(SUM(u.cost), 0) AS total_cost,
		COALESCE(SUM(u.input_tokens), 0) AS total_input,
		COALESCE(SUM(u.output_tokens), 0) AS total_output,
		COALESCE(SUM(u.reasoning_tokens), 0) AS total_reasoning,
		COALESCE(SUM(u.cache_read), 0) AS total_cache_read,
		COALESCE(SUM(u.cache_write), 0) AS total_cache_write
	FROM project p
	LEFT JOIN session_v2 s ON s.project_id = p.id
	LEFT JOIN session_usage u ON u.session_id = s.id
	GROUP BY p.id
	ORDER BY total_cost DESC
`

var projectAggregateV2Query = `
	SELECT
		COUNT(DISTINCT s.id) AS sessions,
		COUNT(m.id) AS messages,
		COALESCE(SUM(CAST(json_extract(m.data, '$.cost') AS REAL)), 0) AS total_cost,
		COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.input') AS INTEGER)), 0) AS input_tokens,
		COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.output') AS INTEGER)), 0) AS output_tokens,
		COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.reasoning') AS INTEGER)), 0) AS reasoning_tokens,
		COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.cache.read') AS INTEGER)), 0) AS cache_read,
		COALESCE(SUM(CAST(json_extract(m.data, '$.tokens.cache.write') AS INTEGER)), 0) AS cache_write
	FROM session_v2 s
	LEFT JOIN session_message m ON m.session_id = s.id
		AND ` + v2Request("m") + `
		AND ` + v2Owned("m", "s") + `
		AND m.time_created >= ? AND m.time_created < ?
	WHERE s.project_id = ?
`

const projectSessionCountV2Query = `SELECT COUNT(*) FROM session_v2 WHERE project_id = ?`

var projectRecentSessionsV2Query = `
	SELECT
		s.id,
		s.title,
		s.project_id,
		p.name,
		p.worktree,
		s.time_created,
		s.time_updated,
		(SELECT COUNT(*) FROM session_message m2 WHERE m2.session_id = s.id AND ` + v2Message("m2") + ` AND ` + v2Owned("m2", "s") + `) AS message_count,
		COALESCE((SELECT SUM(CAST(json_extract(m3.data, '$.cost') AS REAL)) FROM session_message m3 WHERE m3.session_id = s.id AND ` + v2Request("m3") + ` AND ` + v2Owned("m3", "s") + `), 0) AS total_cost
	FROM session_v2 s
	LEFT JOIN project p ON p.id = s.project_id
	WHERE s.project_id = ?
	ORDER BY s.time_created DESC
	LIMIT ? OFFSET ?
`

const sessionsCountV2Query = `
	SELECT COUNT(*)
	FROM session_v2 s
	LEFT JOIN project p ON p.id = s.project_id
	WHERE (? = '' OR LOWER(COALESCE(s.title, '')) LIKE LOWER(?) OR LOWER(COALESCE(p.name, p.worktree, '')) LIKE LOWER(?))
`

var sessionsListV2Query = `
	SELECT
		s.id,
		s.title,
		s.project_id,
		p.name,
		p.worktree,
		s.time_created,
		s.time_updated,
		COUNT(m.id) AS message_count,
		COALESCE(SUM(COALESCE(json_extract(m.data, '$.cost'), 0)), 0) AS total_cost
	FROM session_v2 s
	LEFT JOIN project p ON p.id = s.project_id
	LEFT JOIN session_message m ON m.session_id = s.id
		AND ` + v2Request("m") + `
		AND ` + v2Owned("m", "s") + `
		AND m.time_created >= ? AND m.time_created < ?
	WHERE (? = '' OR LOWER(COALESCE(s.title, '')) LIKE LOWER(?) OR LOWER(COALESCE(p.name, p.worktree, '')) LIKE LOWER(?))
`

// sessionActiveV2Clause restricts sessions (alias s) to those with a counted
// message in [?, ?).
var sessionActiveV2Clause = ` AND EXISTS (
		SELECT 1 FROM session_message m2
		WHERE m2.session_id = s.id
			AND ` + v2Message("m2") + `
			AND ` + v2Owned("m2", "s") + `
			AND m2.time_created >= ? AND m2.time_created < ?
	)`

const sessionMetaV2Query = `
	SELECT
		s.id,
		s.title,
		s.project_id,
		p.name,
		p.worktree,
		s.directory,
		s.time_created,
		s.time_updated
	FROM session_v2 s
	LEFT JOIN project p ON p.id = s.project_id
	WHERE s.id = ?
`

var sessionMessagesV2Query = `
	SELECT
		m.id,
		m.role,
		m.time_created,
		COALESCE(json_extract(m.data, '$.cost'), 0) AS cost,
		COALESCE(json_extract(m.data, '$.tokens.input'), 0) AS input_tokens,
		COALESCE(json_extract(m.data, '$.tokens.output'), 0) AS output_tokens,
		COALESCE(json_extract(m.data, '$.tokens.reasoning'), 0) AS reasoning_tokens,
		COALESCE(json_extract(m.data, '$.tokens.cache.read'), 0) AS cache_read,
		COALESCE(json_extract(m.data, '$.tokens.cache.write'), 0) AS cache_write,
		` + v2ModelIDSQL + ` AS model_id,
		` + v2ProviderIDSQL + ` AS provider_id,
		m.agent,
		m.usage_state
	FROM ` + V2MessagesSQL + ` m
	WHERE m.session_id = ?
	ORDER BY m.seq ASC
`

var messagesCountV2Query = `
	SELECT COUNT(*)
	FROM ` + V2MessagesSQL + ` m
	WHERE m.time_created >= ? AND m.time_created < ?
`

// messagesListV2Select keeps the 1.x column aliases (role, cost, model_id)
// and the m.data/m.time_created names MessageSort.OrderByClause relies on.
var messagesListV2Select = `
	SELECT
		m.id,
		m.session_id,
		s.title,
		m.role AS role,
		m.time_created,
		COALESCE(json_extract(m.data, '$.cost'), 0) AS cost,
		COALESCE(json_extract(m.data, '$.tokens.input'), 0) AS input_tokens,
		COALESCE(json_extract(m.data, '$.tokens.output'), 0) AS output_tokens,
		COALESCE(json_extract(m.data, '$.tokens.reasoning'), 0) AS reasoning_tokens,
		COALESCE(json_extract(m.data, '$.tokens.cache.read'), 0) AS cache_read,
		COALESCE(json_extract(m.data, '$.tokens.cache.write'), 0) AS cache_write,
		` + v2ModelIDSQL + ` AS model_id,
		` + v2ProviderIDSQL + ` AS provider_id,
		m.usage_state
	FROM ` + V2MessagesSQL + ` m
	LEFT JOIN session_v2 s ON s.id = m.session_id
`

func messagesListV2Query(sort MessageSort) string {
	return messagesListV2Select + `
		WHERE m.time_created >= ? AND m.time_created < ?
		ORDER BY ` + sort.OrderByClause() + `
		LIMIT ? OFFSET ?
	`
}

var messageByIDV2Query = messagesListV2Select + `
	WHERE m.id = ?
`

const messageDataV2Query = `SELECT type, data FROM session_message WHERE id = ?`
