package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// forkParentLines is a parent thread with two API requests:
// r0 raw (1000,200,100,40) with PARENT_REPLY content, r1 raw (1600,600,100,40).
func forkParentLines() []string {
	return []string{
		`{"timestamp":"2026-03-01T10:00:00Z","type":"session_meta","payload":{"id":"parent-session","model_provider":"openai","cwd":"[REDACTED_PATH]/forkproj"}}`,
		`{"timestamp":"2026-03-01T10:00:01Z","type":"turn_context","payload":{"turn_id":"pt-1","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"2026-03-01T10:00:02Z","type":"event_msg","payload":{"type":"task_started","turn_id":"pt-1"}}`,
		`{"timestamp":"2026-03-01T10:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"pt-1","message":"P1"}}`,
		`{"timestamp":"2026-03-01T10:00:03.500Z","type":"response_item","payload":{"turn_id":"pt-1","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"PARENT_REPLY"}]}}}`,
		`{"timestamp":"2026-03-01T10:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pt-1","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100},"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}}}}`,
		`{"timestamp":"2026-03-01T10:00:05Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pt-1","info":{"last_token_usage":{"input_tokens":1600,"cached_input_tokens":600,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1700},"total_token_usage":{"input_tokens":2600,"cached_input_tokens":800,"output_tokens":200,"reasoning_output_tokens":80,"total_tokens":2800}}}}`,
		`{"timestamp":"2026-03-01T10:00:06Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"pt-1","status":"success"}}`,
	}
}

// forkReplayBurst mirrors the real fork pattern: the fork's own session_meta,
// then a replayed copy of the parent's history — parent meta, turn context, and
// every parent token_count with its ORIGINAL cumulative values but FRESH
// timestamps in a millisecond burst.
func forkReplayBurst(forkID, base string) []string {
	return []string{
		`{"timestamp":"` + base + `.000Z","type":"session_meta","payload":{"id":"` + forkID + `","session_id":"parent-session","forked_from_id":"parent-session","parent_thread_id":"parent-session","model_provider":"openai","cwd":"[REDACTED_PATH]/forkproj"}}`,
		`{"timestamp":"` + base + `.001Z","type":"session_meta","payload":{"id":"parent-session","model_provider":"openai","cwd":"[REDACTED_PATH]/forkproj"}}`,
		`{"timestamp":"` + base + `.002Z","type":"turn_context","payload":{"turn_id":"pt-1","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"` + base + `.002Z","type":"event_msg","payload":{"type":"task_started","turn_id":"pt-1"}}`,
		`{"timestamp":"` + base + `.003Z","type":"response_item","payload":{"turn_id":"pt-1","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"PARENT_REPLY"}]}}}`,
		`{"timestamp":"` + base + `.004Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pt-1","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}}}}`,
		`{"timestamp":"` + base + `.005Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pt-1","info":{"total_token_usage":{"input_tokens":2600,"cached_input_tokens":800,"output_tokens":200,"reasoning_output_tokens":80,"total_tokens":2800}}}}`,
	}
}

// forkALines continues from the replay burst with the fork's own turn:
// one request of raw delta (1000,700,50,20) with FORK_A_REPLY content.
func forkALines() []string {
	return append(forkReplayBurst("fork-a", "2026-03-01T11:00:00"), []string{
		`{"timestamp":"2026-03-01T11:00:05Z","type":"turn_context","payload":{"turn_id":"fa-1","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"2026-03-01T11:00:06Z","type":"event_msg","payload":{"type":"task_started","turn_id":"fa-1"}}`,
		`{"timestamp":"2026-03-01T11:00:07Z","type":"event_msg","payload":{"type":"user_message","turn_id":"fa-1","message":"FA1"}}`,
		`{"timestamp":"2026-03-01T11:00:08Z","type":"response_item","payload":{"turn_id":"fa-1","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"FORK_A_REPLY"}]}}}`,
		`{"timestamp":"2026-03-01T11:00:09Z","type":"event_msg","payload":{"type":"token_count","turn_id":"fa-1","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":700,"output_tokens":50,"reasoning_output_tokens":20,"total_tokens":1050},"total_token_usage":{"input_tokens":3600,"cached_input_tokens":1500,"output_tokens":250,"reasoning_output_tokens":100,"total_tokens":3850}}}}`,
		`{"timestamp":"2026-03-01T11:00:10Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"fa-1","status":"success"}}`,
	}...)
}

