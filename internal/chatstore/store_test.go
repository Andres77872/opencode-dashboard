package chatstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "assistant-chat.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func appendTurn(t *testing.T, store *Store, turn Turn) Receipt {
	t.Helper()
	receipt, err := store.AppendTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	return receipt
}

func TestHistoryKeyIsStableAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "assistant-chat.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first, err := store.HistoryKey(ctx)
	if err != nil {
		t.Fatalf("HistoryKey: %v", err)
	}
	if len(first) != historyKeyBytes {
		t.Fatalf("history key length = %d, want %d", len(first), historyKeyBytes)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if reopened.Recreated() {
		t.Fatal("reopening the current schema discarded history")
	}
	second, err := reopened.HistoryKey(ctx)
	if err != nil {
		t.Fatalf("HistoryKey after reopen: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("history key changed across reopen")
	}
}

func TestAppendTurnPersistsToolCallsSpecialistsAndAccounting(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	receipt := appendTurn(t, store, Turn{
		Provider:           "minimax",
		Model:              "MiniMax-M3",
		Agent:              "analyst",
		ConsentVersion:     "analytics-assistant-v2",
		Context:            TurnContext{Route: "/models", Source: "opencode", Period: "7d", Timezone: "America/Mexico_City"},
		UserContent:        "How much did I spend this week on every source?",
		AssistantContent:   "Here is the report.",
		AssistantSignature: "sig-1",
		Rounds:             3,
		DurationMS:         4200,
		Usage:              Usage{Requests: 3, InputTokens: 2000, OutputTokens: 400, CachedInputTokens: 1500, ReasoningTokens: 90, TotalTokens: 2400},
		Notices:            []string{"Cost scope: source costs are not additive."},
		ToolCalls: []ToolCall{
			{Name: "list_sources", CallRef: "tool-1", Agent: "analyst", Round: 1, Arguments: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true,"data":[]}`), OK: true, DurationMS: 12},
			{Name: "get_overview", CallRef: "tool-2", ParentCallRef: "tool-1", Agent: "cost_auditor", Round: 2, Arguments: json.RawMessage(`{"source":"opencode"}`), Result: json.RawMessage(`{"ok":false,"error":{"code":"tool_failed","message":"x"}}`), OK: false, DurationMS: 40},
		},
		Subagents: []SubagentRun{{
			CallRef: "tool-1", Agent: "cost_auditor", Title: "Cost auditor",
			Task: "Audit opencode cost provenance for the last 7 days.", Status: "complete",
			Report: "Cost is reported, not estimated.", Rounds: 2,
			ToolsUsed: []string{"get_overview", "get_model_usage"}, DurationMS: 900,
			Usage: Usage{Requests: 2, InputTokens: 700, OutputTokens: 120, TotalTokens: 820},
		}},
	})
	if !IsValidSessionID(receipt.SessionID) {
		t.Fatalf("AppendTurn returned invalid session id %q", receipt.SessionID)
	}
	if receipt.TurnIndex != 0 || receipt.Title != "How much did I spend this week on every source?" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.Session.TotalTokens != 2400 || receipt.Session.Requests != 3 {
		t.Fatalf("receipt session usage = %#v", receipt.Session)
	}

	detail, err := store.GetSession(ctx, receipt.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	session := detail.Session
	if session.Model != "MiniMax-M3" || session.Provider != "minimax" || session.ConsentVersion != "analytics-assistant-v2" {
		t.Fatalf("session identity = %#v", session)
	}
	if session.TurnCount != 1 || session.ToolCallCount != 2 || session.SubagentCount != 1 || session.DurationMS != 4200 {
		t.Fatalf("session counters = %#v", session)
	}
	if session.Usage != (Usage{Requests: 3, InputTokens: 2000, OutputTokens: 400, CachedInputTokens: 1500, ReasoningTokens: 90, TotalTokens: 2400}) {
		t.Fatalf("session usage = %#v", session.Usage)
	}
	if len(detail.Messages) != 2 || detail.Messages[0].Role != "user" || detail.Messages[1].Role != "assistant" {
		t.Fatalf("messages = %#v", detail.Messages)
	}

	prompt := detail.Messages[0]
	if prompt.Usage != nil || prompt.Rounds != 0 {
		t.Fatalf("turn accounting was attached to the prompt: %#v", prompt)
	}
	if prompt.Context == nil || prompt.Context.Route != "/models" {
		t.Fatalf("prompt context = %#v", prompt.Context)
	}

	answer := detail.Messages[1]
	if answer.Signature != "sig-1" || answer.Agent != "analyst" || answer.Rounds != 3 || answer.DurationMS != 4200 {
		t.Fatalf("assistant message = %#v", answer)
	}
	if answer.Usage == nil || answer.Usage.CachedInputTokens != 1500 || answer.Usage.ReasoningTokens != 90 {
		t.Fatalf("assistant usage = %#v", answer.Usage)
	}
	if len(answer.Notices) != 1 {
		t.Fatalf("assistant notices = %#v", answer.Notices)
	}

	calls := answer.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(calls))
	}
	if calls[0].Name != "list_sources" || !calls[0].OK || calls[0].DurationMS != 12 || calls[0].CallRef != "tool-1" {
		t.Fatalf("first call = %+v", calls[0])
	}
	if calls[1].Agent != "cost_auditor" || calls[1].ParentCallRef != "tool-1" || calls[1].Round != 2 || calls[1].OK {
		t.Fatalf("second call = %+v", calls[1])
	}
	if string(calls[1].Arguments) != `{"source":"opencode"}` || !strings.Contains(string(calls[1].Result), `"tool_failed"`) {
		t.Fatalf("second call payloads = %s / %s", calls[1].Arguments, calls[1].Result)
	}

	runs := answer.Subagents
	if len(runs) != 1 {
		t.Fatalf("specialist runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.Agent != "cost_auditor" || run.Status != "complete" || run.Report == "" || run.Rounds != 2 {
		t.Fatalf("specialist run = %+v", run)
	}
	if len(run.ToolsUsed) != 2 || run.Usage.TotalTokens != 820 || run.DurationMS != 900 {
		t.Fatalf("specialist accounting = %+v", run)
	}
}

func TestAppendTurnToExistingAndMissingSession(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	first := appendTurn(t, store, Turn{
		UserContent: "first", AssistantContent: "answer one", AssistantSignature: "s1",
		Usage: Usage{Requests: 1, TotalTokens: 100}, ToolCalls: []ToolCall{{Name: "list_sources"}},
	})
	second := appendTurn(t, store, Turn{
		SessionID: first.SessionID, UserContent: "second", AssistantContent: "answer two", AssistantSignature: "s2",
		Usage: Usage{Requests: 2, TotalTokens: 50},
	})
	if second.SessionID != first.SessionID {
		t.Fatalf("continued session id = %q, want %q", second.SessionID, first.SessionID)
	}
	if second.TurnIndex != 1 {
		t.Fatalf("turn index = %d, want 1", second.TurnIndex)
	}
	if second.Session.TotalTokens != 150 || second.Session.Requests != 3 {
		t.Fatalf("running session usage = %#v", second.Session)
	}

	detail, err := store.GetSession(ctx, first.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if len(detail.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(detail.Messages))
	}
	if detail.Messages[3].TurnIndex != 1 || detail.Messages[1].TurnIndex != 0 {
		t.Fatalf("turn indexes = %d, %d", detail.Messages[1].TurnIndex, detail.Messages[3].TurnIndex)
	}
	// Tool calls belong to the answer they produced, not to the whole session.
	if len(detail.Messages[1].ToolCalls) != 1 || len(detail.Messages[3].ToolCalls) != 0 {
		t.Fatalf("tool calls were not scoped to their turn: %#v", detail.Messages)
	}

	if _, err := store.AppendTurn(ctx, Turn{
		SessionID: "cs_00000000000000000000000000000000", UserContent: "x", AssistantContent: "y",
	}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v, want ErrSessionNotFound", err)
	}
}

func TestAppendTurnRequiresBothSides(t *testing.T) {
	store := openTestStore(t)
	for _, turn := range []Turn{
		{AssistantContent: "answer"},
		{UserContent: "question"},
		{UserContent: "  ", AssistantContent: "  "},
	} {
		if _, err := store.AppendTurn(context.Background(), turn); !errors.Is(err, ErrInvalidTurn) {
			t.Errorf("AppendTurn(%#v) error = %v, want ErrInvalidTurn", turn, err)
		}
	}
}

func TestListAndDeleteSessions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	first := appendTurn(t, store, Turn{
		UserContent: "alpha question", AssistantContent: "alpha answer",
		Usage: Usage{Requests: 1, TotalTokens: 42}, Subagents: []SubagentRun{{Agent: "trend_analyst"}},
	})
	second := appendTurn(t, store, Turn{UserContent: "beta question", AssistantContent: "beta answer"})

	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	for _, session := range sessions {
		if session.MessageCount != 2 || session.TurnCount != 1 {
			t.Fatalf("session %q counters = %#v", session.ID, session)
		}
		if session.ID == first.SessionID && (session.Usage.TotalTokens != 42 || session.SubagentCount != 1) {
			t.Fatalf("listed usage = %#v", session)
		}
	}

	deleted, err := store.DeleteSession(ctx, first.SessionID)
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteSession reported nothing deleted")
	}
	if _, err := store.GetSession(ctx, first.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession after delete = %v, want ErrSessionNotFound", err)
	}
	// Cascading deletes must not leave orphaned turns, tool calls, or runs.
	for _, table := range []string{"chat_turns", "chat_messages", "chat_tool_calls", "chat_subagent_runs"} {
		var remaining int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE session_id = ?`, first.SessionID).Scan(&remaining); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if remaining != 0 {
			t.Fatalf("%s kept %d rows after the session was deleted", table, remaining)
		}
	}

	remaining, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != second.SessionID {
		t.Fatalf("unexpected remaining sessions %+v", remaining)
	}

	deleted, err = store.DeleteSession(ctx, "not-a-session")
	if err != nil || deleted {
		t.Fatalf("DeleteSession invalid id = (%v, %v), want (false, nil)", deleted, err)
	}
}

func TestTitleFromPrompt(t *testing.T) {
	if got := TitleFromPrompt("  many \n words   here "); got != "many words here" {
		t.Fatalf("TitleFromPrompt = %q", got)
	}
	if got := TitleFromPrompt(""); got != "New conversation" {
		t.Fatalf("TitleFromPrompt empty = %q", got)
	}
	long := strings.Repeat("privacy ", 40)
	got := TitleFromPrompt(long)
	if len([]rune(got)) > MaxTitleLength {
		t.Fatalf("TitleFromPrompt length = %d, want <= %d", len([]rune(got)), MaxTitleLength)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("TitleFromPrompt long = %q, want ellipsis suffix", got)
	}
}

func TestOpenRefusesForeignDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "other.sqlite")
	foreign, err := openDB(ctx, path)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	if _, err := foreign.ExecContext(ctx, `CREATE TABLE something_else (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create foreign table: %v", err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatalf("close foreign db: %v", err)
	}
	if _, err := Open(ctx, path); err == nil || !strings.Contains(err.Error(), "not an assistant chat database") {
		t.Fatalf("Open foreign db error = %v, want refusal", err)
	}
}

// Assistant history is intentionally not migrated: a database written by any
// other version of this store is rebuilt empty rather than upgraded.
func TestOpenRebuildsAnUnrecognizedSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "assistant-chat.sqlite")

	previous, err := openDB(ctx, path)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	if _, err := previous.ExecContext(ctx, `
		CREATE TABLE chat_meta (key TEXT PRIMARY KEY, value BLOB NOT NULL);
		CREATE TABLE chat_sessions (session_id TEXT PRIMARY KEY, legacy_column TEXT);
		INSERT INTO chat_sessions(session_id, legacy_column) VALUES('cs_old', 'stale');
		PRAGMA user_version = 1;
	`); err != nil {
		t.Fatalf("create previous schema: %v", err)
	}
	if err := previous.Close(); err != nil {
		t.Fatalf("close previous db: %v", err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if !store.Recreated() {
		t.Fatal("Recreated() = false, want a reported rebuild")
	}

	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("rebuilt store kept %d sessions", len(sessions))
	}
	var version int
	if err := store.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
	// The rebuilt store must be fully usable, not merely empty.
	receipt := appendTurn(t, store, Turn{UserContent: "after rebuild", AssistantContent: "answer"})
	if _, err := store.GetSession(ctx, receipt.SessionID); err != nil {
		t.Fatalf("GetSession after rebuild: %v", err)
	}
}

func TestGetSessionToleratesMalformedStoredJSON(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	receipt := appendTurn(t, store, Turn{
		UserContent: "question", AssistantContent: "answer",
		ToolCalls: []ToolCall{{Name: "list_sources", Arguments: json.RawMessage(`{}`)}},
		Subagents: []SubagentRun{{Agent: "trend_analyst", ToolsUsed: []string{"get_daily_usage"}}},
	})
	for _, statement := range []string{
		`UPDATE chat_tool_calls SET result_json = 'not json'`,
		`UPDATE chat_subagent_runs SET tools_used_json = 'not json'`,
		`UPDATE chat_turns SET notices_json = 'not json'`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("corrupt row: %v", err)
		}
	}

	detail, err := store.GetSession(ctx, receipt.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	answer := detail.Messages[1]
	if len(answer.ToolCalls) != 1 || !json.Valid(answer.ToolCalls[0].Result) {
		t.Fatalf("malformed tool result was not made safe: %#v", answer.ToolCalls)
	}
	if len(answer.Subagents) != 1 || answer.Subagents[0].ToolsUsed != nil || answer.Notices != nil {
		t.Fatalf("malformed lists were not dropped: %#v", answer.Subagents)
	}
}

func TestSessionExistsRejectsMalformedIDsWithoutQuerying(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	receipt := appendTurn(t, store, Turn{UserContent: "question", AssistantContent: "answer"})

	exists, err := store.SessionExists(ctx, receipt.SessionID)
	if err != nil || !exists {
		t.Fatalf("SessionExists(existing) = (%v, %v)", exists, err)
	}
	for _, id := range []string{"", "cs_short", "cs_00000000000000000000000000000000", "'; DROP TABLE chat_sessions; --"} {
		exists, err := store.SessionExists(ctx, id)
		if err != nil || exists {
			t.Errorf("SessionExists(%q) = (%v, %v), want (false, nil)", id, exists, err)
		}
	}
	var tables int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_sessions'`).Scan(&tables); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("inspect tables: %v", err)
	}
	if tables != 1 {
		t.Fatal("the sessions table did not survive a hostile identifier")
	}
}
