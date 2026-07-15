package codex

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

// independentThreadLines is a minimal self-contained thread: one user prompt and
// one assistant reply (ids <id>:<turn>:u0 and <id>:<turn>:r0). base is a
// minute-precision RFC3339 prefix, e.g. "2026-05-01T10:00".
func independentThreadLines(id, turn, base, userText, replyText string) []string {
	return []string{
		`{"timestamp":"` + base + `:00Z","type":"session_meta","payload":{"id":"` + id + `","model_provider":"openai","cwd":"[REDACTED_PATH]/proj-` + id + `"}}`,
		`{"timestamp":"` + base + `:01Z","type":"turn_context","payload":{"turn_id":"` + turn + `","model":"gpt-5.5","model_provider":"openai"}}`,
		`{"timestamp":"` + base + `:02Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` + turn + `"}}`,
		`{"timestamp":"` + base + `:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"` + turn + `","message":"` + userText + `"}}`,
		`{"timestamp":"` + base + `:04Z","type":"response_item","payload":{"turn_id":"` + turn + `","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + replyText + `"}]}}}`,
		`{"timestamp":"` + base + `:05Z","type":"event_msg","payload":{"type":"token_count","turn_id":"` + turn + `","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1340},"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1340}}}}`,
		`{"timestamp":"` + base + `:06Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"` + turn + `","status":"success"}}`,
	}
}

func newTargetedSource(t *testing.T, home string) *Source {
	t.Helper()
	return New(Options{
		CodexHome:           home,
		PathSource:          "targeted test",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		SnapshotTTL:         time.Nanosecond, // force fresh loads so each lookup is independent
	})
}

func TestThreadIDFromMessageID(t *testing.T) {
	cases := []struct {
		id     string
		wantID string
		wantOK bool
	}{
		{"codex:019f6272-65d9-77b0-ac8c-073a89aaf2b9:d51634e7-fb39-41ae-8c2d-8a5eb9ff7d03:r58", "019f6272-65d9-77b0-ac8c-073a89aaf2b9", true},
		{"codex:thread:turn:u0", "thread", true},
		{"codex:thread:turn:r0", "thread", true},
		{"codex:a:b:c:d:e", "a", true}, // extra colons still yield parts[1]
		{"claudecode:s:t:r0", "", false},
		{"codex:thread:turn", "", false}, // too few parts
		{"codex::turn:r0", "", false},    // empty thread id
		{"", "", false},
		{"not-an-id", "", false},
	}
	for _, tc := range cases {
		gotID, gotOK := threadIDFromMessageID(tc.id)
		if gotID != tc.wantID || gotOK != tc.wantOK {
			t.Errorf("threadIDFromMessageID(%q) = (%q, %v), want (%q, %v)", tc.id, gotID, gotOK, tc.wantID, tc.wantOK)
		}
	}
}