// forkBLines is a second parallel fork whose own request is usage-only:
// raw delta (2500,200,200,70) with no buffered content.
func forkBLines() []string {
	return append(forkReplayBurst("fork-b", "2026-03-01T11:30:00"), []string{
		`{"timestamp":"2026-03-01T11:30:05Z","type":"turn_context","payload":{"turn_id":"fb-1","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"2026-03-01T11:30:06Z","type":"event_msg","payload":{"type":"task_started","turn_id":"fb-1"}}`,
		`{"timestamp":"2026-03-01T11:30:07Z","type":"event_msg","payload":{"type":"user_message","turn_id":"fb-1","message":"FB1"}}`,
		`{"timestamp":"2026-03-01T11:30:09Z","type":"event_msg","payload":{"type":"token_count","turn_id":"fb-1","info":{"total_token_usage":{"input_tokens":5100,"cached_input_tokens":1000,"output_tokens":400,"reasoning_output_tokens":150,"total_tokens":5500}}}}`,
		`{"timestamp":"2026-03-01T11:30:10Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"fb-1","status":"success"}}`,
	}...)
}

func forkFamilyFiles() map[string][]string {
	return map[string][]string{
		"sessions/2026/03/01/rollout-2026-03-01T10-00-00Z-parent-session.jsonl": forkParentLines(),
		"sessions/2026/03/01/rollout-2026-03-01T11-00-00Z-fork-a.jsonl":         forkALines(),
		"sessions/2026/03/01/rollout-2026-03-01T11-30-00Z-fork-b.jsonl":         forkBLines(),
	}
}

func TestCodexForkedThreadsAccountPerThreadWithoutReplayDoubleCount(t *testing.T) {
	src := newTempCodexSource(t, forkFamilyFiles())
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Disjoint sums: parent r0 (800,200,60,40) + parent r1 (1000,600,60,40)
	// + fork A (300,700,30,20) + fork B (2300,200,130,70).
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 4400, 1700, 280, 170)
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1 (forks group under the parent session)", overview.Sessions)
	}
	if overview.Messages != 7 {
		t.Errorf("Overview().Messages = %d, want 7 (3 user + 4 assistant, replayed history suppressed)", overview.Messages)
	}

	messages := readAllMessages(t, src)
	if messages.Total != 7 {
		t.Fatalf("Messages total = %d, want 7", messages.Total)
	}
	for _, msg := range messages.Messages {
		if msg.SessionID != "parent-session" {
			t.Errorf("message %q SessionID = %q, want parent-session", msg.ID, msg.SessionID)
		}
	}

	// Per-thread rows carry thread-scoped ids and exact per-request deltas.
	forkARow := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == "codex:fork-a:fa-1:r0" })
	if forkARow.Tokens == nil || forkARow.Tokens.Input != 300 || forkARow.Tokens.Cache.Read != 700 || forkARow.Tokens.Output != 30 || forkARow.Tokens.Reasoning != 20 {
		t.Errorf("fork A row tokens = %+v, want disjoint (300,700,30,20)", forkARow.Tokens)
	}
	forkBRow := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == "codex:fork-b:fb-1:r0" })
	if forkBRow.Tokens == nil || forkBRow.Tokens.Input != 2300 || forkBRow.Tokens.Cache.Read != 200 || forkBRow.Tokens.Output != 130 || forkBRow.Tokens.Reasoning != 70 {
		t.Errorf("fork B row tokens = %+v, want disjoint (2300,200,130,70)", forkBRow.Tokens)
	}

	// Replayed parent content must not duplicate into fork rows.
	occurrences := 0
	for _, msg := range messages.Messages {
		detail := mustMessageDetail(t, src, msg.ID)
		occurrences += detailTextOccurrences(detail, "PARENT_REPLY")
	}
	if occurrences != 1 {
		t.Errorf("PARENT_REPLY occurrences across all message details = %d, want exactly 1 (on the parent row)", occurrences)
	}
}

