package codex

import (
	"context"
	"os"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// TestCodexCorpusTotals is an opt-in regression check against a real (or
// sanitized) ~/.codex directory. Point OPENCODE_DASHBOARD_CODEX_CORPUS at the
// directory to run it; expected totals were computed independently from the
// corpus with a reference implementation of per-thread cumulative deltas plus
// fork/resume replay detection:
//
//	go test ./internal/source/codex/ -run TestCodexCorpusTotals -v
//
// Expected values below match the sanitized corpus snapshot of 2026-07-10
// (289 rollout files, 2026-05-30..2026-07-10). A different corpus needs
// different expectations — override them with the OPENCODE_DASHBOARD_CODEX_
// CORPUS_{INPUT,CACHE_READ,OUTPUT,REASONING} variables if needed.
func TestCodexCorpusTotals(t *testing.T) {
	home := os.Getenv("OPENCODE_DASHBOARD_CODEX_CORPUS")
	if home == "" {
		t.Skip("set OPENCODE_DASHBOARD_CODEX_CORPUS to a .codex directory to run the corpus regression check")
	}

	src := New(Options{CodexHome: home, PathSource: "corpus", ScanTimeout: 5 * time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}

	// Input/Output include a small clamp-floor component (125,532 / 1,520 in
	// this corpus): at rare mid-ladder counter regressions the per-field
	// positive delta can carry cached > input (or reasoning > output), and the
	// disjoint mapping floors the difference at zero rather than inventing
	// negative buckets. Verified equal to an independent reference
	// implementation of the same semantics, event for event.
	want := struct{ input, cacheRead, output, reasoning int64 }{
		input:     envInt64(t, "OPENCODE_DASHBOARD_CODEX_CORPUS_INPUT", 128_153_717),
		cacheRead: envInt64(t, "OPENCODE_DASHBOARD_CODEX_CORPUS_CACHE_READ", 3_622_749_568),
		output:    envInt64(t, "OPENCODE_DASHBOARD_CODEX_CORPUS_OUTPUT", 9_728_023),
		reasoning: envInt64(t, "OPENCODE_DASHBOARD_CODEX_CORPUS_REASONING", 4_439_370),
	}
	got := overview.Tokens
	t.Logf("corpus totals: input=%d cache_read=%d output=%d reasoning=%d cache_write=%d sessions=%d messages=%d",
		got.Input, got.Cache.Read, got.Output, got.Reasoning, got.Cache.Write, overview.Sessions, overview.Messages)
	if got.Input != want.input || got.Cache.Read != want.cacheRead || got.Output != want.output || got.Reasoning != want.reasoning {
		t.Errorf("corpus totals = input/cache_read/output/reasoning %d/%d/%d/%d, want %d/%d/%d/%d",
			got.Input, got.Cache.Read, got.Output, got.Reasoning, want.input, want.cacheRead, want.output, want.reasoning)
	}
	if got.Cache.Write != 0 {
		t.Errorf("cache_write = %d, want 0 (Codex rollouts report no cache writes)", got.Cache.Write)
	}
}

func envInt64(t *testing.T, name string, fallback int64) int64 {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	var value int64
	for _, r := range raw {
		if r < '0' || r > '9' {
			t.Fatalf("%s = %q, want a decimal integer", name, raw)
		}
		value = value*10 + int64(r-'0')
	}
	return value
}
