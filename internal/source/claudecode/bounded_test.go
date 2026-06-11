package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// TestBoundedSnapshotPrunesByFileMtime proves file-level pruning: a transcript
// whose mtime predates the bounded window's threshold is never parsed, even
// if its content claims in-window timestamps; the full snapshot still sees it.
func TestBoundedSnapshotPrunesByFileMtime(t *testing.T) {
	ctx := testContext(t)
	home := t.TempDir()
	projDir := filepath.Join(home, "projects", "-tmp-proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	recentTS := now.Add(-1 * time.Hour).Format(time.RFC3339)
	line := func(uuid, session string) string {
		return fmt.Sprintf(`{"type":"assistant","uuid":%q,"session_id":%q,"timestamp":%q,"cwd":"/tmp/proj","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":10,"output_tokens":5}}}`, uuid, session, recentTS)
	}
	oldFile := filepath.Join(projDir, "old-session.jsonl")
	if err := os.WriteFile(oldFile, []byte(line("old-msg", "old-session")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-48 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(projDir, "new-session.jsonl")
	if err := os.WriteFile(newFile, []byte(line("new-msg", "new-session")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := New(Options{
		ClaudeHome:          home,
		PathSource:          "bounded test",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		SnapshotTTL:         time.Nanosecond, // force fresh loads per call
	})

	bounded, err := src.Messages(ctx, stats.PeriodQuery{FromTime: now.Add(-2 * time.Hour)}, 1, 100, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("bounded Messages() failed: %v", err)
	}
	if bounded.Total != 1 || bounded.Messages[0].SessionID != "new-session" {
		t.Fatalf("bounded load = %+v, want only the new-session message (old-mtime file pruned)", bounded.Messages)
	}

	full, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 100, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("full Messages() failed: %v", err)
	}
	if full.Total != 2 {
		t.Fatalf("full load total = %d, want 2 (bounded load must not poison the full snapshot)", full.Total)
	}
}
