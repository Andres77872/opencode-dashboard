package codex

import (
	"os"
	"path/filepath"
	"strings"
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
