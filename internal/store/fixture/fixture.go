// Package fixture provides test database setup utilities for OpenCode-like SQLite databases.
package fixture

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// OpenCodeSchema defines the expected schema for an OpenCode database.
const OpenCodeSchema = `
CREATE TABLE IF NOT EXISTS session (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	workspace_id TEXT,
	parent_id TEXT,
	slug TEXT NOT NULL,
	directory TEXT NOT NULL,
	title TEXT,
	version TEXT NOT NULL,
	share_url TEXT,
	summary TEXT,
	time_created TEXT NOT NULL,
	time_updated TEXT NOT NULL,
	time_compacting TEXT,
	time_archived TEXT
);

CREATE TABLE IF NOT EXISTS message (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	time_created TEXT NOT NULL,
	data TEXT NOT NULL,
	FOREIGN KEY (session_id) REFERENCES session(id)
);

CREATE TABLE IF NOT EXISTS project (
	id TEXT PRIMARY KEY,
	worktree TEXT NOT NULL,
	name TEXT,
	vcs TEXT,
	sandboxes TEXT
);

CREATE TABLE IF NOT EXISTS workspace (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	type TEXT NOT NULL,
	branch TEXT,
	name TEXT,
	directory TEXT,
	FOREIGN KEY (project_id) REFERENCES project(id)
);

CREATE TABLE IF NOT EXISTS part (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	message_id TEXT,
	tool TEXT,
	input TEXT,
	output TEXT,
	data TEXT,
	time_created TEXT NOT NULL,
	time_updated TEXT NOT NULL,
	metadata TEXT,
	FOREIGN KEY (session_id) REFERENCES session(id),
	FOREIGN KEY (message_id) REFERENCES message(id)
);
`

// SessionBuilder provides a fluent API for constructing test sessions.
type SessionBuilder struct {
	id        string
	projectID string
	title     string
	directory string
	slug      string
	version   string
	createdAt time.Time
	updatedAt time.Time
	messages  []MessageBuilder
}

// NewSession creates a new session builder with required fields.
func NewSession(id, projectID string) *SessionBuilder {
	return &SessionBuilder{
		id:        id,
		projectID: projectID,
		directory: "/test/project",
		slug:      "test-session",
		version:   "1.0.0",
		createdAt: time.Now().UTC(),
		updatedAt: time.Now().UTC(),
	}
}

func (s *SessionBuilder) Title(title string) *SessionBuilder {
	s.title = title
	return s
}

func (s *SessionBuilder) Directory(dir string) *SessionBuilder {
	s.directory = dir
	return s
}

func (s *SessionBuilder) Slug(slug string) *SessionBuilder {
	s.slug = slug
	return s
}

func (s *SessionBuilder) Version(v string) *SessionBuilder {
	s.version = v
	return s
}

func (s *SessionBuilder) CreatedAt(t time.Time) *SessionBuilder {
	s.createdAt = t
	return s
}

func (s *SessionBuilder) UpdatedAt(t time.Time) *SessionBuilder {
	s.updatedAt = t
	return s
}

func (s *SessionBuilder) AddMessage(msg *MessageBuilder) *SessionBuilder {
	s.messages = append(s.messages, *msg)
	return s
}

func (s *SessionBuilder) AddUserMessage(id string, createdAt time.Time) *SessionBuilder {
	return s.AddMessage(NewMessage(id, s.id, "user").CreatedAt(createdAt))
}

func (s *SessionBuilder) AddAssistantMessage(id string, createdAt time.Time, cost float64, modelID, providerID string, input, output, reasoning int64) *SessionBuilder {
	msg := NewMessage(id, s.id, "assistant").
		CreatedAt(createdAt).
		Cost(cost).
		ModelID(modelID).
		ProviderID(providerID).
		Tokens(input, output, reasoning)
	return s.AddMessage(msg)
}

// MessageBuilder provides a fluent API for constructing test messages.
type MessageBuilder struct {
	id         string
	sessionID  string
	role       string
	createdAt  time.Time
	cost       float64
	modelID    string
	providerID string
	input      int64
	output     int64
	reasoning  int64
}

// NewMessage creates a new message builder with required fields.
func NewMessage(id, sessionID, role string) *MessageBuilder {
	return &MessageBuilder{
		id:        id,
		sessionID: sessionID,
		role:      role,
		createdAt: time.Now().UTC(),
	}
}

func (m *MessageBuilder) CreatedAt(t time.Time) *MessageBuilder {
	m.createdAt = t
	return m
}

func (m *MessageBuilder) Cost(c float64) *MessageBuilder {
	m.cost = c
	return m
}

func (m *MessageBuilder) ModelID(id string) *MessageBuilder {
	m.modelID = id
	return m
}

func (m *MessageBuilder) ProviderID(id string) *MessageBuilder {
	m.providerID = id
	return m
}

