package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestCodexCostUsesGPT55EstimatedAPIEquivalentPricing(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	messages := readAllMessages(t, src)
	// The first API request row (r0) carries the first cumulative delta
	// 1000/100/50/25 (raw input/cached/output/reasoning) plus all of the turn's
	// content. Stored buckets are disjoint: Input 900, Cache.Read 100, Output 25,
	// Reasoning 25.
	entry := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 900
	})

	if entry.CostStatus != stats.CostEstimatedAPIEquivalent {
		t.Fatalf("CostStatus = %q, want %q", entry.CostStatus, stats.CostEstimatedAPIEquivalent)
	}
	// Non-cached input at full rate + cached at discounted + (output+reasoning) as output.
	wantCost := (float64(900)*5.0 + float64(100)*0.50 + float64(25+25)*30.0) / 1_000_000
	if !approxEqual(entry.Cost, wantCost) {
		t.Errorf("Cost = %.9f, want %.9f from normal input + discounted cached input + output/reasoning", entry.Cost, wantCost)
	}
	if entry.CostProvenance == nil {
		t.Fatalf("CostProvenance = nil")
	}
	if entry.CostProvenance.Status != stats.CostEstimatedAPIEquivalent {
		t.Errorf("provenance status = %q, want %q", entry.CostProvenance.Status, stats.CostEstimatedAPIEquivalent)
	}
	if entry.CostProvenance.PricingSnapshotID != "openai-codex-gpt-5.5-2026-04-23" {
		t.Errorf("PricingSnapshotID = %q, want openai-codex-gpt-5.5-2026-04-23", entry.CostProvenance.PricingSnapshotID)
	}
	if !strings.Contains(strings.ToLower(entry.CostProvenance.Note), "api-equivalent") || !strings.Contains(strings.ToLower(entry.CostProvenance.Note), "not actual") {
		t.Errorf("provenance note = %q, want API-equivalent/not actual spend caveat", entry.CostProvenance.Note)
	}
	if entry.Tokens == nil || entry.Tokens.Cache.Write != 0 {
		t.Fatalf("Cache.Write = %#v, want zero/absent Codex cache write tokens", entry.Tokens)
	}
}

func TestCodexReasoningTokensBillAsOutputButRemainVisible(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	// Assert on the request row that carries tokens (r0: raw 1000/100/50/25,
	// stored disjoint as Input 900 / Cache.Read 100 / Output 25 / Reasoning 25).
	entry := findMessage(t, readAllMessages(t, src), func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 900
	})
	if entry.Tokens == nil {
		t.Fatalf("Tokens = nil")
	}
	if entry.Tokens.Reasoning != 25 {
		t.Errorf("Reasoning tokens = %d, want 25 visible per-request reasoning tokens", entry.Tokens.Reasoning)
	}
	wantOutputPriced := float64(entry.Tokens.Output+entry.Tokens.Reasoning) * 30.0 / 1_000_000
	if entry.Cost < wantOutputPriced {
		t.Errorf("Cost = %.9f, want at least output+reasoning priced bucket %.9f", entry.Cost, wantOutputPriced)
	}
}

func TestCodexLongContextPricingMultipliers(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T08-00-00Z-long-context.jsonl": {
			`{"timestamp":"2026-01-02T08:00:00Z","type":"session_meta","payload":{"id":"long-context-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-02T08:00:01Z","type":"turn_context","payload":{"turn_id":"long-turn","model":"gpt-5.5","model_provider":"openai","model_context_window":400000}}`,
			`{"timestamp":"2026-01-02T08:00:02Z","type":"event_msg","payload":{"type":"task_started","turn_id":"long-turn"}}`,
			`{"timestamp":"2026-01-02T08:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"long-turn","message":"[REDACTED_LONG_CONTEXT_PROMPT]"}}`,
			`{"timestamp":"2026-01-02T08:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"long-turn","info":{"total_token_usage":{"input_tokens":300000,"cached_input_tokens":100000,"output_tokens":10000,"reasoning_output_tokens":5000,"total_tokens":315000},"rate_limits":{"plan_type":"plus"}}}}`,
			`{"timestamp":"2026-01-02T08:00:05Z","type":"response_item","payload":{"turn_id":"long-turn","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"[REDACTED_LONG_CONTEXT_ASSISTANT]"}]}}}`,
			`{"timestamp":"2026-01-02T08:00:06Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"long-turn","status":"success"}}`,
		},
	})
	// The long-context model API request is its own row (token_count closes it).
	// The long-context threshold compares the raw request input (300000, cached
	// included) even though the stored disjoint Input is 200000.
	entry := findMessage(t, readAllMessages(t, src), func(m stats.MessageEntry) bool {
		return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 200000
	})
	want := ((float64(200000)*5.0 + float64(100000)*0.50) * 2.0 / 1_000_000) + (float64(5000+5000) * 30.0 * 1.5 / 1_000_000)
	if !approxEqual(entry.Cost, want) {
		t.Errorf("long-context cost = %.9f, want %.9f with 2x input/cached and 1.5x output/reasoning multipliers", entry.Cost, want)
	}
}

