// Package chatstore owns the durable SQLite store for analytics-assistant
// chat sessions. It is deliberately separate from the usage cache: the cache
// holds rebuildable derived data and is dropped on schema changes, while chat
// history is the record of conversations the local user actually had.
//
// The store persists the browser-visible conversation plus everything the
// assistant generated to produce it: the analytics tool invocations (name,
// normalized accepted arguments or redacted rejected arguments, output
// envelope, status, duration), each delegated
// specialist run with its finding and usage, per-turn provider token
// accounting, and the dashboard context the question was asked from.
// Everything stored here is data the local user already saw in the dashboard
// UI; source transcripts are never written to this database.
//
// The schema is versioned but deliberately not migrated. Assistant history is
// a local, reproducible convenience rather than a system of record, so an
// unrecognized version is rebuilt from scratch instead of being upgraded in
// place. Callers can report that through Recreated.
package chatstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// schemaVersion is stamped into PRAGMA user_version. A database at any other
// version is rebuilt empty: this store never migrates assistant history.
const schemaVersion = 2

const (
	busyTimeout = 5 * time.Second

	historyKeyMetaName = "history_key_v1"
	historyKeyBytes    = 32

	sessionIDPrefix = "cs_"

	// MaxTitleLength bounds stored session titles; longer titles are trimmed
	// at a rune boundary with an ellipsis.
	MaxTitleLength = 80

	// DefaultSessionListLimit is used when a caller does not bound a listing.
	DefaultSessionListLimit = 50
)

var (
	ErrSessionNotFound = errors.New("assistant chat session not found")
	ErrInvalidTurn     = errors.New("invalid assistant chat turn")
)

