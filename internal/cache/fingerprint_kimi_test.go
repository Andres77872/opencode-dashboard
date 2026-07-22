package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"opencode-dashboard/internal/source"
)

func TestKimiFingerprintTracksV2MetadataAllWiresAndSessionIndex(t *testing.T) {
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "workspace-key", "opaque-v2-session-id")
	mainDir := filepath.Join(sessionDir, "agents", "main")
	subagentDir := filepath.Join(sessionDir, "agents", "worker-with-arbitrary-id")
	for _, dir := range []string{mainDir, subagentDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeFingerprintFixture(t, filepath.Join(sessionDir, "state.json"), `{"version":2,"id":"opaque-v2-session-id"}`)
	writeFingerprintFixture(t, filepath.Join(sessionDir, "wire.jsonl"), "root-wire\n")
	writeFingerprintFixture(t, filepath.Join(mainDir, "wire.jsonl"), "main-wire\n")
	writeFingerprintFixture(t, filepath.Join(subagentDir, "wire.jsonl"), "subagent-wire\n")
	writeFingerprintFixture(t, filepath.Join(home, "session_index.jsonl"), "index-v1\n")

	info := source.SourceInfo{ID: source.SourceKimiCode, Kind: "jsonl", Path: home}
	first := kimiFingerprintForTest(t, info)
	writeFingerprintFixture(t, filepath.Join(sessionDir, "state.json"), `{"version":2,"id":"opaque-v2-session-id","cwd":"/workspace"}`)
	stateChanged := kimiFingerprintForTest(t, info)
	if stateChanged == first {
		t.Fatal("Kimi fingerprint did not change for state.json in an arbitrary v2 session directory")
	}
	writeFingerprintFixture(t, filepath.Join(sessionDir, "wire.jsonl"), "root-wire-with-canonical-record\n")
	rootWireChanged := kimiFingerprintForTest(t, info)
	if rootWireChanged == stateChanged {
		t.Fatal("Kimi fingerprint did not change after a root wire.jsonl change")
	}

	// No main/root/state file changes: a subagent-only append must still make
	// the consolidated cache stale because every agent wire contributes usage.
	writeFingerprintFixture(t, filepath.Join(subagentDir, "wire.jsonl"), "subagent-wire-with-new-request\n")
	second := kimiFingerprintForTest(t, info)
	if second == rootWireChanged {
		t.Fatal("Kimi fingerprint did not change after a subagent-only wire change")
	}

	// The read-model index can supply cwd for sessions whose state omits it.
	writeFingerprintFixture(t, filepath.Join(home, "session_index.jsonl"), "index-v2-with-updated-workspace\n")
	third := kimiFingerprintForTest(t, info)
	if third == second {
		t.Fatal("Kimi fingerprint did not change after session_index.jsonl changed")
	}
}

func kimiFingerprintForTest(t *testing.T, info source.SourceInfo) string {
	t.Helper()
	fingerprint, err := sourceFingerprint(context.Background(), info)
	if err != nil {
		t.Fatalf("sourceFingerprint: %v", err)
	}
	return fingerprint
}

func writeFingerprintFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
