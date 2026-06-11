package cache

const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS source_state (
	source_id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	kind TEXT NOT NULL,
	path TEXT,
	path_source TEXT,
	available INTEGER NOT NULL DEFAULT 0,
	diagnostics_json TEXT,
	cost_policy_json TEXT,
	privacy_json TEXT,
	source_info_json TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	reason TEXT,
	last_synced_ms INTEGER NOT NULL,
	last_safe_cutoff_ms INTEGER NOT NULL DEFAULT 0,
	fresh_through_ms INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS source_files (
	source_id TEXT NOT NULL,
	path TEXT NOT NULL,
	size INTEGER NOT NULL,
	mod_time_ms INTEGER NOT NULL,
	PRIMARY KEY (source_id, path)
);

CREATE TABLE IF NOT EXISTS projects (
	source_id TEXT NOT NULL,
	project_id TEXT NOT NULL,
	project_name TEXT NOT NULL,
	worktree TEXT,
	PRIMARY KEY (source_id, project_id)
);

CREATE TABLE IF NOT EXISTS sessions (
	source_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	title TEXT NOT NULL,
	project_id TEXT,
	project_name TEXT,
	time_created_ms INTEGER NOT NULL,
	time_updated_ms INTEGER NOT NULL,
	message_count INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	cost_status TEXT,
	cost_provenance_json TEXT,
	PRIMARY KEY (source_id, session_id)
);

CREATE TABLE IF NOT EXISTS message_index (
	source_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	session_title TEXT NOT NULL,
	role TEXT NOT NULL,
	time_created_ms INTEGER NOT NULL,
	cost REAL NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	model_id TEXT,
	provider_id TEXT,
	agent TEXT,
	is_subagent INTEGER NOT NULL DEFAULT 0,
	folded_assistant_calls INTEGER NOT NULL DEFAULT 0,
	folded_tool_calls INTEGER NOT NULL DEFAULT 0,
	folded_token_updates INTEGER NOT NULL DEFAULT 0,
	cost_status TEXT,
	cost_provenance_json TEXT,
	project_id TEXT,
	project_name TEXT,
	PRIMARY KEY (source_id, message_id)
);

CREATE TABLE IF NOT EXISTS tool_index (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	project_id TEXT,
	project_name TEXT,
	time_created_ms INTEGER NOT NULL,
	tool_name TEXT NOT NULL,
	status TEXT
);

CREATE TABLE IF NOT EXISTS hourly_usage (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	project_id TEXT NOT NULL,
	project_name TEXT NOT NULL,
	model_id TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	role TEXT NOT NULL,
	sessions INTEGER NOT NULL DEFAULT 0,
	messages INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms, project_id, model_id, provider_id, role)
);

CREATE TABLE IF NOT EXISTS hourly_tool_usage (
	source_id TEXT NOT NULL,
	bucket_start_ms INTEGER NOT NULL,
	tool_name TEXT NOT NULL,
	invocations INTEGER NOT NULL DEFAULT 0,
	successes INTEGER NOT NULL DEFAULT 0,
	failures INTEGER NOT NULL DEFAULT 0,
	sessions INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, bucket_start_ms, tool_name)
);

CREATE INDEX IF NOT EXISTS idx_message_index_source_time ON message_index(source_id, time_created_ms);
CREATE INDEX IF NOT EXISTS idx_message_index_source_session ON message_index(source_id, session_id);
CREATE INDEX IF NOT EXISTS idx_message_index_source_project ON message_index(source_id, project_id);
CREATE INDEX IF NOT EXISTS idx_message_index_source_model ON message_index(source_id, model_id, provider_id);
CREATE INDEX IF NOT EXISTS idx_sessions_source_project ON sessions(source_id, project_id);
CREATE INDEX IF NOT EXISTS idx_tool_index_source_time ON tool_index(source_id, time_created_ms);
CREATE INDEX IF NOT EXISTS idx_tool_index_source_name ON tool_index(source_id, tool_name);
CREATE INDEX IF NOT EXISTS idx_hourly_usage_source_bucket ON hourly_usage(source_id, bucket_start_ms);
CREATE INDEX IF NOT EXISTS idx_hourly_tool_usage_source_bucket ON hourly_tool_usage(source_id, bucket_start_ms);
`
