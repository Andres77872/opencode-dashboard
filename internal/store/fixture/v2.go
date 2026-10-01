package fixture

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// OpenCodeV2Schema mirrors the OpenCode 2.0.21 tables the dashboard reads,
// copied from packages/core/src/database/schema.gen.ts.
const OpenCodeV2Schema = `
CREATE TABLE project (
	id text PRIMARY KEY,
	worktree text NOT NULL,
	vcs text,
	name text,
	icon_url text,
	icon_url_override text,
	icon_color text,
	time_created integer NOT NULL,
	time_updated integer NOT NULL,
	time_initialized integer,
	time_active integer DEFAULT 0 NOT NULL,
	sandboxes text NOT NULL,
	commands text
);

CREATE TABLE workspace (
	id text PRIMARY KEY,
	provider text NOT NULL,
	binding text,
	created_at integer NOT NULL,
	last_used_at integer NOT NULL
);

CREATE TABLE kv (
	key text PRIMARY KEY,
	value text NOT NULL,
	time_created integer NOT NULL,
	time_updated integer NOT NULL
);

CREATE TABLE session_v2 (
	id text PRIMARY KEY,
	project_id text NOT NULL,
	workspace_id text,
	parent_id text,
	fork_session_id text,
	fork_boundary text,
	slug text NOT NULL,
	directory text NOT NULL,
	path text,
	title text,
	version text NOT NULL,
	share_url text,
	summary_additions integer,
	summary_deletions integer,
	summary_files integer,
	summary_diffs text,
	metadata text,
	cost real DEFAULT 0 NOT NULL,
	tokens_input integer DEFAULT 0 NOT NULL,
	tokens_output integer DEFAULT 0 NOT NULL,
	tokens_reasoning integer DEFAULT 0 NOT NULL,
	tokens_cache_read integer DEFAULT 0 NOT NULL,
	tokens_cache_write integer DEFAULT 0 NOT NULL,
	revert text,
	permission text,
	agent text,
	model text,
	time_created integer NOT NULL,
	time_updated integer NOT NULL,
	time_idle integer,
	time_viewed integer,
	idle_outcome text,
	time_compacting integer,
	time_archived integer,
	time_suspended integer,
	resume_attempts integer DEFAULT 0 NOT NULL,
	CONSTRAINT fk_session_v2_project_id_project_id_fk FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE CASCADE
);

CREATE TABLE session_message (
	id text PRIMARY KEY,
	session_id text NOT NULL,
	type text NOT NULL,
	seq integer NOT NULL,
	time_created integer NOT NULL,
	time_updated integer NOT NULL,
	data text NOT NULL,
	CONSTRAINT fk_session_message_session_id_session_v2_id_fk FOREIGN KEY (session_id) REFERENCES session_v2(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX session_message_session_seq_idx ON session_message (session_id, seq);
CREATE INDEX session_message_session_type_seq_idx ON session_message (session_id, type, seq);
CREATE INDEX session_message_session_time_created_id_idx ON session_message (session_id, time_created, id);
CREATE INDEX session_message_time_created_idx ON session_message (time_created);
CREATE INDEX session_v2_project_idx ON session_v2 (project_id);
CREATE INDEX session_v2_parent_idx ON session_v2 (parent_id);
`

// V2Session is one session_v2 row. ForkOf marks a fork of that session.
type V2Session struct {
	ID        string
	ProjectID string
	Title     string
	Directory string
	ParentID  string
	ForkOf    string
	Created   time.Time
	Updated   time.Time
}

// V2Row is one session_message row; Data is marshalled to the JSON payload.
type V2Row struct {
	ID        string
	SessionID string
	Type      string
	Seq       int
	Created   time.Time
	Data      map[string]any
}

// V2Builder builds an OpenCode 2 database. WithLegacyTables adds the 1.x
// tables an upgraded database still carries; Exec runs extra statements
// (e.g. legacy rows) after the schema exists.
type V2Builder struct {
	projects []*ProjectBuilder
	sessions []V2Session
	rows     []V2Row
	legacy   bool
	kv       map[string]string
	exec     []execStatement
}

type execStatement struct {
	query string
	args  []any
}

func NewV2Builder() *V2Builder {
	return &V2Builder{kv: map[string]string{}}
}

func (b *V2Builder) AddProject(p *ProjectBuilder) *V2Builder {
	b.projects = append(b.projects, p)
	return b
}

func (b *V2Builder) AddSession(s V2Session) *V2Builder {
	b.sessions = append(b.sessions, s)
	return b
}

func (b *V2Builder) AddRow(r V2Row) *V2Builder {
	b.rows = append(b.rows, r)
	return b
}

func (b *V2Builder) WithLegacyTables() *V2Builder {
	b.legacy = true
	return b
}

// SetKV stores a kv row; value must be JSON.
func (b *V2Builder) SetKV(key, value string) *V2Builder {
	b.kv[key] = value
	return b
}

func (b *V2Builder) Exec(query string, args ...any) *V2Builder {
	b.exec = append(b.exec, execStatement{query: query, args: args})
	return b
}

