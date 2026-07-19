// Package chatstore owns the durable SQLite store for analytics-assistant
// chat sessions. It is deliberately separate from the usage cache: the cache
// holds rebuildable derived data and is dropped on schema changes, while chat
// history is user data that must survive restarts and cache rebuilds.
//
// The store persists the browser-visible conversation plus the assistant's own
// analytics tool invocations (name, input arguments, output envelope, status,
// duration). Everything stored here is data the local user already saw in the
// dashboard UI; source transcripts are never written to this database.
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

// schemaVersion is stamped into PRAGMA user_version. Unlike the usage cache,
// this database holds durable user data: an unknown (newer) version is
// refused, never deleted.
const schemaVersion = 1

const (
	busyTimeout = 5 * time.Second

	historyKeyMetaName = "history_key_v1"
	historyKeyBytes    = 32

	sessionIDPrefix = "cs_"

	// MaxTitleLength bounds stored session titles; longer titles are trimmed
	// at a rune boundary with an ellipsis.
	MaxTitleLength = 80
)

var (
	ErrSessionNotFound = errors.New("assistant chat session not found")
	ErrInvalidTurn     = errors.New("invalid assistant chat turn")
)

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
	created_ms INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_messages (
	message_id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES chat_sessions(session_id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	signature TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_tool_calls (
	tool_call_id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL REFERENCES chat_messages(message_id) ON DELETE CASCADE,
	session_id TEXT NOT NULL,
	call_index INTEGER NOT NULL,
	tool_name TEXT NOT NULL,
	arguments_json TEXT NOT NULL DEFAULT '{}',
	result_json TEXT NOT NULL DEFAULT '',
	ok INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id, message_id);
CREATE INDEX IF NOT EXISTS idx_chat_tool_calls_message ON chat_tool_calls(message_id, call_index);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_updated ON chat_sessions(updated_ms);
`

type Store struct {
	db   *sql.DB
	path string
}

// Session is one persisted assistant conversation.
type Session struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	CreatedMS    int64  `json:"created_ms"`
	UpdatedMS    int64  `json:"updated_ms"`
	MessageCount int64  `json:"message_count"`
}

// ToolCall is one analytics tool invocation made while producing an assistant
// message, including the exact input arguments and output envelope that were
// exchanged with the tool.
type ToolCall struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	OK         bool            `json:"ok"`
	DurationMS int64           `json:"duration_ms"`
}

// Message is one persisted chat message. Assistant messages may carry the tool
// calls that produced them and the HMAC signature the stateless chat protocol
// requires to replay them.
type Message struct {
	ID        int64      `json:"id"`
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Signature string     `json:"signature,omitempty"`
	Model     string     `json:"model,omitempty"`
	CreatedMS int64      `json:"created_ms"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
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
	UserContent        string
	AssistantContent   string
	AssistantSignature string
	ToolCalls          []ToolCall
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
	if err := ensureSchema(ctx, db, path); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
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

func ensureSchema(ctx context.Context, db *sql.DB, path string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin chat store schema check: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version, tables, chatTables int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read chat store schema version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect chat store: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'chat_sessions'`).Scan(&chatTables); err != nil {
		return fmt.Errorf("inspect chat store: %w", err)
	}
	switch {
	case tables > 0 && chatTables == 0:
		return fmt.Errorf("%s is not an assistant chat database; refusing to modify it", path)
	case version > schemaVersion:
		return fmt.Errorf("%s uses a newer chat schema (%d) than this binary supports (%d)", path, version, schemaVersion)
	}
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create chat store schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("record chat store schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit chat store schema: %w", err)
	}
	return nil
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

// AppendTurn persists one completed exchange atomically and returns the
// session id (newly created when turn.SessionID is empty).
func (s *Store) AppendTurn(ctx context.Context, turn Turn) (string, error) {
	if strings.TrimSpace(turn.UserContent) == "" {
		return "", fmt.Errorf("%w: user content is required", ErrInvalidTurn)
	}
	if strings.TrimSpace(turn.AssistantContent) == "" {
		return "", fmt.Errorf("%w: assistant content is required", ErrInvalidTurn)
	}
	now := time.Now().UnixMilli()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin chat turn: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	sessionID := turn.SessionID
	if sessionID == "" {
		sessionID, err = newSessionID()
		if err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_sessions(session_id, title, provider, model, created_ms, updated_ms)
			VALUES(?, ?, ?, ?, ?, ?)
		`, sessionID, TitleFromPrompt(turn.UserContent), turn.Provider, turn.Model, now, now); err != nil {
			return "", fmt.Errorf("create chat session: %w", err)
		}
	} else {
		result, err := tx.ExecContext(ctx, `
			UPDATE chat_sessions SET updated_ms = ?, provider = ?, model = ? WHERE session_id = ?
		`, now, turn.Provider, turn.Model, sessionID)
		if err != nil {
			return "", fmt.Errorf("update chat session: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return "", fmt.Errorf("update chat session: %w", err)
		}
		if affected == 0 {
			return "", ErrSessionNotFound
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chat_messages(session_id, role, content, created_ms) VALUES(?, 'user', ?, ?)
	`, sessionID, turn.UserContent, now); err != nil {
		return "", fmt.Errorf("persist user message: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO chat_messages(session_id, role, content, signature, model, created_ms)
		VALUES(?, 'assistant', ?, ?, ?, ?)
	`, sessionID, turn.AssistantContent, turn.AssistantSignature, turn.Model, now)
	if err != nil {
		return "", fmt.Errorf("persist assistant message: %w", err)
	}
	assistantMessageID, err := result.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("persist assistant message: %w", err)
	}
	for index, call := range turn.ToolCalls {
		arguments := string(call.Arguments)
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_tool_calls(message_id, session_id, call_index, tool_name, arguments_json, result_json, ok, duration_ms, created_ms)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, assistantMessageID, sessionID, index, call.Name, arguments, string(call.Result), boolToInt(call.OK), call.DurationMS, now); err != nil {
			return "", fmt.Errorf("persist tool call: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit chat turn: %w", err)
	}
	return sessionID, nil
}

// ListSessions returns persisted sessions, most recently updated first.
func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.session_id, s.title, s.provider, s.model, s.created_ms, s.updated_ms,
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
		if err := rows.Scan(&session.ID, &session.Title, &session.Provider, &session.Model,
			&session.CreatedMS, &session.UpdatedMS, &session.MessageCount); err != nil {
			return nil, fmt.Errorf("scan chat session: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	return sessions, nil
}

// GetSession loads one session with its ordered messages and tool calls.
func (s *Store) GetSession(ctx context.Context, id string) (*SessionDetail, error) {
	if !IsValidSessionID(id) {
		return nil, ErrSessionNotFound
	}
	detail := &SessionDetail{}
	err := s.db.QueryRowContext(ctx, `
		SELECT session_id, title, provider, model, created_ms, updated_ms
		FROM chat_sessions WHERE session_id = ?
	`, id).Scan(&detail.Session.ID, &detail.Session.Title, &detail.Session.Provider,
		&detail.Session.Model, &detail.Session.CreatedMS, &detail.Session.UpdatedMS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load chat session: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, role, content, signature, model, created_ms
		FROM chat_messages WHERE session_id = ? ORDER BY message_id
	`, id)
	if err != nil {
		return nil, fmt.Errorf("load chat messages: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]int)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.Role, &message.Content,
			&message.Signature, &message.Model, &message.CreatedMS); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		byID[message.ID] = len(detail.Messages)
		detail.Messages = append(detail.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load chat messages: %w", err)
	}
	detail.Session.MessageCount = int64(len(detail.Messages))

	toolRows, err := s.db.QueryContext(ctx, `
		SELECT message_id, call_index, tool_name, arguments_json, result_json, ok, duration_ms
		FROM chat_tool_calls WHERE session_id = ? ORDER BY message_id, call_index
	`, id)
	if err != nil {
		return nil, fmt.Errorf("load chat tool calls: %w", err)
	}
	defer toolRows.Close()

	for toolRows.Next() {
		var messageID int64
		var call ToolCall
		var arguments, result string
		var ok int
		if err := toolRows.Scan(&messageID, &call.Index, &call.Name, &arguments, &result, &ok, &call.DurationMS); err != nil {
			return nil, fmt.Errorf("scan chat tool call: %w", err)
		}
		call.OK = ok != 0
		call.Arguments = safeRawJSON(arguments)
		call.Result = safeRawJSON(result)
		if index, found := byID[messageID]; found {
			detail.Messages[index].ToolCalls = append(detail.Messages[index].ToolCalls, call)
		}
	}
	if err := toolRows.Err(); err != nil {
		return nil, fmt.Errorf("load chat tool calls: %w", err)
	}
	return detail, nil
}

// DeleteSession removes a session with its messages and tool calls, reporting
// whether anything was deleted.
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
	return json.RawMessage(encoded)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