// TestLoadThreadSnapshotParsesOnlyTargetThread proves the targeted loader parses
// only the requested thread's file, never unrelated files — the property that
// keeps a single-message lookup from re-parsing the whole corpus (the 500 bug).
// It also asserts the targeted result is byte-identical to the full snapshot.
func TestLoadThreadSnapshotParsesOnlyTargetThread(t *testing.T) {
	ctx := testContext(t)
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/05/01/rollout-2026-05-01T10-00-00Z-target-thread.jsonl": independentThreadLines("target-thread", "t1", "2026-05-01T10:00", "HELLO_TARGET", "REPLY_TARGET"),
		"sessions/2026/05/01/rollout-2026-05-01T11-00-00Z-decoy-thread.jsonl":  independentThreadLines("decoy-thread", "d1", "2026-05-01T11:00", "HELLO_DECOY", "REPLY_DECOY"),
	})
	s := newTargetedSource(t, home)

	snap, matched, err := s.loadThreadSnapshot(ctx, "target-thread", false)
	if err != nil {
		t.Fatalf("loadThreadSnapshot failed: %v", err)
	}
	if !matched {
		t.Fatal("loadThreadSnapshot(target-thread) matched = false, want true")
	}
	if _, ok := snap.sessionMap["target-thread"]; !ok {
		t.Fatalf("targeted snapshot missing target-thread session: %v", sessionMapKeys(snap))
	}
	if _, ok := snap.sessionMap["decoy-thread"]; ok {
		t.Fatalf("targeted snapshot parsed the unrelated decoy-thread: %v", sessionMapKeys(snap))
	}

	// Parity: the targeted lookup returns exactly what a full-corpus parse would.
	id := "codex:target-thread:t1:r0"
	got := snap.messageByID(id)
	if got == nil {
		t.Fatalf("targeted messageByID(%q) = nil, want detail", id)
	}
	fullSnap, err := s.loadSnapshot(ctx)
	if err != nil {
		t.Fatalf("loadSnapshot failed: %v", err)
	}
	want := fullSnap.messageByID(id)
	if want == nil {
		t.Fatalf("full messageByID(%q) = nil, want detail", id)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("targeted detail != full detail:\n got=%#v\nwant=%#v", got, want)
	}
}

// TestMessageByIDForkedThreadMatchesFullSnapshot proves ancestor inclusion: a
// forked thread's message is resolved by parsing the fork's file plus its parent
// (needed to suppress the replayed parent history), yielding the same detail as
// a full parse — no PARENT_REPLY leakage into the fork row.
func TestMessageByIDForkedThreadMatchesFullSnapshot(t *testing.T) {
	ctx := testContext(t)
	s := newTargetedSource(t, writeTempCodexHome(t, forkFamilyFiles()))

	id := "codex:fork-a:fa-1:r0"
	got, err := s.MessageByID(ctx, id)
	if err != nil {
		t.Fatalf("MessageByID(%q) failed: %v", id, err)
	}
	if got == nil {
		t.Fatalf("MessageByID(%q) = nil, want detail", id)
	}
	if !detailTextContains(got, "FORK_A_REPLY") {
		t.Errorf("fork message missing its own reply: %#v", got.Content.TextParts)
	}
	if detailTextContains(got, "PARENT_REPLY") {
		t.Errorf("fork message leaked replayed parent content: %#v", got.Content.TextParts)
	}

	fullSnap, err := s.loadSnapshot(ctx)
	if err != nil {
		t.Fatalf("loadSnapshot failed: %v", err)
	}
	if want := fullSnap.messageByID(id); !reflect.DeepEqual(got, want) {
		t.Errorf("forked-thread detail != full detail:\n got=%#v\nwant=%#v", got, want)
	}
}

// TestMessageByIDResolvesNonZFilename proves the fix does not depend on the
// rollout filename format: the thread is found via its session_meta id even when
// the filename carries no trailing "Z" (so rolloutSessionID cannot extract the
// bare id).
func TestMessageByIDResolvesNonZFilename(t *testing.T) {
	ctx := testContext(t)
	home := writeTempCodexHome(t, map[string][]string{
		// No 'Z' before the id — matches the real-world filename that surfaced the bug.
		"sessions/2026/07/14/rollout-2026-07-14T15-04-52-thread-noz.jsonl": independentThreadLines("thread-noz", "t1", "2026-07-14T15:04", "HELLO_NOZ", "REPLY_NOZ"),
	})
	s := newTargetedSource(t, home)

	id := "codex:thread-noz:t1:r0"
	got, err := s.MessageByID(ctx, id)
	if err != nil {
		t.Fatalf("MessageByID(%q) failed: %v", id, err)
	}
	if got == nil || !detailTextContains(got, "REPLY_NOZ") {
		t.Fatalf("MessageByID(%q) did not resolve the message: %#v", id, got)
	}
}

