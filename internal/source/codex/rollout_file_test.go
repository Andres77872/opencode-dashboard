package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"opencode-dashboard/internal/stats"
)

const syntheticRolloutRel = "sessions/2026/01/02/rollout-2026-01-02T03-04-05Z-synthetic-session.jsonl"

// compressRollout replaces home/relPath with its zstd-compressed .zst
// sibling, as Codex does once a rollout goes idle.
func compressRollout(t *testing.T, home, relPath string) string {
	t.Helper()
	plain := filepath.Join(home, filepath.FromSlash(relPath))
	content, err := os.ReadFile(plain)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	compressed := plain + ".zst"
	writeZstdFile(t, compressed, content)
	if err := os.Remove(plain); err != nil {
		t.Fatalf("remove %s: %v", relPath, err)
	}
	return compressed
}

func writeZstdFile(t *testing.T, path string, content []byte) {
	t.Helper()
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := enc.Write(content); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCodexCompressedRolloutCountsLikePlain(t *testing.T) {
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	plainSrc, _ := newCopiedFixtureSource(t, "valid_home")
	want, err := plainSrc.Overview(ctx, period)
	if err != nil {
		t.Fatalf("plain Overview(all) failed: %v", err)
	}
	if want.Requests == 0 {
		t.Fatal("plain fixture has no requests; the comparison would prove nothing")
	}

	compressedSrc, home := newCopiedFixtureSource(t, "valid_home")
	compressRollout(t, home, syntheticRolloutRel)
	got, err := compressedSrc.Overview(ctx, period)
	if err != nil {
		t.Fatalf("compressed Overview(all) failed: %v", err)
	}

	if got.Sessions != want.Sessions || got.Messages != want.Messages || got.Requests != want.Requests {
		t.Errorf("compressed sessions/messages/requests = %d/%d/%d, want %d/%d/%d",
			got.Sessions, got.Messages, got.Requests, want.Sessions, want.Messages, want.Requests)
	}
	if got.Tokens != want.Tokens {
		t.Errorf("compressed tokens = %+v, want %+v", got.Tokens, want.Tokens)
	}
	if !approxEqual(got.Cost, want.Cost) {
		t.Errorf("compressed cost = %.9f, want %.9f", got.Cost, want.Cost)
	}
}

func TestCodexDiscoveryIncludesCompressedRollouts(t *testing.T) {
	home := copyFixtureHome(t, "valid_home")
	compressed := compressRollout(t, home, syntheticRolloutRel)

	discovered := discoverTranscripts(testContext(t), home)
	if !discovered.available || len(discovered.files) != 1 {
		t.Fatalf("discoverTranscripts = available %v, %d files; want the compressed rollout: %#v", discovered.available, len(discovered.files), discovered.diagnostics)
	}
	file := discovered.files[0]
	if file.Path != compressed {
		t.Errorf("discovered %q, want %q", file.Path, compressed)
	}
	if want := rolloutSessionID(filepath.Base(syntheticRolloutRel)); file.SessionID != want {
		t.Errorf("compressed SessionID = %q, want %q (same as the plain rollout)", file.SessionID, want)
	}
}

func TestCodexDiscoveryPrefersPlainRolloutMidCompression(t *testing.T) {
	home := copyFixtureHome(t, "valid_home")
	plain := filepath.Join(home, filepath.FromSlash(syntheticRolloutRel))
	content, err := os.ReadFile(plain)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	writeZstdFile(t, plain+".zst", content)

	discovered := discoverTranscripts(testContext(t), home)
	if len(discovered.files) != 1 || discovered.files[0].Path != plain {
		t.Fatalf("files = %#v, want only the plain rollout while both copies exist", discovered.files)
	}
	if discovered.diagnostics.ScannedFiles != 1 {
		t.Errorf("ScannedFiles = %d, want 1", discovered.diagnostics.ScannedFiles)
	}
}

func TestCodexUndecodableCompressedRolloutIsSkipped(t *testing.T) {
	home := copyFixtureHome(t, "valid_home")
	broken := filepath.Join(home, "sessions", "2026", "01", "03", "rollout-2026-01-03T00-00-00Z-broken.jsonl.zst")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A zstd magic number followed by garbage, like a rollout truncated
	// mid-compression.
	if err := os.WriteFile(broken, []byte{0x28, 0xb5, 0x2f, 0xfd, 0xff, 0x00, 0x13, 0x37}, 0o644); err != nil {
		t.Fatalf("write broken rollout: %v", err)
	}
	src := New(Options{CodexHome: home, PathSource: "test", PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json")})

	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed on one undecodable rollout: %v", err)
	}
	if overview.Requests == 0 {
		t.Error("Overview().Requests = 0, want the decodable rollout still counted")
	}
	info := src.Info(testContext(t))
	if !strings.Contains(info.Diagnostics.Reason, "could not be decoded") {
		t.Errorf("diagnostics reason = %q, want it to report the undecodable rollout", info.Diagnostics.Reason)
	}
}

func TestLatestRateLimitsReadsCompressedRollout(t *testing.T) {
	rel := "sessions/2026/07/11/rollout-compressed.jsonl"
	home := writeTempCodexHome(t, map[string][]string{
		rel: {rateLimitLine("2026-07-11T11:00:00.000Z", 42.0, 17.0)},
	})
	compressed := compressRollout(t, home, rel)
	recent := time.Now().Add(-time.Hour)
	if err := os.Chtimes(compressed, recent, recent); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	got, err := LatestRateLimits(testContext(t), home)
	if err != nil {
		t.Fatalf("LatestRateLimits failed: %v", err)
	}
	if got == nil || !approxEqual(got.Primary.UsedPercent, 42.0) {
		t.Fatalf("LatestRateLimits = %#v, want primary 42%% from the compressed rollout", got)
	}
}

func TestCodexInfoDeclaresCompressedRolloutLayout(t *testing.T) {
	info := newFixtureSource(t, "valid_home").Info(testContext(t))
	if info.DataLayout != rolloutLayout {
		t.Errorf("Info().DataLayout = %q, want %q so caches built without compressed rollouts rebuild", info.DataLayout, rolloutLayout)
	}
}