// chatTables is the complete set of tables this store owns. It drives both
// creation and the rebuild that replaces an unrecognized schema.
var chatTables = []string{
	"chat_subagent_runs", "chat_tool_calls", "chat_messages", "chat_turns", "chat_sessions", "chat_meta",
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS chat_meta (
	key TEXT PRIMARY KEY,
	value BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_sessions (
	session_id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	consent_version TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL,
	turn_count INTEGER NOT NULL DEFAULT 0,
	tool_call_count INTEGER NOT NULL DEFAULT 0,
	subagent_count INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	requests INTEGER NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cached_input_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS chat_turns (
	turn_id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES chat_sessions(session_id) ON DELETE CASCADE,
	turn_index INTEGER NOT NULL,
	agent TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	rounds INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	requests INTEGER NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cached_input_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	route TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	period TEXT NOT NULL DEFAULT '',
	timezone TEXT NOT NULL DEFAULT '',
	notices_json TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_messages (
	message_id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES chat_sessions(session_id) ON DELETE CASCADE,
	turn_id INTEGER NOT NULL REFERENCES chat_turns(turn_id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	signature TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_tool_calls (
	tool_call_id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL REFERENCES chat_messages(message_id) ON DELETE CASCADE,
	session_id TEXT NOT NULL,
	call_index INTEGER NOT NULL,
	call_ref TEXT NOT NULL DEFAULT '',
	parent_call_ref TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT '',
	round INTEGER NOT NULL DEFAULT 0,
	tool_name TEXT NOT NULL,
	arguments_json TEXT NOT NULL DEFAULT '{}',
	result_json TEXT NOT NULL DEFAULT '',
	ok INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_subagent_runs (
	subagent_run_id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL REFERENCES chat_messages(message_id) ON DELETE CASCADE,
	session_id TEXT NOT NULL,
	run_index INTEGER NOT NULL,
	call_ref TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	task TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	report TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	rounds INTEGER NOT NULL DEFAULT 0,
	tools_used_json TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	requests INTEGER NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cached_input_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	created_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id, message_id);
CREATE INDEX IF NOT EXISTS idx_chat_turns_session ON chat_turns(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_chat_tool_calls_message ON chat_tool_calls(message_id, call_index);
CREATE INDEX IF NOT EXISTS idx_chat_subagent_runs_message ON chat_subagent_runs(message_id, run_index);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_updated ON chat_sessions(updated_ms);
`

type Store struct {
	db        *sql.DB
	path      string
	recreated bool
}

// Usage is the provider token accounting recorded for a turn, a session, or one
// specialist run. A zero counter means the provider reported nothing, never
// that the work was free.
type Usage struct {
	Requests          int64 `json:"requests"`
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
	TotalTokens       int64 `json:"total_tokens"`
}

// Session is one persisted assistant conversation with its running totals.
type Session struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	ConsentVersion string `json:"consent_version,omitempty"`
	CreatedMS      int64  `json:"created_ms"`
	UpdatedMS      int64  `json:"updated_ms"`
	MessageCount   int64  `json:"message_count"`
	TurnCount      int64  `json:"turn_count"`
	ToolCallCount  int64  `json:"tool_call_count"`
	SubagentCount  int64  `json:"subagent_count"`
	DurationMS     int64  `json:"duration_ms"`
	Usage          Usage  `json:"usage"`
}

// ToolCall is one analytics tool invocation made while producing an assistant
// message, including normalized arguments for an accepted call (or {} for a
// rejected proposal) and the output envelope exchanged with the tool.
type ToolCall struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	// CallRef is the stream-local id the browser used for this call, and
	// ParentCallRef links a specialist's call to the delegation that started it.
	CallRef       string          `json:"call_ref,omitempty"`
	ParentCallRef string          `json:"parent_call_ref,omitempty"`
	Agent         string          `json:"agent,omitempty"`
	Round         int             `json:"round,omitempty"`
	Arguments     json.RawMessage `json:"arguments,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	OK            bool            `json:"ok"`
	DurationMS    int64           `json:"duration_ms"`
}

// SubagentRun is one delegated specialist investigation: the task it was given,
// the finding it returned, and what the delegation cost.
type SubagentRun struct {
	Index      int      `json:"index"`
	CallRef    string   `json:"call_ref,omitempty"`
	Agent      string   `json:"agent"`
	Title      string   `json:"title,omitempty"`
	Task       string   `json:"task,omitempty"`
	Status     string   `json:"status,omitempty"`
	Report     string   `json:"report,omitempty"`
	Error      string   `json:"error,omitempty"`
	Rounds     int      `json:"rounds,omitempty"`
	ToolsUsed  []string `json:"tools_used,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Usage      Usage    `json:"usage"`
}

// TurnContext is the dashboard view the question was asked from. It is stored
// so a restored conversation can be read in the context it was created in.
type TurnContext struct {
	Route    string `json:"route,omitempty"`
	Source   string `json:"source,omitempty"`
	Period   string `json:"period,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// Message is one persisted chat message. Assistant messages carry everything
// that produced them: the tool calls, the specialist runs, the turn's usage and
// timing, and the HMAC signature the stateless chat protocol needs to replay
// them.
type Message struct {
	ID         int64         `json:"id"`
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	Signature  string        `json:"signature,omitempty"`
	Model      string        `json:"model,omitempty"`
	Agent      string        `json:"agent,omitempty"`
	CreatedMS  int64         `json:"created_ms"`
	TurnIndex  int           `json:"turn_index"`
	Rounds     int           `json:"rounds,omitempty"`
	DurationMS int64         `json:"duration_ms,omitempty"`
	Usage      *Usage        `json:"usage,omitempty"`
	Context    *TurnContext  `json:"context,omitempty"`
	Notices    []string      `json:"notices,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	Subagents  []SubagentRun `json:"subagents,omitempty"`
}

type SessionDetail struct {
	Session  Session   `json:"session"`
	Messages []Message `json:"messages"`
}

// Turn is one completed prompt/answer exchange to persist atomically.
type Turn struct {
	// SessionID selects an existing session; empty creates a new one titled
	// from the user prompt.
	SessionID          string
	Provider           string
	Model              string
	Agent              string
	ConsentVersion     string
	Context            TurnContext
	UserContent        string
	AssistantContent   string
	AssistantSignature string
	Rounds             int
	DurationMS         int64
	Usage              Usage
	Notices            []string
	ToolCalls          []ToolCall
	Subagents          []SubagentRun
}

// Receipt reports where a turn landed, so the browser can adopt the session and
// show its running totals without a second round trip.
type Receipt struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	TurnIndex int    `json:"turn_index"`
	Session   Usage  `json:"session_usage"`
}

// Open opens (or creates) the chat store at path, creating parent directories
// as needed.
func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("chat store path is required")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create chat store directory: %w", err)
		}
	}
	db, err := openDB(ctx, path)
	if err != nil {
		return nil, err
	}
	recreated, err := ensureSchema(ctx, db, path)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, path: path, recreated: recreated}, nil
}

func openDB(ctx context.Context, path string) (*sql.DB, error) {
	params := []string{
		"_txlock=immediate",
		fmt.Sprintf("_pragma=busy_timeout(%d)", busyTimeout.Milliseconds()),
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(1)",
	}
	db, err := sql.Open("sqlite", path+"?"+strings.Join(params, "&"))
	if err != nil {
		return nil, fmt.Errorf("open chat store: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect chat store: %w", err)
	}
	return db, nil
}

// ensureSchema creates the current schema, rebuilding the database when it was
// written by a different version of this store. It reports whether existing
// history was discarded. A database that is not an assistant chat store is
// never modified.
func ensureSchema(ctx context.Context, db *sql.DB, path string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin chat store schema check: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version, tables, chatTableCount int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return false, fmt.Errorf("read chat store schema version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
		return false, fmt.Errorf("inspect chat store: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('chat_sessions', 'chat_meta')`).Scan(&chatTableCount); err != nil {
		return false, fmt.Errorf("inspect chat store: %w", err)
	}
	if tables > 0 && chatTableCount == 0 {
		return false, fmt.Errorf("%s is not an assistant chat database; refusing to modify it", path)
	}

	// Assistant history is never migrated: an unrecognized schema is rebuilt so
	// the dashboard always runs against exactly the shape this binary writes.
	recreated := false
	if tables > 0 && version != schemaVersion {
		for _, table := range chatTables {
			if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
				return false, fmt.Errorf("rebuild chat store: %w", err)
			}
		}
		recreated = true
	}
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return false, fmt.Errorf("create chat store schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return false, fmt.Errorf("record chat store schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit chat store schema: %w", err)
	}
	return recreated, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Recreated reports whether opening this store discarded history written by a
// different schema version.
func (s *Store) Recreated() bool {
	return s != nil && s.recreated
}

// HistoryKey loads the persistent assistant-history signing key, generating
// and storing one on first use. Persisting the key keeps saved assistant
// message signatures verifiable across dashboard restarts.
func (s *Store) HistoryKey(ctx context.Context) ([]byte, error) {
	var value []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM chat_meta WHERE key = ?`, historyKeyMetaName).Scan(&value)
	if err == nil && len(value) == historyKeyBytes {
		return value, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("read chat history key: %w", err)
	}
	fresh := make([]byte, historyKeyBytes)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("generate chat history key: %w", err)
	}
	// A concurrent opener may have won the race; the stored key stays
	// authoritative either way.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO chat_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO NOTHING`, historyKeyMetaName, fresh); err != nil {
		return nil, fmt.Errorf("store chat history key: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM chat_meta WHERE key = ?`, historyKeyMetaName).Scan(&value); err != nil {
		return nil, fmt.Errorf("reload chat history key: %w", err)
	}
	if len(value) != historyKeyBytes {
		return nil, errors.New("chat history key has an unexpected size")
	}
	return value, nil
}

// IsValidSessionID reports whether value looks like an identifier this store
// generates. It accepts only the exact "cs_" + 32 lowercase hex shape so
// session ids can safely travel through URLs and JSON.
func IsValidSessionID(value string) bool {
	if len(value) != len(sessionIDPrefix)+2*16 || !strings.HasPrefix(value, sessionIDPrefix) {
		return false
	}
	for _, char := range value[len(sessionIDPrefix):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func newSessionID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate chat session id: %w", err)
	}
	return sessionIDPrefix + hex.EncodeToString(raw), nil
}

// TitleFromPrompt derives a bounded single-line session title from the first
// user prompt.
func TitleFromPrompt(prompt string) string {
	title := strings.Join(strings.Fields(prompt), " ")
	if title == "" {
		return "New conversation"
	}
	runes := []rune(title)
	if len(runes) > MaxTitleLength {
		title = strings.TrimSpace(string(runes[:MaxTitleLength-1])) + "…"
	}
	return title
}

// SessionExists reports whether id names a persisted session.
func (s *Store) SessionExists(ctx context.Context, id string) (bool, error) {
	if !IsValidSessionID(id) {
		return false, nil
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM chat_sessions WHERE session_id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check chat session: %w", err)
	}
	return true, nil
}

// AppendTurn persists one completed exchange atomically: the prompt, the
// answer, every tool call, every specialist run, and the turn's accounting. The
// session totals are advanced in the same transaction so a listing never
// disagrees with the turns it summarizes.
func (s *Store) AppendTurn(ctx context.Context, turn Turn) (Receipt, error) {
	if strings.TrimSpace(turn.UserContent) == "" {
		return Receipt{}, fmt.Errorf("%w: user content is required", ErrInvalidTurn)
	}
	if strings.TrimSpace(turn.AssistantContent) == "" {
		return Receipt{}, fmt.Errorf("%w: assistant content is required", ErrInvalidTurn)
	}
	now := time.Now().UnixMilli()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, fmt.Errorf("begin chat turn: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	receipt := Receipt{SessionID: turn.SessionID}
	if receipt.SessionID == "" {
		receipt.SessionID, err = newSessionID()
		if err != nil {
			return Receipt{}, err
		}
		receipt.Title = TitleFromPrompt(turn.UserContent)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_sessions(session_id, title, provider, model, consent_version, created_ms, updated_ms)
			VALUES(?, ?, ?, ?, ?, ?, ?)
		`, receipt.SessionID, receipt.Title, turn.Provider, turn.Model, turn.ConsentVersion, now, now); err != nil {
			return Receipt{}, fmt.Errorf("create chat session: %w", err)
		}
	} else {
		if err := tx.QueryRowContext(ctx, `SELECT title FROM chat_sessions WHERE session_id = ?`, receipt.SessionID).
			Scan(&receipt.Title); errors.Is(err, sql.ErrNoRows) {
			return Receipt{}, ErrSessionNotFound
		} else if err != nil {
			return Receipt{}, fmt.Errorf("load chat session: %w", err)
		}
	}

	var turnIndex int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_turns WHERE session_id = ?`, receipt.SessionID).Scan(&turnIndex); err != nil {
		return Receipt{}, fmt.Errorf("count chat turns: %w", err)
	}
	receipt.TurnIndex = int(turnIndex)

	notices, err := encodeStrings(turn.Notices)
	if err != nil {
		return Receipt{}, fmt.Errorf("encode turn notices: %w", err)
	}
	turnResult, err := tx.ExecContext(ctx, `
		INSERT INTO chat_turns(
			session_id, turn_index, agent, provider, model, rounds, duration_ms,
			requests, input_tokens, output_tokens, cached_input_tokens, reasoning_tokens, total_tokens,
			route, source, period, timezone, notices_json, created_ms)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, receipt.SessionID, turnIndex, turn.Agent, turn.Provider, turn.Model, turn.Rounds, turn.DurationMS,
		turn.Usage.Requests, turn.Usage.InputTokens, turn.Usage.OutputTokens,
		turn.Usage.CachedInputTokens, turn.Usage.ReasoningTokens, turn.Usage.TotalTokens,
		turn.Context.Route, turn.Context.Source, turn.Context.Period, turn.Context.Timezone, notices, now)
	if err != nil {
		return Receipt{}, fmt.Errorf("persist chat turn: %w", err)
	}
	turnID, err := turnResult.LastInsertId()
	if err != nil {
		return Receipt{}, fmt.Errorf("persist chat turn: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chat_messages(session_id, turn_id, role, content, created_ms) VALUES(?, ?, 'user', ?, ?)
	`, receipt.SessionID, turnID, turn.UserContent, now); err != nil {
		return Receipt{}, fmt.Errorf("persist user message: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO chat_messages(session_id, turn_id, role, content, signature, model, agent, created_ms)
		VALUES(?, ?, 'assistant', ?, ?, ?, ?, ?)
	`, receipt.SessionID, turnID, turn.AssistantContent, turn.AssistantSignature, turn.Model, turn.Agent, now)
	if err != nil {
		return Receipt{}, fmt.Errorf("persist assistant message: %w", err)
	}
	assistantMessageID, err := result.LastInsertId()
	if err != nil {
		return Receipt{}, fmt.Errorf("persist assistant message: %w", err)
	}

	for index, call := range turn.ToolCalls {
		arguments := strings.TrimSpace(string(call.Arguments))
		if arguments == "" {
			arguments = "{}"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_tool_calls(
				message_id, session_id, call_index, call_ref, parent_call_ref, agent, round,
				tool_name, arguments_json, result_json, ok, duration_ms, created_ms)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, assistantMessageID, receipt.SessionID, index, call.CallRef, call.ParentCallRef, call.Agent, call.Round,
			call.Name, arguments, string(call.Result), boolToInt(call.OK), call.DurationMS, now); err != nil {
			return Receipt{}, fmt.Errorf("persist tool call: %w", err)
		}
	}

	for index, run := range turn.Subagents {
		toolsUsed, err := encodeStrings(run.ToolsUsed)
		if err != nil {
			return Receipt{}, fmt.Errorf("encode specialist tools: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_subagent_runs(
				message_id, session_id, run_index, call_ref, agent, title, task, status, report, error,
				rounds, tools_used_json, duration_ms,
				requests, input_tokens, output_tokens, cached_input_tokens, reasoning_tokens, total_tokens, created_ms)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, assistantMessageID, receipt.SessionID, index, run.CallRef, run.Agent, run.Title, run.Task,
			run.Status, run.Report, run.Error, run.Rounds, toolsUsed, run.DurationMS,
			run.Usage.Requests, run.Usage.InputTokens, run.Usage.OutputTokens,
			run.Usage.CachedInputTokens, run.Usage.ReasoningTokens, run.Usage.TotalTokens, now); err != nil {
			return Receipt{}, fmt.Errorf("persist specialist run: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE chat_sessions SET
			updated_ms = ?,
			provider = CASE WHEN ? <> '' THEN ? ELSE provider END,
			model = CASE WHEN ? <> '' THEN ? ELSE model END,
			consent_version = CASE WHEN ? <> '' THEN ? ELSE consent_version END,
			turn_count = turn_count + 1,
			tool_call_count = tool_call_count + ?,
			subagent_count = subagent_count + ?,
			duration_ms = duration_ms + ?,
			requests = requests + ?,
			input_tokens = input_tokens + ?,
			output_tokens = output_tokens + ?,
			cached_input_tokens = cached_input_tokens + ?,
			reasoning_tokens = reasoning_tokens + ?,
			total_tokens = total_tokens + ?
		WHERE session_id = ?
	`, now, turn.Provider, turn.Provider, turn.Model, turn.Model, turn.ConsentVersion, turn.ConsentVersion,
		len(turn.ToolCalls), len(turn.Subagents), turn.DurationMS,
		turn.Usage.Requests, turn.Usage.InputTokens, turn.Usage.OutputTokens,
		turn.Usage.CachedInputTokens, turn.Usage.ReasoningTokens, turn.Usage.TotalTokens,
		receipt.SessionID); err != nil {
		return Receipt{}, fmt.Errorf("update chat session: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT requests, input_tokens, output_tokens, cached_input_tokens, reasoning_tokens, total_tokens
		FROM chat_sessions WHERE session_id = ?
	`, receipt.SessionID).Scan(&receipt.Session.Requests, &receipt.Session.InputTokens, &receipt.Session.OutputTokens,
		&receipt.Session.CachedInputTokens, &receipt.Session.ReasoningTokens, &receipt.Session.TotalTokens); err != nil {
		return Receipt{}, fmt.Errorf("read chat session totals: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Receipt{}, fmt.Errorf("commit chat turn: %w", err)
	}
	return receipt, nil
}

const sessionColumns = `s.session_id, s.title, s.provider, s.model, s.consent_version,
	s.created_ms, s.updated_ms, s.turn_count, s.tool_call_count, s.subagent_count, s.duration_ms,
	s.requests, s.input_tokens, s.output_tokens, s.cached_input_tokens, s.reasoning_tokens, s.total_tokens`

func scanSession(scan func(...any) error, session *Session) error {
	return scan(&session.ID, &session.Title, &session.Provider, &session.Model, &session.ConsentVersion,
		&session.CreatedMS, &session.UpdatedMS, &session.TurnCount, &session.ToolCallCount,
		&session.SubagentCount, &session.DurationMS,
		&session.Usage.Requests, &session.Usage.InputTokens, &session.Usage.OutputTokens,
		&session.Usage.CachedInputTokens, &session.Usage.ReasoningTokens, &session.Usage.TotalTokens)
}

// ListSessions returns persisted sessions, most recently updated first.
func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = DefaultSessionListLimit
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+sessionColumns+`,
			(SELECT COUNT(*) FROM chat_messages m WHERE m.session_id = s.session_id)
		FROM chat_sessions s
		ORDER BY s.updated_ms DESC, s.session_id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		var session Session
		if err := scanSessionWithCount(rows.Scan, &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	return sessions, nil
}

func scanSessionWithCount(scan func(...any) error, session *Session) error {
	err := scan(&session.ID, &session.Title, &session.Provider, &session.Model, &session.ConsentVersion,
		&session.CreatedMS, &session.UpdatedMS, &session.TurnCount, &session.ToolCallCount,
		&session.SubagentCount, &session.DurationMS,
		&session.Usage.Requests, &session.Usage.InputTokens, &session.Usage.OutputTokens,
		&session.Usage.CachedInputTokens, &session.Usage.ReasoningTokens, &session.Usage.TotalTokens,
		&session.MessageCount)
	if err != nil {
		return fmt.Errorf("scan chat session: %w", err)
	}
	return nil
}

// GetSession loads one session with its ordered messages, and for every
// assistant message the turn metadata, tool calls, and specialist runs that
// produced it. The result is everything the browser needs to restore the
// conversation exactly as it was shown live.
func (s *Store) GetSession(ctx context.Context, id string) (*SessionDetail, error) {
	if !IsValidSessionID(id) {
		return nil, ErrSessionNotFound
	}
	detail := &SessionDetail{Messages: make([]Message, 0)}
	err := scanSession(func(targets ...any) error {
		return s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM chat_sessions s WHERE s.session_id = ?`, id).Scan(targets...)
	}, &detail.Session)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load chat session: %w", err)
	}

	byID, err := s.loadMessages(ctx, id, detail)
	if err != nil {
		return nil, err
	}
	detail.Session.MessageCount = int64(len(detail.Messages))
	if err := s.loadToolCalls(ctx, id, detail, byID); err != nil {
		return nil, err
	}
	return detail, s.loadSubagentRuns(ctx, id, detail, byID)
}

func (s *Store) loadMessages(ctx context.Context, id string, detail *SessionDetail) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.message_id, m.role, m.content, m.signature, m.model, m.agent, m.created_ms,
			t.turn_index, t.rounds, t.duration_ms,
			t.requests, t.input_tokens, t.output_tokens, t.cached_input_tokens, t.reasoning_tokens, t.total_tokens,
			t.route, t.source, t.period, t.timezone, t.notices_json
		FROM chat_messages m
		JOIN chat_turns t ON t.turn_id = m.turn_id
		WHERE m.session_id = ? ORDER BY m.message_id
	`, id)
	if err != nil {
		return nil, fmt.Errorf("load chat messages: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]int)
	for rows.Next() {
		var message Message
		var usage Usage
		var turnContext TurnContext
		var notices string
		if err := rows.Scan(&message.ID, &message.Role, &message.Content, &message.Signature,
			&message.Model, &message.Agent, &message.CreatedMS,
			&message.TurnIndex, &message.Rounds, &message.DurationMS,
			&usage.Requests, &usage.InputTokens, &usage.OutputTokens,
			&usage.CachedInputTokens, &usage.ReasoningTokens, &usage.TotalTokens,
			&turnContext.Route, &turnContext.Source, &turnContext.Period, &turnContext.Timezone, &notices); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		// Turn-level accounting belongs to the answer, not to the prompt.
		if message.Role == "assistant" {
			message.Usage = &usage
			message.Notices = decodeStrings(notices)
		} else {
			message.Rounds = 0
			message.DurationMS = 0
		}
		if turnContext != (TurnContext{}) {
			message.Context = &turnContext
		}
		byID[message.ID] = len(detail.Messages)
		detail.Messages = append(detail.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load chat messages: %w", err)
	}
	return byID, nil
}

func (s *Store) loadToolCalls(ctx context.Context, id string, detail *SessionDetail, byID map[int64]int) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, call_index, call_ref, parent_call_ref, agent, round,
			tool_name, arguments_json, result_json, ok, duration_ms
		FROM chat_tool_calls WHERE session_id = ? ORDER BY message_id, call_index
	`, id)
	if err != nil {
		return fmt.Errorf("load chat tool calls: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var messageID int64
		var call ToolCall
		var arguments, result string
		var ok int
		if err := rows.Scan(&messageID, &call.Index, &call.CallRef, &call.ParentCallRef, &call.Agent,
			&call.Round, &call.Name, &arguments, &result, &ok, &call.DurationMS); err != nil {
			return fmt.Errorf("scan chat tool call: %w", err)
		}
		call.OK = ok != 0
		call.Arguments = safeRawJSON(arguments)
		call.Result = safeRawJSON(result)
		if index, found := byID[messageID]; found {
			detail.Messages[index].ToolCalls = append(detail.Messages[index].ToolCalls, call)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load chat tool calls: %w", err)
	}
	return nil
}

func (s *Store) loadSubagentRuns(ctx context.Context, id string, detail *SessionDetail, byID map[int64]int) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, run_index, call_ref, agent, title, task, status, report, error,
			rounds, tools_used_json, duration_ms,
			requests, input_tokens, output_tokens, cached_input_tokens, reasoning_tokens, total_tokens
		FROM chat_subagent_runs WHERE session_id = ? ORDER BY message_id, run_index
	`, id)
	if err != nil {
		return fmt.Errorf("load specialist runs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var messageID int64
		var run SubagentRun
		var toolsUsed string
		if err := rows.Scan(&messageID, &run.Index, &run.CallRef, &run.Agent, &run.Title, &run.Task,
			&run.Status, &run.Report, &run.Error, &run.Rounds, &toolsUsed, &run.DurationMS,
			&run.Usage.Requests, &run.Usage.InputTokens, &run.Usage.OutputTokens,
			&run.Usage.CachedInputTokens, &run.Usage.ReasoningTokens, &run.Usage.TotalTokens); err != nil {
			return fmt.Errorf("scan specialist run: %w", err)
		}
		run.ToolsUsed = decodeStrings(toolsUsed)
		if index, found := byID[messageID]; found {
			detail.Messages[index].Subagents = append(detail.Messages[index].Subagents, run)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load specialist runs: %w", err)
	}
	return nil
}

// DeleteSession removes a session with its turns, messages, tool calls, and
// specialist runs, reporting whether anything was deleted.
func (s *Store) DeleteSession(ctx context.Context, id string) (bool, error) {
	if !IsValidSessionID(id) {
		return false, nil
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM chat_sessions WHERE session_id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete chat session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete chat session: %w", err)
	}
	return affected > 0, nil
}

func encodeStrings(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// decodeStrings tolerates a malformed row rather than failing a whole session
// load for a value that is only ever presentational.
func decodeStrings(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err != nil {
		return nil
	}
	return values
}

// safeRawJSON returns value as raw JSON when it is valid, and a JSON string
// fallback otherwise, so one malformed row cannot break session responses.
func safeRawJSON(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
