package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// scopeFixture is two sessions in one project; the first has a subagent
// transcript, which lives below the session's own directory.
func scopeFixture(t *testing.T) *Source {
	t.Helper()
	return newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-scope/main-session.jsonl": {
			`{"type":"user","uuid":"main-user-1","sessionId":"main-session","timestamp":"2026-05-01T10:00:00Z","cwd":"/home/andres/projects/scope","message":{"role":"user","content":"Investigate the repo using a subagent."}}`,
			`{"type":"assistant","uuid":"main-assistant-1","sessionId":"main-session","requestId":"req_main_1","timestamp":"2026-05-01T10:00:01Z","cwd":"/home/andres/projects/scope","message":{"id":"msg_main_1","role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Here is the answer."}],"usage":{"input_tokens":1000,"output_tokens":200}}}`,
		},
		"-home-andres-projects-scope/main-session/subagents/agent-explore.jsonl": {
			`{"type":"user","uuid":"sub-user-1","sessionId":"main-session","isSidechain":true,"agentId":"agent-explore-1","timestamp":"2026-05-01T10:00:05Z","cwd":"/home/andres/projects/scope","message":{"role":"user","content":"Explore the codebase."}}`,
			`{"type":"assistant","uuid":"sub-assistant-1","sessionId":"main-session","isSidechain":true,"attributionAgent":"Explore","requestId":"req_sub_1","timestamp":"2026-05-01T10:00:06Z","cwd":"/home/andres/projects/scope","message":{"id":"msg_sub_1","role":"assistant","model":"claude-test-approx","content":[{"type":"text","text":"The repo has packages a and b."}],"usage":{"input_tokens":500,"output_tokens":100}}}`,
		},
		"-home-andres-projects-scope/other-session.jsonl": {
			`{"type":"user","uuid":"other-user-1","sessionId":"other-session","timestamp":"2026-05-02T09:00:00Z","cwd":"/home/andres/projects/scope","message":{"role":"user","content":"Unrelated question."}}`,
			`{"type":"assistant","uuid":"other-assistant-1","sessionId":"other-session","requestId":"req_other_1","timestamp":"2026-05-02T09:00:01Z","cwd":"/home/andres/projects/scope","message":{"id":"msg_other_1","role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Unrelated answer."}],"usage":{"input_tokens":40,"output_tokens":10}}}`,
		},
	})
}

func fullSnapshotLoaded(src *Source) bool {
	src.mu.Lock()
	defer src.mu.Unlock()
	return src.snapshot != nil
}

// A session detail must not cost a parse of the whole history: on a large
// Claude home that parse outlasts the request and every lookup fails.
func TestSessionByIDReadsOnlyThatSessionsTranscripts(t *testing.T) {
	src := scopeFixture(t)
	ctx := testContext(t)

	scoped, err := src.SessionByID(ctx, "main-session")
	if err != nil || scoped == nil {
		t.Fatalf("SessionByID(main-session) = %+v, %v; want the session", scoped, err)
	}
	if fullSnapshotLoaded(src) {
		t.Fatal("SessionByID loaded the full snapshot; want only the session's own transcripts")
	}
	if scoped.MessageCount != 4 {
		t.Errorf("MessageCount = %d, want 4 (2 main rows + 2 subagent rows)", scoped.MessageCount)
	}
	var subagentRows int
	for _, msg := range scoped.Messages {
		if msg.IsSubagent {
			subagentRows++
		}
		if msg.ID == synthesizeMessageID("other-session", "other-assistant-1", 0) {
			t.Errorf("session detail contains a message of another session: %s", msg.ID)
		}
	}
	if subagentRows != 2 {
		t.Errorf("subagent rows = %d, want 2 (the transcript below <session>/subagents is part of the session)", subagentRows)
	}

	// The scoped read must agree with what the full history reports.
	if _, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"}); err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if !fullSnapshotLoaded(src) {
		t.Fatal("Overview(all) did not load the full snapshot; the comparison below would be vacuous")
	}
	full, err := src.SessionByID(ctx, "main-session")
	if err != nil || full == nil {
		t.Fatalf("SessionByID(main-session) on the full snapshot = %+v, %v", full, err)
	}
	if full.MessageCount != scoped.MessageCount || !approxEqual(full.TotalCost, scoped.TotalCost) || full.TotalTokens != scoped.TotalTokens || full.Title != scoped.Title {
		t.Errorf("scoped detail differs from the full snapshot:\n scoped %d msgs, cost %.6f, tokens %+v, title %q\n full   %d msgs, cost %.6f, tokens %+v, title %q",
			scoped.MessageCount, scoped.TotalCost, scoped.TotalTokens, scoped.Title, full.MessageCount, full.TotalCost, full.TotalTokens, full.Title)
	}
}

