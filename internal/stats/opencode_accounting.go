package stats

// ClaudeAgentProvider is OpenCode's Claude Code-backed provider. Its requests
// are already recorded by the Claude Code source, which owns their accounting.
const ClaudeAgentProvider = "claude-agent"

// v1Accounted and v2Accounted select the OpenCode-owned transcript rows. User
// prompts do not always carry a provider: attribute them to the next assistant
// response, falling back to the preceding response for an unanswered prompt.
// Compactions without a model inherit the preceding request's provider. Never
// use a session's current model: a conversation can switch providers midstream.
// Missing providers stay included, rather than guessing that they are Claude.
func v1Accounted(m string) string {
	provider := `json_extract(` + m + `.data, '$.providerID')`
	return `(CASE WHEN json_extract(` + m + `.data, '$.role') = 'user' THEN COALESCE(
		json_extract(` + m + `.data, '$.model.providerID'),
		(SELECT COALESCE(json_extract(accounting_next.data, '$.providerID'), '')
		 FROM message accounting_next
		 WHERE accounting_next.session_id = ` + m + `.session_id
		   AND accounting_next.time_created >= ` + m + `.time_created
		   AND json_extract(accounting_next.data, '$.role') = 'assistant'
		 ORDER BY accounting_next.time_created, accounting_next.id LIMIT 1),
		(SELECT COALESCE(json_extract(accounting_prev.data, '$.providerID'), '')
		 FROM message accounting_prev
		 WHERE accounting_prev.session_id = ` + m + `.session_id
		   AND accounting_prev.time_created < ` + m + `.time_created
		   AND json_extract(accounting_prev.data, '$.role') = 'assistant'
		 ORDER BY accounting_prev.time_created DESC, accounting_prev.id DESC LIMIT 1)
	) ELSE ` + provider + ` END) IS NOT '` + ClaudeAgentProvider + `'`
}

func v2Accounted(m string) string {
	provider := `json_extract(` + m + `.data, '$.model.providerID')`
	previous := `(SELECT COALESCE(json_extract(accounting_prev.data, '$.model.providerID'), '')
		 FROM session_message accounting_prev
		 WHERE accounting_prev.session_id = ` + m + `.session_id
		   AND accounting_prev.type IN ('assistant', 'model-switched') AND accounting_prev.seq < ` + m + `.seq
		 ORDER BY accounting_prev.seq DESC LIMIT 1)`
	return `(CASE
		WHEN ` + m + `.type = 'user' THEN COALESCE(` + provider + `,
			(SELECT COALESCE(json_extract(accounting_next.data, '$.model.providerID'), '')
			 FROM session_message accounting_next
			 WHERE accounting_next.session_id = ` + m + `.session_id
			   AND accounting_next.type = 'assistant' AND accounting_next.seq > ` + m + `.seq
			 ORDER BY accounting_next.seq LIMIT 1), ` + previous + `)
		WHEN ` + m + `.type = 'compaction' THEN COALESCE(` + provider + `, ` + previous + `)
		ELSE ` + provider + ` END) IS NOT '` + ClaudeAgentProvider + `'`
}

// V1MessagesSQL and V1PartsSQL are read-only relations, shared by interactive
// statistics and cache consolidation so excluded requests cannot leak through
// another query path. Unattached legacy parts are retained as unknown ownership.
var V1MessagesSQL = `(SELECT m.* FROM message m WHERE ` + v1Accounted("m") + `)`

var V1PartsSQL = `(SELECT p.* FROM part p WHERE NOT EXISTS (
	SELECT 1 FROM message accounting_parent WHERE accounting_parent.id = p.message_id
	AND NOT (` + v1Accounted("accounting_parent") + `)
))`

// Keep empty sessions, but hide sessions whose entire accounted history belongs
// to Claude Code. These relations also keep unbounded session/project counts in
// agreement with the filtered messages, including pure Claude Agent sessions.
var V1SessionsSQL = `(SELECT s.* FROM session s WHERE
	NOT EXISTS (SELECT 1 FROM message session_row WHERE session_row.session_id = s.id)
	OR EXISTS (SELECT 1 FROM message session_row WHERE session_row.session_id = s.id
		AND ` + v1Accounted("session_row") + `))`

var V2SessionsSQL = `(SELECT s.* FROM session_v2 s WHERE
	NOT EXISTS (SELECT 1 FROM session_message session_row WHERE session_row.session_id = s.id
		AND ` + v2TranscriptMessage("session_row") + ` AND ` + v2Owned("session_row", "s") + `)
	OR EXISTS (SELECT 1 FROM session_message session_row WHERE session_row.session_id = s.id
		AND ` + v2Message("session_row") + ` AND ` + v2Owned("session_row", "s") + `))`