// Build writes the database to a new temp directory and returns its path.
func (b *V2Builder) Build(ctx context.Context) (string, error) {
	tmpDir, err := os.MkdirTemp("", "opencode-v2-fixture-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	dbPath := filepath.Join(tmpDir, "opencode.db")
	if err := b.write(ctx, dbPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	return dbPath, nil
}

func (b *V2Builder) write(ctx context.Context, dbPath string) error {
	db, err := sql.Open("sqlite", dbPath+"?mode=rwc&_journal=WAL")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, OpenCodeV2Schema); err != nil {
		return fmt.Errorf("failed to create v2 schema: %w", err)
	}
	if b.legacy {
		// IF NOT EXISTS keeps the 2.x project and workspace tables, as in a
		// real upgraded database.
		if _, err := db.ExecContext(ctx, OpenCodeSchema); err != nil {
			return fmt.Errorf("failed to create legacy schema: %w", err)
		}
	}

	for _, p := range b.projects {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO project (id, worktree, name, time_created, time_updated, sandboxes)
			VALUES (?, ?, ?, 0, 0, '[]')
		`, p.id, p.worktree, nullString(p.name)); err != nil {
			return fmt.Errorf("failed to insert project %q: %w", p.id, err)
		}
	}
	for _, s := range b.sessions {
		directory := s.Directory
		if directory == "" {
			directory = "/work"
		}
		updated := s.Updated
		if updated.IsZero() {
			updated = s.Created
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO session_v2 (id, project_id, parent_id, fork_session_id, slug, directory, title, version, time_created, time_updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, '2.0.21', ?, ?)
		`, s.ID, s.ProjectID, nullString(s.ParentID), nullString(s.ForkOf), s.ID, directory, nullString(s.Title), s.Created.UnixMilli(), updated.UnixMilli()); err != nil {
			return fmt.Errorf("failed to insert session %q: %w", s.ID, err)
		}
	}
	for _, r := range b.rows {
		data := r.Data
		if data == nil {
			data = map[string]any{}
		}
		if _, ok := data["time"]; !ok {
			data["time"] = map[string]any{"created": r.Created.UnixMilli()}
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to encode row %q: %w", r.ID, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO session_message (id, session_id, type, seq, time_created, time_updated, data)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, r.ID, r.SessionID, r.Type, r.Seq, r.Created.UnixMilli(), r.Created.UnixMilli(), string(encoded)); err != nil {
			return fmt.Errorf("failed to insert row %q: %w", r.ID, err)
		}
	}
	for key, value := range b.kv {
		if _, err := db.ExecContext(ctx, `INSERT INTO kv (key, value, time_created, time_updated) VALUES (?, ?, 0, 0)`, key, value); err != nil {
			return fmt.Errorf("failed to insert kv %q: %w", key, err)
		}
	}
	for _, statement := range b.exec {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("failed to exec %q: %w", statement.query, err)
		}
	}
	return nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// V2User is a user prompt payload.
func V2User(text string) map[string]any {
	return map[string]any{"text": text, "files": []any{}}
}

// V2Assistant is an assistant step payload. A nil usage omits cost and
// tokens, as OpenCode does before a step settles or when it failed early.
func V2Assistant(providerID, modelID string, usage *V2Usage, content ...map[string]any) map[string]any {
	if content == nil {
		content = []map[string]any{}
	}
	data := map[string]any{
		"agent":   "build",
		"model":   map[string]any{"id": modelID, "providerID": providerID, "variant": "default"},
		"content": content,
	}
	if usage != nil {
		data["cost"] = usage.Cost
		data["tokens"] = usage.tokens()
	}
	return data
}

// V2Compaction is a compaction payload with the given status ("running",
// "completed", "failed"). A nil usage omits cost and tokens.
func V2Compaction(status string, usage *V2Usage, model map[string]any) map[string]any {
	data := map[string]any{"status": status, "reason": "auto", "summary": "summary of earlier work", "recent": ""}
	if model != nil {
		data["model"] = model
	}
	if usage != nil {
		data["cost"] = usage.Cost
		data["tokens"] = usage.tokens()
	}
	return data
}

// V2Usage is one request's reported usage.
type V2Usage struct {
	Cost                                            float64
	Input, Output, Reasoning, CacheRead, CacheWrite int64
}

func (u V2Usage) tokens() map[string]any {
	return map[string]any{
		"input":     u.Input,
		"output":    u.Output,
		"reasoning": u.Reasoning,
		"cache":     map[string]any{"read": u.CacheRead, "write": u.CacheWrite},
	}
}

// V2Text is assistant text content.
func V2Text(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

// V2Reasoning is assistant reasoning content.
func V2Reasoning(text string) map[string]any {
	return map[string]any{"type": "reasoning", "text": text}
}

// V2Tool is an assistant tool call with the given state.
func V2Tool(callID, name string, state map[string]any) map[string]any {
	return map[string]any{
		"type":  "tool",
		"id":    callID,
		"name":  name,
		"state": state,
		"time":  map[string]any{"created": 1000, "completed": 2000},
	}
}