// TestSessionByIDAggregatesForkFamily proves SessionByID pulls in every file
// that shares the session id (root + forks/resumes), not just the root file.
func TestSessionByIDAggregatesForkFamily(t *testing.T) {
	ctx := testContext(t)
	s := newTargetedSource(t, writeTempCodexHome(t, forkFamilyFiles()))

	got, err := s.SessionByID(ctx, "parent-session")
	if err != nil {
		t.Fatalf("SessionByID(parent-session) failed: %v", err)
	}
	if got == nil {
		t.Fatal("SessionByID(parent-session) = nil, want detail")
	}
	// 3 user + 4 assistant across the parent and both fork files.
	if got.MessageCount != 7 {
		t.Errorf("SessionByID(parent-session).MessageCount = %d, want 7", got.MessageCount)
	}

	fullSnap, err := s.loadSnapshot(ctx)
	if err != nil {
		t.Fatalf("loadSnapshot failed: %v", err)
	}
	if want := fullSnap.sessionByID("parent-session"); !reflect.DeepEqual(got, want) {
		t.Errorf("targeted session != full session:\n got=%#v\nwant=%#v", got, want)
	}
}

// TestMessageByIDUnderTightTimeoutSkipsUnrelatedFiles is the end-to-end
// regression guard for the reported 500: with a large unrelated decoy present
// and a scan budget far too small to parse it, a message lookup still succeeds
// (hit) or cleanly misses (nil, no error), because only the target thread's tiny
// file is parsed. If the code ever reverts to a full-corpus parse, the decoy
// blows the budget and this fails.
func TestMessageByIDUnderTightTimeoutSkipsUnrelatedFiles(t *testing.T) {
	ctx := testContext(t)

	decoy := []string{
		`{"timestamp":"2026-05-02T09:00:00Z","type":"session_meta","payload":{"id":"decoy-thread","model_provider":"openai","cwd":"[REDACTED_PATH]/decoy"}}`,
	}
	for i := 0; i < 40000; i++ {
		n := strconv.Itoa(i)
		decoy = append(decoy,
			`{"timestamp":"2026-05-02T09:00:01Z","type":"response_item","payload":{"turn_id":"dt-`+n+`","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"decoy line `+n+` with enough text to make the JSON decode non-trivial"}]}}}`,
		)
	}
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/05/01/rollout-2026-05-01T10-00-00Z-target-thread.jsonl": independentThreadLines("target-thread", "t1", "2026-05-01T10:00", "HELLO_TARGET", "REPLY_TARGET"),
		"sessions/2026/05/02/rollout-2026-05-02T09-00-00Z-decoy-thread.jsonl":  decoy,
	})
	s := New(Options{
		CodexHome:           home,
		PathSource:          "tight timeout test",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		SnapshotTTL:         time.Nanosecond,
		ScanTimeout:         100 * time.Millisecond, // far too small to parse the 40k-line decoy
	})

	hit, err := s.MessageByID(ctx, "codex:target-thread:t1:r0")
	if err != nil {
		t.Fatalf("MessageByID(hit) failed (did it parse the decoy?): %v", err)
	}
	if hit == nil || !detailTextContains(hit, "REPLY_TARGET") {
		t.Fatalf("MessageByID(hit) did not resolve target message: %#v", hit)
	}

	// A miss on a real thread must be a clean 404 (nil, nil) — not a fallback to
	// the full corpus (which would trip the timeout).
	miss, err := s.MessageByID(ctx, "codex:target-thread:t1:r99")
	if err != nil {
		t.Fatalf("MessageByID(miss) returned error instead of nil (fell back to full parse?): %v", err)
	}
	if miss != nil {
		t.Fatalf("MessageByID(miss) = %#v, want nil", miss)
	}
}

func sessionMapKeys(snap *snapshot) []string {
	keys := make([]string, 0, len(snap.sessionMap))
	for k := range snap.sessionMap {
		keys = append(keys, k)
	}
	return keys
}
