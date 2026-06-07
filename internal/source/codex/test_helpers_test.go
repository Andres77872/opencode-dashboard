package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func fixturePath(t *testing.T, elems ...string) string {
	t.Helper()
	parts := append([]string{"testdata"}, elems...)
	return filepath.Join(parts...)
}

func syntheticRolloutPath(t *testing.T) string {
	t.Helper()
	return fixturePath(t, "valid_home", "sessions", "2026", "01", "02", "rollout-2026-01-02T03-04-05Z-synthetic-session.jsonl")
}

func copyFixtureHome(t *testing.T, fixtureName string) string {
	t.Helper()
	src := fixturePath(t, fixtureName)
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy fixture %s: %v", fixtureName, err)
	}
	return dst
}

func newFixtureSource(t *testing.T, fixtureName string) source.Source {
	t.Helper()
	return New(Options{
		CodexHome:           fixturePath(t, fixtureName),
		PathSource:          "test fixture",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
	})
}

func newCopiedFixtureSource(t *testing.T, fixtureName string) (source.Source, string) {
	t.Helper()
	home := copyFixtureHome(t, fixtureName)
	return New(Options{
		CodexHome:           home,
		PathSource:          "copied test fixture",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
	}), home
}

func newTempCodexSource(t *testing.T, files map[string][]string) source.Source {
	t.Helper()
	home := writeTempCodexHome(t, files)
	return New(Options{
		CodexHome:           home,
		PathSource:          "temp codex fixture",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
	})
}

func writeTempCodexHome(t *testing.T, files map[string][]string) string {
	t.Helper()
	home := t.TempDir()
	for relPath, lines := range files {
		path := filepath.Join(home, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture dir for %s: %v", relPath, err)
		}
		content := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", relPath, err)
		}
	}
	return home
}

func readAllMessages(t *testing.T, src source.Source) stats.MessageList {
	t.Helper()
	messages, err := src.Messages(testContext(t), stats.PeriodQuery{Period: "all"}, 1, 200, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	return messages
}

func chronologicalMessageSort() stats.MessageSort {
	return stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc}
}

func findMessage(t *testing.T, list stats.MessageList, predicate func(stats.MessageEntry) bool) stats.MessageEntry {
	t.Helper()
	for _, msg := range list.Messages {
		if predicate(msg) {
			return msg
		}
	}
	encoded, _ := json.MarshalIndent(list.Messages, "", "  ")
	t.Fatalf("message not found in list: %s", encoded)
	return stats.MessageEntry{}
}

func mustMessageDetail(t *testing.T, src source.Source, id string) *stats.MessageDetail {
	t.Helper()
	detail, err := src.MessageByID(testContext(t), id)
	if err != nil {
		t.Fatalf("MessageByID(%q) failed: %v", id, err)
	}
	if detail == nil {
		t.Fatalf("MessageByID(%q) = nil, want detail", id)
	}
	return detail
}

func findToolPart(t *testing.T, detail *stats.MessageDetail, callID string) stats.ToolPart {
	t.Helper()
	for _, part := range detail.Content.ToolParts {
		if part.CallID == callID {
			return part
		}
	}
	t.Fatalf("tool call %q not found in message %q: %#v", callID, detail.ID, detail.Content.ToolParts)
	return stats.ToolPart{}
}

func assertCodexSourceID(t *testing.T, got string) {
	t.Helper()
	if got != string(source.SourceCodex) {
		t.Errorf("source_id = %q, want %q", got, source.SourceCodex)
	}
}

func assertJSONDoesNotContain(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	body := string(encoded)
	for _, needle := range forbidden {
		if strings.Contains(body, needle) {
			t.Errorf("JSON output leaked %q in %s", needle, body)
		}
	}
}

func detailTextContains(detail *stats.MessageDetail, want string) bool {
	return detailTextOccurrences(detail, want) > 0
}

func detailTextOccurrences(detail *stats.MessageDetail, want string) int {
	count := 0
	for _, part := range detail.Content.TextParts {
		count += strings.Count(part.Text, want)
	}
	for _, part := range detail.Content.ReasoningParts {
		count += strings.Count(part.Text, want)
	}
	return count
}

func findModelEntryByID(t *testing.T, models stats.ModelStats, modelID string) stats.ModelEntry {
	t.Helper()
	for _, model := range models.Models {
		if model.ModelID == modelID {
			return model
		}
	}
	t.Fatalf("model %q not found in %#v", modelID, models.Models)
	return stats.ModelEntry{}
}

func findToolEntryByName(t *testing.T, tools stats.ToolStats, name string) stats.ToolEntry {
	t.Helper()
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %#v", name, tools.Tools)
	return stats.ToolEntry{}
}

type fileTreeSnapshot map[string]fileSnapshot

type fileSnapshot struct {
	Mode   os.FileMode
	Size   int64
	SHA256 string
}

func snapshotTree(t *testing.T, root string) fileTreeSnapshot {
	t.Helper()
	snap := make(fileTreeSnapshot)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		snap[filepath.ToSlash(rel)] = fileSnapshot{Mode: info.Mode(), Size: info.Size(), SHA256: hex.EncodeToString(sum[:])}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %s: %v", root, err)
	}
	return snap
}

func assertTreeUnchanged(t *testing.T, before, after fileTreeSnapshot) {
	t.Helper()
	if len(after) != len(before) {
		t.Errorf("file count after scan = %d, want %d", len(after), len(before))
	}
	for rel, want := range before {
		got, ok := after[rel]
		if !ok {
			t.Errorf("file %s missing after scan", rel)
			continue
		}
		if got != want {
			t.Errorf("file %s changed after scan: got %#v, want %#v", rel, got, want)
		}
	}
	for rel := range after {
		if _, ok := before[rel]; !ok {
			t.Errorf("unexpected file created during scan: %s", rel)
		}
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func approxEqual(got, want float64) bool {
	return math.Abs(got-want) < 0.0000001
}

func codexForbiddenText() []string {
	return []string{
		"SYNTHETIC_AUTH_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_REFRESH_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_LOG_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_STATE_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_HISTORY_PROMPT_MUST_NOT_LEAK",
		"SYNTHETIC_MODEL_CACHE_MUST_NOT_LEAK",
		"SYNTHETIC_CACHE_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_SKILL_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_TMP_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_PLUGIN_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_DEBUG_LOG_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_NON_ROLLOUT_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_SESSION_HISTORY_SENTINEL_MUST_NOT_LEAK",
		"SYNTHETIC_CONFIG_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_HEADER_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_INSTRUCTIONS_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_PROMPT_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_ASSISTANT_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_TOOL_ARG_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_TOOL_OUTPUT_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_PATCH_SECRET_MUST_NOT_LEAK",
	}
}