func TestCodexForkWithDeletedParentReconstructsHistoryOnce(t *testing.T) {
	// When the parent transcript is genuinely gone, the fork's replayed ladder
	// is the only record of that usage: it is counted (once), timestamped at
	// the replay, and the fork's own usage follows on top.
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/03/01/rollout-2026-03-01T11-00-00Z-fork-a.jsonl": forkALines(),
	})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Reconstructed parent ladder (800,200,60,40)+(1000,600,60,40) plus the
	// fork's own request (300,700,30,20).
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 2100, 1500, 150, 100)
	if overview.Sessions != 1 {
		t.Errorf("Overview().Sessions = %d, want 1", overview.Sessions)
	}
}

func TestCodexParallelForksOfDeletedParentReconstructHistoryOnlyOnce(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/03/01/rollout-2026-03-01T11-00-00Z-fork-a.jsonl": forkALines(),
		"sessions/2026/03/01/rollout-2026-03-01T11-30-00Z-fork-b.jsonl": forkBLines(),
	})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Fork A reconstructs the parent ladder; fork B's replay of the same ladder
	// is recognized as already counted and only seeds its baseline.
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 4400, 1700, 280, 170)
}

func TestCodexBoundedLoadPullsPrunedParentForForkAccounting(t *testing.T) {
	home := writeTempCodexHome(t, forkFamilyFiles())
	parentPath := filepath.Join(home, "sessions", "2026", "03", "01", "rollout-2026-03-01T10-00-00Z-parent-session.jsonl")
	oldTime := time.Date(2026, 3, 1, 10, 5, 0, 0, time.UTC)
	if err := os.Chtimes(parentPath, oldTime, oldTime); err != nil {
		t.Fatalf("age parent transcript: %v", err)
	}
	src := New(Options{
		CodexHome:           home,
		PathSource:          "temp codex fixture",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
	})

	// FromTime 10:30 prunes the parent by mtime (10:05 < 10:20 threshold) but
	// keeps both fork files. Without the parent pull-in, the forks' replayed
	// ladders would surface as new usage inside the window.
	from := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{FromTime: from})
	if err != nil {
		t.Fatalf("Overview(bounded) failed: %v", err)
	}
	// Only the forks' own usage lies within the window.
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 2600, 900, 160, 90)
}

func TestCodexForkReplayWithReplayedUserMessageStaysSuppressed(t *testing.T) {
	// Some fork replays re-emit the parent's user_message events inside the
	// burst (observed in real rollouts). The replayed prompt must not end
	// replay mode, or the rest of the replayed token ladder would count again.
	forkLines := []string{
		`{"timestamp":"2026-03-01T12:00:00.000Z","type":"session_meta","payload":{"id":"fork-c","session_id":"parent-session","forked_from_id":"parent-session","model_provider":"openai","cwd":"[REDACTED_PATH]/forkproj"}}`,
		`{"timestamp":"2026-03-01T12:00:00.001Z","type":"session_meta","payload":{"id":"parent-session","model_provider":"openai","cwd":"[REDACTED_PATH]/forkproj"}}`,
		`{"timestamp":"2026-03-01T12:00:00.002Z","type":"turn_context","payload":{"turn_id":"pt-1","model":"gpt-5.5","model_provider":"openai"}}`,
		// Replayed parent user prompt between the replayed token counts.
		`{"timestamp":"2026-03-01T12:00:00.003Z","type":"event_msg","payload":{"type":"user_message","turn_id":"pt-1","message":"P1"}}`,
		`{"timestamp":"2026-03-01T12:00:00.004Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pt-1","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}}}}`,
		`{"timestamp":"2026-03-01T12:00:00.005Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pt-1","info":{"total_token_usage":{"input_tokens":2600,"cached_input_tokens":800,"output_tokens":200,"reasoning_output_tokens":80,"total_tokens":2800}}}}`,
		`{"timestamp":"2026-03-01T12:00:05Z","type":"turn_context","payload":{"turn_id":"fc-1","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"2026-03-01T12:00:06Z","type":"event_msg","payload":{"type":"user_message","turn_id":"fc-1","message":"FC1"}}`,
		`{"timestamp":"2026-03-01T12:00:07Z","type":"event_msg","payload":{"type":"token_count","turn_id":"fc-1","info":{"total_token_usage":{"input_tokens":3600,"cached_input_tokens":1500,"output_tokens":250,"reasoning_output_tokens":100,"total_tokens":3850}}}}`,
	}
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/03/01/rollout-2026-03-01T10-00-00Z-parent-session.jsonl": forkParentLines(),
		"sessions/2026/03/01/rollout-2026-03-01T12-00-00Z-fork-c.jsonl":         forkLines,
	})
	ctx := testContext(t)
	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Parent (800,200,60,40)+(1000,600,60,40) plus fork C's own (300,700,30,20).
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 2100, 1500, 150, 100)
	if overview.Messages != 5 {
		t.Errorf("Overview().Messages = %d, want 5 (replayed P1 prompt suppressed, own FC1 kept)", overview.Messages)
	}
	// The replayed P1 prompt must not duplicate; the fork's own FC1 must exist.
	messages := readAllMessages(t, src)
	p1Rows := 0
	for _, msg := range messages.Messages {
		if msg.Role != "user" {
			continue
		}
		detail := mustMessageDetail(t, src, msg.ID)
		p1Rows += detailTextOccurrences(detail, "P1")
	}
	if p1Rows != 1 {
		t.Errorf("user prompt P1 appears %d times, want once", p1Rows)
	}
	findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == "codex:fork-c:fc-1:u0" })
}