func TestMessageByIDReadsTheMessagesSessionOnly(t *testing.T) {
	src := scopeFixture(t)
	ctx := testContext(t)

	id := synthesizeMessageID("main-session", "sub-assistant-1", 0)
	detail, err := src.MessageByID(ctx, id)
	if err != nil || detail == nil {
		t.Fatalf("MessageByID(%s) = %+v, %v; want the subagent message", id, detail, err)
	}
	if detail.SessionID != "main-session" || !detail.IsSubagent {
		t.Errorf("MessageByID returned session %q, subagent=%v; want main-session, true", detail.SessionID, detail.IsSubagent)
	}
	if fullSnapshotLoaded(src) {
		t.Fatal("MessageByID loaded the full snapshot; want only the message's session")
	}
}

// An id that no transcript is stored under still gets the authoritative
// answer from the full history instead of a false "not found".
func TestDetailLookupsFallBackToTheFullHistory(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		// The record names a session other than the one its file is stored as.
		"-home-andres-projects-scope/stored-as.jsonl": {
			`{"type":"user","uuid":"moved-user-1","sessionId":"recorded-as","timestamp":"2026-05-03T08:00:00Z","cwd":"/home/andres/projects/scope","message":{"role":"user","content":"Where am I stored?"}}`,
		},
	})
	ctx := testContext(t)

	detail, err := src.SessionByID(ctx, "recorded-as")
	if err != nil || detail == nil {
		t.Fatalf("SessionByID(recorded-as) = %+v, %v; want the session found through the full history", detail, err)
	}
	message, err := src.MessageByID(ctx, synthesizeMessageID("recorded-as", "moved-user-1", 0))
	if err != nil || message == nil {
		t.Fatalf("MessageByID for a relocated record = %+v, %v; want the message", message, err)
	}
	missing, err := src.SessionByID(ctx, "no-such-session")
	if err != nil || missing != nil {
		t.Fatalf("SessionByID(no-such-session) = %+v, %v; want nil, nil", missing, err)
	}
}

func TestMessageByIDRejectsForeignIDsWithoutScanning(t *testing.T) {
	src := scopeFixture(t)
	detail, err := src.MessageByID(testContext(t), "nope-123")
	if err != nil || detail != nil {
		t.Fatalf("MessageByID(nope-123) = %+v, %v; want nil, nil", detail, err)
	}
	if fullSnapshotLoaded(src) {
		t.Fatal("an id this source never issues must not cost a scan of the history")
	}
}

// A request the client abandoned must surface as its context error. Reported
// as "source unavailable" it would be recorded as a failed read of the recent
// window and shown as a data-completeness warning.
func TestCancelledLoadsReportTheContextNotAnUnavailableSource(t *testing.T) {
	src := scopeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := src.Messages(ctx, stats.PeriodQuery{FromTime: time.Now().Add(-time.Hour)}, 1, 10, stats.DefaultMessageSort()); !errors.Is(err, context.Canceled) {
		t.Errorf("bounded Messages() with a cancelled context = %v, want context.Canceled", err)
	}
	if _, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Overview(all) with a cancelled context = %v, want context.Canceled", err)
	}
	if _, err := src.SessionByID(ctx, "main-session"); !errors.Is(err, context.Canceled) {
		t.Errorf("SessionByID with a cancelled context = %v, want context.Canceled", err)
	}
	if info := src.Info(context.Background()); !info.Available {
		t.Errorf("source reported unavailable after cancelled requests: %+v", info.Diagnostics)
	}
}

// The single-session slot is reused while the session's files are unchanged
// and dropped as soon as one of them grows.
func TestSessionSnapshotIsReusedUntilATranscriptChanges(t *testing.T) {
	src := scopeFixture(t)
	ctx := testContext(t)
	belongs := func(pathSessionID string) bool { return pathSessionID == "main-session" }

	first, err := src.sessionSnapshot(ctx, belongs)
	if err != nil || first == nil {
		t.Fatalf("sessionSnapshot() = %v, %v", first, err)
	}
	again, err := src.sessionSnapshot(ctx, belongs)
	if err != nil || again != first {
		t.Fatalf("an unchanged session was parsed again (err %v)", err)
	}

	path := filepath.Join(src.opts.ClaudeHome, "projects", "-home-andres-projects-scope", "main-session.jsonl")
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(`{"type":"user","uuid":"main-user-2","sessionId":"main-session","timestamp":"2026-05-01T10:05:00Z","cwd":"/home/andres/projects/scope","message":{"role":"user","content":"One more thing."}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}

	detail, err := src.SessionByID(ctx, "main-session")
	if err != nil || detail == nil {
		t.Fatalf("SessionByID after an append = %+v, %v", detail, err)
	}
	if detail.MessageCount != 5 {
		t.Errorf("MessageCount after an append = %d, want 5 (the grown transcript must be re-read)", detail.MessageCount)
	}
}