func (m *MessageBuilder) Tokens(input, output, reasoning int64) *MessageBuilder {
	m.input = input
	m.output = output
	m.reasoning = reasoning
	return m
}

// ProjectBuilder provides a fluent API for constructing test projects.
type ProjectBuilder struct {
	id       string
	worktree string
	name     string
}

// NewProject creates a new project builder.
func NewProject(id, worktree string) *ProjectBuilder {
	return &ProjectBuilder{
		id:       id,
		worktree: worktree,
	}
}

func (p *ProjectBuilder) Name(name string) *ProjectBuilder {
	p.name = name
	return p
}

// Builder aggregates all test data to create a fixture database.
type Builder struct {
	sessions []*SessionBuilder
	projects []*ProjectBuilder
	parts    []*PartBuilder
}

// NewBuilder creates a new fixture builder.
func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) AddProject(p *ProjectBuilder) *Builder {
	b.projects = append(b.projects, p)
	return b
}

func (b *Builder) AddSession(s *SessionBuilder) *Builder {
	b.sessions = append(b.sessions, s)
	return b
}

func (b *Builder) AddPart(p *PartBuilder) *Builder {
	b.parts = append(b.parts, p)
	return b
}

// Build creates a temporary SQLite database with the fixture data.
// Returns the path to the database file, which should be cleaned up after use.
func (b *Builder) Build(ctx context.Context) (string, error) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "opencode-fixture-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	dbPath := filepath.Join(tmpDir, "opencode.db")

	dsn := dbPath + "?mode=rw&_journal=WAL"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to open database: %w", err)
	}

	// Create schema
	if _, err := db.ExecContext(ctx, OpenCodeSchema); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to create schema: %w", err)
	}

	// Insert projects
	for _, p := range b.projects {
		if err := insertProject(ctx, db, p); err != nil {
			db.Close()
			os.RemoveAll(tmpDir)
			return "", err
		}
	}

	// Ensure at least one project exists if we have sessions
	if len(b.sessions) > 0 && len(b.projects) == 0 {
		// Create a default project for sessions without explicit project
		defaultProject := NewProject("default-project", "/default/path").Name("default-project")
		if err := insertProject(ctx, db, defaultProject); err != nil {
			db.Close()
			os.RemoveAll(tmpDir)
			return "", err
		}
	}

	// Insert sessions and their messages
	for _, s := range b.sessions {
		// Ensure session has a valid project_id
		if s.projectID == "" {
			s.projectID = "default-project"
		}

		if err := insertSession(ctx, db, s); err != nil {
			db.Close()
			os.RemoveAll(tmpDir)
			return "", err
		}

		for _, m := range s.messages {
			m.sessionID = s.id // Ensure message links to correct session
			if err := insertMessage(ctx, db, &m); err != nil {
				db.Close()
				os.RemoveAll(tmpDir)
				return "", err
			}
		}
	}

	for _, p := range b.parts {
		if err := insertPart(ctx, db, p); err != nil {
			db.Close()
			os.RemoveAll(tmpDir)
			return "", err
		}
	}

	if err := db.Close(); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to close database: %w", err)
	}

	return dbPath, nil
}

func insertProject(ctx context.Context, db *sql.DB, p *ProjectBuilder) error {
	query := `INSERT OR REPLACE INTO project (id, worktree, name) VALUES (?, ?, ?)`
	_, err := db.ExecContext(ctx, query, p.id, p.worktree, p.name)
	if err != nil {
		return fmt.Errorf("failed to insert project %q: %w", p.id, err)
	}
	return nil
}

func insertSession(ctx context.Context, db *sql.DB, s *SessionBuilder) error {
	query := `INSERT OR REPLACE INTO session 
		(id, project_id, slug, directory, title, version, time_created, time_updated) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query,
		s.id,
		s.projectID,
		s.slug,
		s.directory,
		s.title,
		s.version,
		s.createdAt.UnixMilli(),
		s.updatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert session %q: %w", s.id, err)
	}
	return nil
}

func insertMessage(ctx context.Context, db *sql.DB, m *MessageBuilder) error {
	// Build JSON data for message (OpenCode stores message metadata in data column)
	data := buildMessageJSON(m)

	query := `INSERT OR REPLACE INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query,
		m.id,
		m.sessionID,
		m.createdAt.UnixMilli(),
		data,
	)
	if err != nil {
		return fmt.Errorf("failed to insert message %q: %w", m.id, err)
	}
	return nil
}

// PartBuilder provides a fluent API for constructing part rows.
type PartBuilder struct {
	id        string
	sessionID string
	messageID string
	data      string
	tool      string
	createdAt time.Time
	updatedAt time.Time
}

// NewPart creates a new part builder with required fields.
func NewPart(id, sessionID, data string) *PartBuilder {
	return &PartBuilder{
		id:        id,
		sessionID: sessionID,
		data:      data,
		createdAt: time.Now().UTC(),
		updatedAt: time.Now().UTC(),
	}
}