func TestCodexUnknownMissingOrClaudePricingFallbackRendersMissingCost(t *testing.T) {
	stalePricingPath := writeStaleCodexPricingSnapshot(t)
	tests := []struct {
		name                string
		model               string
		pricingSnapshotPath string
	}{
		{name: "unknown OpenAI model is missing", model: "gpt-unknown"},
		{name: "Claude model never falls back to Anthropic pricing", model: "claude-sonnet-4-6"},
		{name: "missing pricing snapshot is missing", model: "gpt-5.5", pricingSnapshotPath: "testdata/does-not-exist-pricing.json"},
		{name: "stale pricing snapshot is missing", model: "gpt-5.5", pricingSnapshotPath: stalePricingPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := writeTempCodexHome(t, map[string][]string{
				"sessions/2026/01/02/rollout-2026-01-02T09-00-00Z-missing-cost.jsonl": missingCostLines("missing-cost-session", tt.model),
			})
			pricingPath := fixturePath(t, "pricing_snapshot.json")
			if tt.pricingSnapshotPath != "" {
				pricingPath = tt.pricingSnapshotPath
			}
			src := New(Options{CodexHome: home, PathSource: "temp missing-cost fixture", PricingSnapshotPath: pricingPath})
			// The assistant request row that carries usage cannot be priced.
			// (Raw input 1000 with 100 cached stores as disjoint Input 900.)
			entry := findMessage(t, readAllMessages(t, src), func(m stats.MessageEntry) bool {
				return m.Role == "assistant" && m.Tokens != nil && m.Tokens.Input == 900
			})
			if entry.CostStatus != stats.CostMissing {
				t.Errorf("CostStatus = %q, want %q", entry.CostStatus, stats.CostMissing)
			}
			if entry.Cost != 0 {
				t.Errorf("Cost = %.9f, want zero compatibility value for missing cost", entry.Cost)
			}
			if entry.CostProvenance == nil || entry.CostProvenance.MissingCount != 1 {
				t.Errorf("CostProvenance = %#v, want missing count 1", entry.CostProvenance)
			}
		})
	}
}

func writeStaleCodexPricingSnapshot(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stale_pricing_snapshot.json")
	content := `{
  "id": "openai-codex-gpt-5.5-stale-test",
  "retrieved_at": "2020-01-01T00:00:00Z",
  "source": "synthetic stale pricing fixture",
  "currency": "USD",
  "models": {
    "gpt-5.5": {
      "input_per_million": 5.0,
      "cached_input_per_million": 0.5,
      "output_per_million": 30.0,
      "cache_write_input_per_million": 0.0,
      "reasoning_output_billed_as": "output",
      "long_context_threshold_input_tokens": 272000,
      "long_context_input_multiplier": 2.0,
      "long_context_output_multiplier": 1.5
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write stale pricing snapshot: %v", err)
	}
	return path
}

func missingCostLines(sessionID string, model string) []string {
	return []string{
		`{"timestamp":"2026-01-02T09:00:00Z","type":"session_meta","payload":{"id":"` + sessionID + `","model_provider":"openai"}}`,
		`{"timestamp":"2026-01-02T09:00:01Z","type":"turn_context","payload":{"turn_id":"missing-cost-turn","model":"` + model + `","model_provider":"openai"}}`,
		`{"timestamp":"2026-01-02T09:00:02Z","type":"event_msg","payload":{"type":"task_started","turn_id":"missing-cost-turn"}}`,
		`{"timestamp":"2026-01-02T09:00:03Z","type":"event_msg","payload":{"type":"user_message","turn_id":"missing-cost-turn","message":"[REDACTED_MISSING_COST_PROMPT]"}}`,
		`{"timestamp":"2026-01-02T09:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"missing-cost-turn","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":25,"total_tokens":1075},"rate_limits":{"plan_type":"plus"}}}}`,
		`{"timestamp":"2026-01-02T09:00:05Z","type":"response_item","payload":{"turn_id":"missing-cost-turn","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"[REDACTED_MISSING_COST_ASSISTANT]"}]}}}`,
		`{"timestamp":"2026-01-02T09:00:06Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"missing-cost-turn","status":"success"}}`,
	}
}
