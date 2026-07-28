package codex

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// TestBoundedSnapshotPrunesByFileMtime proves file-level pruning: a rollout
// whose mtime predates the bounded window's threshold is never parsed, even
// if its content claims in-window timestamps; the full snapshot still sees it.
func TestBoundedSnapshotPrunesByFileMtime(t *testing.T) {
	ctx := testContext(t)
	home := copyFixtureHome(t, "valid_home")

	// Build a second rollout with the fixture's structure but recent in-file
	// timestamps and a distinct session id.
	fixtureFile := filepath.Join(home, "sessions", "2026", "01", "02", "rollout-2026-01-02T03-04-05Z-synthetic-session.jsonl")
	content, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	recentHour := now.Add(-1 * time.Hour).Format("2006-01-02T15")
	recent := strings.ReplaceAll(string(content), "2026-01-02T03", recentHour)
	recent = strings.ReplaceAll(recent, "synthetic-session", "recent-session")

	dir := filepath.Join(home, "sessions", "recent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prunedFile := filepath.Join(dir, "rollout-recent-pruned.jsonl")
	if err := os.WriteFile(prunedFile, []byte(strings.ReplaceAll(recent, "recent-session", "pruned-session")), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-48 * time.Hour)
	if err := os.Chtimes(prunedFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	keptFile := filepath.Join(dir, "rollout-recent-kept.jsonl")
	if err := os.WriteFile(keptFile, []byte(recent), 0o644); err != nil {
		t.Fatal(err)
	}

	src := New(Options{
		CodexHome:           home,
		PathSource:          "bounded test",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		SnapshotTTL:         time.Nanosecond, // force fresh loads per call
	})

	bounded, err := src.Messages(ctx, stats.PeriodQuery{FromTime: now.Add(-2 * time.Hour)}, 1, 200, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("bounded Messages() failed: %v", err)
	}
	sessions := map[string]bool{}
	for _, msg := range bounded.Messages {
		sessions[msg.SessionID] = true
	}
	if !sessions["recent-session"] {
		t.Fatalf("bounded load missing recent-session messages: %v", sessions)
	}
	if sessions["pruned-session"] {
		t.Fatalf("bounded load parsed the old-mtime file: %v", sessions)
	}

	full, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 200, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("full Messages() failed: %v", err)
	}
	fullSessions := map[string]bool{}
	for _, msg := range full.Messages {
		fullSessions[msg.SessionID] = true
	}
	if !fullSessions["pruned-session"] || !fullSessions["recent-session"] {
		t.Fatalf("full load = %v, want both recent rollouts (bounded load must not poison the full snapshot)", fullSessions)
	}
}

func TestSnapshotLoadsRetryAfterInFlightInvalidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		bounded bool
	}{
		{name: "full"},
		{name: "bounded", bounded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
			relPath := "sessions/current/rollout-invalidation.jsonl"
			home := writeTempCodexHome(t, map[string][]string{
				relPath: invalidationRollout(now, "stale-session"),
			})
			path := filepath.Join(home, filepath.FromSlash(relPath))
			src := New(Options{
				CodexHome:           home,
				PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
			})

			parsed := make(chan struct{})
			release := make(chan struct{})
			var parsedOnce sync.Once
			var releaseOnce sync.Once
			releaseLoad := func() {
				releaseOnce.Do(func() { close(release) })
			}
			t.Cleanup(releaseLoad)
			src.afterSnapshotParsed = func() {
				parsedOnce.Do(func() {
					close(parsed)
					<-release
				})
			}

			query := stats.PeriodQuery{Period: "all"}
			if tt.bounded {
				query = stats.PeriodQuery{FromTime: now.Add(-time.Hour)}
			}
			type loadResult struct {
				messages stats.MessageList
				err      error
			}
			resultC := make(chan loadResult, 1)
			ctx := testContext(t)
			go func() {
				messages, err := src.Messages(ctx, query, 1, 200, chronologicalMessageSort())
				resultC <- loadResult{messages: messages, err: err}
			}()

			select {
			case <-parsed:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for the first snapshot parse")
			}
			content := strings.Join(invalidationRollout(now, "fresh-session"), "\n") + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("replace transcript during load: %v", err)
			}
			src.Invalidate()
			releaseLoad()

			var result loadResult
			select {
			case result = <-resultC:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for the retried snapshot load")
			}
			if result.err != nil {
				t.Fatalf("Messages() after in-flight invalidation failed: %v", result.err)
			}
			sessions := make(map[string]bool)
			for _, message := range result.messages.Messages {
				sessions[message.SessionID] = true
			}
			if !sessions["fresh-session"] || sessions["stale-session"] {
				t.Fatalf("retried %s load returned sessions %v, want only fresh-session", tt.name, sessions)
			}

			src.mu.Lock()
			generation := src.generation
			fullCached := src.snapshot != nil
			boundedCached := src.bounded != nil
			src.mu.Unlock()
			if generation != 1 {
				t.Errorf("generation after in-flight invalidation = %d, want 1", generation)
			}
			if tt.bounded && !boundedCached {
				t.Error("stable bounded snapshot was not cached")
			}
			if !tt.bounded && !fullCached {
				t.Error("stable full snapshot was not cached")
			}
		})
	}
}

func invalidationRollout(now time.Time, sessionID string) []string {
	turnID := sessionID + "-turn"
	return []string{
		`{"timestamp":"` + now.Format(time.RFC3339) + `","type":"session_meta","payload":{"id":"` + sessionID + `","model_provider":"openai"}}`,
		`{"timestamp":"` + now.Add(time.Second).Format(time.RFC3339) + `","type":"turn_context","payload":{"turn_id":"` + turnID + `","model":"gpt-5.2-codex","model_provider":"openai"}}`,
		`{"timestamp":"` + now.Add(2*time.Second).Format(time.RFC3339) + `","type":"event_msg","payload":{"type":"token_count","turn_id":"` + turnID + `","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":125}}}}`,
	}
}
