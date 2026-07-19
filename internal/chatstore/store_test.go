package chatstore

import (
	"context"
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
	second, err := reopened.HistoryKey(ctx)
	if err != nil {
		t.Fatalf("HistoryKey after reopen: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("history key changed across reopen")
	}
}

func TestAppendTurnCreatesSessionWithToolCalls(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	sessionID, err := store.AppendTurn(ctx, Turn{
		Provider:           "minimax",
		Model:              "MiniMax-M3",
		UserContent:        "How much did I spend this week on every source?",
		AssistantContent:   "Here is the report.",
		AssistantSignature: "sig-1",
		ToolCalls: []ToolCall{
			{Name: "list_sources", Arguments: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true,"data":[]}`), OK: true, DurationMS: 12},
			{Name: "get_overview", Arguments: json.RawMessage(`{"source":"opencode"}`), Result: json.RawMessage(`{"ok":false,"error":{"code":"tool_failed","message":"x"}}`), OK: false, DurationMS: 40},
		},
	})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	if !IsValidSessionID(sessionID) {
		t.Fatalf("AppendTurn returned invalid session id %q", sessionID)
	}

	detail, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if detail.Session.Title != "How much did I spend this week on every source?" {
		t.Fatalf("unexpected title %q", detail.Session.Title)
	}
	if detail.Session.Model != "MiniMax-M3" || detail.Session.Provider != "minimax" {
		t.Fatalf("unexpected provider/model %q/%q", detail.Session.Provider, detail.Session.Model)
	}
	if len(detail.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(detail.Messages))
	}
	if detail.Messages[0].Role != "user" || detail.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected roles %q, %q", detail.Messages[0].Role, detail.Messages[1].Role)
	}
	if detail.Messages[1].Signature != "sig-1" {
		t.Fatalf("assistant signature = %q", detail.Messages[1].Signature)
	}
	calls := detail.Messages[1].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(calls))
	}
	if calls[0].Name != "list_sources" || !calls[0].OK || calls[0].DurationMS != 12 {
		t.Fatalf("unexpected first call %+v", calls[0])
	}
	if calls[1].Name != "get_overview" || calls[1].OK {
		t.Fatalf("unexpected second call %+v", calls[1])
	}
	if string(calls[1].Arguments) != `{"source":"opencode"}` {
		t.Fatalf("unexpected arguments %s", calls[1].Arguments)
	}
	if !strings.Contains(string(calls[1].Result), `"tool_failed"`) {
		t.Fatalf("unexpected result %s", calls[1].Result)
	}
}

func TestAppendTurnToExistingAndMissingSession(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	sessionID, err := store.AppendTurn(ctx, Turn{
		UserContent: "first", AssistantContent: "answer one", AssistantSignature: "s1",
	})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	returned, err := store.AppendTurn(ctx, Turn{
		SessionID: sessionID, UserContent: "second", AssistantContent: "answer two", AssistantSignature: "s2",
	})
	if err != nil {
		t.Fatalf("AppendTurn continue: %v", err)
	}
	if returned != sessionID {
		t.Fatalf("continued session id = %q, want %q", returned, sessionID)
	}
	detail, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if len(detail.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(detail.Messages))
	}

	if _, err := store.AppendTurn(ctx, Turn{
		SessionID: "cs_00000000000000000000000000000000", UserContent: "x", AssistantContent: "y",
	}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v, want ErrSessionNotFound", err)
	}
}

func TestListAndDeleteSessions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	first, err := store.AppendTurn(ctx, Turn{UserContent: "alpha question", AssistantContent: "alpha answer"})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	second, err := store.AppendTurn(ctx, Turn{UserContent: "beta question", AssistantContent: "beta answer"})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	for _, session := range sessions {
		if session.MessageCount != 2 {
			t.Fatalf("session %q message count = %d, want 2", session.ID, session.MessageCount)
		}
	}

	deleted, err := store.DeleteSession(ctx, first)
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteSession reported nothing deleted")
	}
	if _, err := store.GetSession(ctx, first); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession after delete = %v, want ErrSessionNotFound", err)
	}
	remaining, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != second {
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