func TestCodexResumedThreadContinuesSessionWithoutReplayDoubleCount(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/03/02/rollout-2026-03-02T09-00-00Z-session-r.jsonl": {
			`{"timestamp":"2026-03-02T09:00:00Z","type":"session_meta","payload":{"id":"session-r","model_provider":"openai","cwd":"[REDACTED_PATH]/resumeproj"}}`,
			`{"timestamp":"2026-03-02T09:00:01Z","type":"turn_context","payload":{"turn_id":"rt-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-03-02T09:00:02Z","type":"event_msg","payload":{"type":"user_message","turn_id":"rt-1","message":"R1"}}`,
			`{"timestamp":"2026-03-02T09:00:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"rt-1","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}}}}`,
		},
		"sessions/2026/03/02/rollout-2026-03-02T10-00-00Z-resume-thread.jsonl": {
			`{"timestamp":"2026-03-02T10:00:00.000Z","type":"session_meta","payload":{"id":"resume-thread","session_id":"session-r","model_provider":"openai","cwd":"[REDACTED_PATH]/resumeproj"}}`,
			`{"timestamp":"2026-03-02T10:00:00.001Z","type":"session_meta","payload":{"id":"session-r","model_provider":"openai","cwd":"[REDACTED_PATH]/resumeproj"}}`,
			`{"timestamp":"2026-03-02T10:00:00.002Z","type":"turn_context","payload":{"turn_id":"rt-1","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-03-02T10:00:00.003Z","type":"event_msg","payload":{"type":"token_count","turn_id":"rt-1","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}}}}`,
			`{"timestamp":"2026-03-02T10:00:05Z","type":"turn_context","payload":{"turn_id":"rt-2","model":"gpt-5.5","model_provider":"openai"}}`,
			`{"timestamp":"2026-03-02T10:00:06Z","type":"event_msg","payload":{"type":"user_message","turn_id":"rt-2","message":"R2"}}`,
			`{"timestamp":"2026-03-02T10:00:07Z","type":"event_msg","payload":{"type":"token_count","turn_id":"rt-2","info":{"total_token_usage":{"input_tokens":1800,"cached_input_tokens":500,"output_tokens":180,"reasoning_output_tokens":60,"total_tokens":1980}}}}`,
		},
	})
	ctx := testContext(t)
	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Original r0 disjoint (800,200,60,40) + resume's own request (500,300,60,20).
	assertCodexTokenTotals(t, "Overview().Tokens", overview.Tokens, 1300, 500, 120, 60)
	if overview.Sessions != 1 || overview.Messages != 4 {
		t.Errorf("overview sessions/messages = %d/%d, want 1/4", overview.Sessions, overview.Messages)
	}
}