func (p *PartBuilder) Tool(tool string) *PartBuilder {
	p.tool = tool
	return p
}

func (p *PartBuilder) MessageID(id string) *PartBuilder {
	p.messageID = id
	return p
}

func (p *PartBuilder) CreatedAt(t time.Time) *PartBuilder {
	p.createdAt = t
	return p
}

func (p *PartBuilder) UpdatedAt(t time.Time) *PartBuilder {
	p.updatedAt = t
	return p
}

func insertPart(ctx context.Context, db *sql.DB, p *PartBuilder) error {
	query := `INSERT OR REPLACE INTO part (id, session_id, message_id, tool, data, time_created, time_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query,
		p.id,
		p.sessionID,
		p.messageID,
		p.tool,
		p.data,
		p.createdAt.UnixMilli(),
		p.updatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert part %q: %w", p.id, err)
	}
	return nil
}

func buildMessageJSON(m *MessageBuilder) string {
	// Simplified JSON matching OpenCode's expected structure
	if m.role == "user" {
		return fmt.Sprintf(`{"role":"user"}`)
	}

	// Assistant message with cost/tokens
	return fmt.Sprintf(`{"role":"assistant","cost":%.6f,"modelID":"%s","providerID":"%s","tokens":{"input":%d,"output":%d,"reasoning":%d,"cache":{"read":0,"write":0}}}`,
		m.cost, m.modelID, m.providerID, m.input, m.output, m.reasoning)
}

// SampleFixture creates a typical OpenCode-like database for testing.
// This provides representative data across multiple sessions, projects, and models.
func SampleFixture(ctx context.Context) (string, error) {
	now := time.Now().UTC()
	day1 := now.AddDate(0, 0, -6)
	day2 := now.AddDate(0, 0, -3)
	day3 := now

	b := NewBuilder()

	// Add projects
	b.AddProject(NewProject("proj-001", "/home/user/opencode-dashboard").Name("opencode-dashboard"))
	b.AddProject(NewProject("proj-002", "/home/user/my-app").Name("my-app"))
	b.AddProject(NewProject("proj-003", "/home/user/legacy-project").Name("legacy-project"))

	// Session 1: opencode-dashboard with multiple messages
	s1 := NewSession("ses-001", "proj-001").
		Title("Implement auth middleware").
		CreatedAt(day1).
		UpdatedAt(day1)
	s1.AddUserMessage("msg-001-01", day1.Add(1*time.Minute))
	s1.AddAssistantMessage("msg-001-02", day1.Add(2*time.Minute), 0.05, "claude-3-sonnet", "anthropic", 1000, 500, 100)
	s1.AddUserMessage("msg-001-03", day1.Add(3*time.Minute))
	s1.AddAssistantMessage("msg-001-04", day1.Add(4*time.Minute), 0.08, "claude-3-sonnet", "anthropic", 2000, 800, 150)
	b.AddSession(s1)

	// Session 2: my-app with single exchange
	s2 := NewSession("ses-002", "proj-002").
		Title("Fix login bug").
		CreatedAt(day2).
		UpdatedAt(day2)
	s2.AddUserMessage("msg-002-01", day2.Add(1*time.Minute))
	s2.AddAssistantMessage("msg-002-02", day2.Add(2*time.Minute), 0.03, "gpt-4", "openai", 800, 400, 50)
	b.AddSession(s2)

	// Session 3: opencode-dashboard with different model
	s3 := NewSession("ses-003", "proj-001").
		Title("Add unit tests").
		CreatedAt(day3).
		UpdatedAt(day3)
	s3.AddUserMessage("msg-003-01", day3.Add(1*time.Minute))
	s3.AddAssistantMessage("msg-003-02", day3.Add(2*time.Minute), 0.12, "gpt-4-turbo", "openai", 3000, 1200, 200)
	s3.AddUserMessage("msg-003-03", day3.Add(3*time.Minute))
	s3.AddAssistantMessage("msg-003-04", day3.Add(4*time.Minute), 0.15, "gpt-4-turbo", "openai", 4000, 1500, 250)
	b.AddSession(s3)

	// Session 4: legacy-project (no title)
	s4 := NewSession("ses-004", "proj-003").
		CreatedAt(day3.Add(-1 * time.Hour)).
		UpdatedAt(day3.Add(-1 * time.Hour))
	s4.AddUserMessage("msg-004-01", day3.Add(-1*time.Hour).Add(1*time.Minute))
	s4.AddAssistantMessage("msg-004-02", day3.Add(-1*time.Hour).Add(2*time.Minute), 0.02, "gpt-3.5-turbo", "openai", 500, 200, 30)
	b.AddSession(s4)

	return b.Build(ctx)
}
