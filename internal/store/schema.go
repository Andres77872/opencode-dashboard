// Package store provides SQLite connection management for OpenCode database.
// This file contains domain types matching OpenCode's SQLite schema.
package store

import "time"

// CacheUsage tracks cache token operations.
type CacheUsage struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// TokenUsage tracks token consumption for assistant messages.
type TokenUsage struct {
	Input     int64      `json:"input"`
	Output    int64      `json:"output"`
	Reasoning int64      `json:"reasoning"`
	Cache     CacheUsage `json:"cache"`
}

// SessionSummary contains code change statistics from a session.
type SessionSummary struct {
	Additions int64 `json:"additions"`
	Deletions int64 `json:"deletions"`
	Files     int64 `json:"files"`
}

// Session represents an OpenCode conversation session.
// Matches the session table in opencode.db.
type Session struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"project_id"`
	WorkspaceID    *string         `json:"workspace_id,omitempty"`
	ParentID       *string         `json:"parent_id,omitempty"`
	Slug           string          `json:"slug"`
	Directory      string          `json:"directory"`
	Title          string          `json:"title"`
	Version        string          `json:"version"`
	ShareURL       *string         `json:"share_url,omitempty"`
	Summary        *SessionSummary `json:"summary,omitempty"`
	TimeCreated    time.Time       `json:"time_created"`
	TimeUpdated    time.Time       `json:"time_updated"`
	TimeCompacting *time.Time      `json:"time_compacting,omitempty"`
	TimeArchived   *time.Time      `json:"time_archived,omitempty"`
}

// MessageRole distinguishes between user and assistant messages.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// Message represents a single message in a session.
// Matches the message table in opencode.db.
// User messages have role="user" with no cost/token data.
// Assistant messages have role="assistant" with cost, tokens, model, provider, agent.
type Message struct {
	ID          string      `json:"id"`
	SessionID   string      `json:"session_id"`
	Role        MessageRole `json:"role"`
	TimeCreated time.Time   `json:"time_created"`
	Cost        float64     `json:"cost,omitempty"`
	Tokens      *TokenUsage `json:"tokens,omitempty"`
	ModelID     *string     `json:"model_id,omitempty"`
	ProviderID  *string     `json:"provider_id,omitempty"`
	Agent       *string     `json:"agent,omitempty"`
}

// Project represents a tracked project (git repository).
// Matches the project table in opencode.db.
// Project ID is derived from the first commit hash.
type Project struct {
	ID        string   `json:"id"`
	Worktree  string   `json:"worktree"`
	Name      *string  `json:"name,omitempty"`
	VCS       *string  `json:"vcs,omitempty"`
	Sandboxes []string `json:"sandboxes,omitempty"`
}

// Workspace represents a git worktree within a project.
// Matches the workspace table in opencode.db.
type Workspace struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	Type      string  `json:"type"`
	Branch    *string `json:"branch,omitempty"`
	Name      *string `json:"name,omitempty"`
	Directory *string `json:"directory,omitempty"`
}
