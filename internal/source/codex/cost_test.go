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
	if entry.CostProvenance.PricingSnapshotID != "openai-codex-api-pricing-2026-07-27" {
		t.Errorf("PricingSnapshotID = %q, want openai-codex-api-pricing-2026-07-27", entry.CostProvenance.PricingSnapshotID)
	}
	if !strings.Contains(strings.ToLower(entry.CostProvenance.Note), "api-equivalent") || !strings.Contains(strings.ToLower(entry.CostProvenance.Note), "not actual") {
		t.Errorf("provenance note = %q, want API-equivalent/not actual spend caveat", entry.CostProvenance.Note)
	}
	if entry.Tokens == nil || entry.Tokens.Cache.Write != 0 {
		t.Fatalf("Cache.Write = %#v, want zero/absent Codex cache write tokens", entry.Tokens)
	}
}

func TestCodexFastUsesPriorityUSDForScreenshotRequest(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/07/17/rollout-2026-07-17T21-43-00Z-fast-pricing.jsonl": {
			`{"timestamp":"2026-07-17T21:43:00Z","type":"session_meta","payload":{"id":"fast-pricing","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T21:43:01Z","type":"turn_context","payload":{"turn_id":"pricing-turn","model":"gpt-5.6-sol","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T21:43:02Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}`,
			`{"timestamp":"2026-07-17T21:43:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pricing-turn","info":{"total_token_usage":{"input_tokens":214629,"cached_input_tokens":212736,"output_tokens":1834,"reasoning_output_tokens":1034,"total_tokens":216463}}}}`,
			`{"timestamp":"2026-07-17T21:43:04Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"default"}}}`,
			`{"timestamp":"2026-07-17T21:43:05Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pricing-turn","info":{"total_token_usage":{"input_tokens":429258,"cached_input_tokens":425472,"output_tokens":3668,"reasoning_output_tokens":2068,"total_tokens":432926}}}}`,
		},
	})

	messages := readAllMessages(t, src)
	fast := findMessage(t, messages, func(entry stats.MessageEntry) bool {
		return entry.ID == "codex:fast-pricing:pricing-turn:r0"
	})
	standard := findMessage(t, messages, func(entry stats.MessageEntry) bool {
		return entry.ID == "codex:fast-pricing:pricing-turn:r1"
	})

	if fast.ProcessingMode != stats.ProcessingModeFast || standard.ProcessingMode != stats.ProcessingModeStandard {
		t.Fatalf("fast/standard modes = %q/%q, want %q/%q", fast.ProcessingMode, standard.ProcessingMode, stats.ProcessingModeFast, stats.ProcessingModeStandard)
	}
	if fast.Tokens == nil || *fast.Tokens != (stats.TokenStats{Input: 1_893, Output: 800, Reasoning: 1_034, Cache: stats.CacheStats{Read: 212_736}}) {
		t.Fatalf("fast tokens = %+v, want screenshot's disjoint token buckets", fast.Tokens)
	}
	if !approxEqual(fast.Cost, 0.341706) {
		t.Errorf("Fast/Priority cost = %.9f, want 0.341706 USD", fast.Cost)
	}
	if !approxEqual(standard.Cost, 0.170853) {
		t.Errorf("Standard cost = %.9f, want 0.170853 USD", standard.Cost)
	}
	if fast.CostProvenance == nil || !strings.Contains(fast.CostProvenance.Note, "Priority API") || !strings.Contains(fast.CostProvenance.Note, "Fast was requested") {
		t.Errorf("Fast provenance = %#v, want requested Fast mapped to Priority API rates", fast.CostProvenance)
	}
}

func TestCodexUnknownProcessingTierUsesStandardPricing(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"sessions/2026/07/17/rollout-2026-07-17T13-00-00Z-unknown-pricing.jsonl": {
			`{"timestamp":"2026-07-17T13:00:00Z","type":"session_meta","payload":{"id":"unknown-pricing","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-17T13:00:01Z","type":"turn_context","payload":{"turn_id":"pricing-turn","model":"gpt-5.5","model_provider":"openai"}}`,
			// With no tier marker, the first request remains unknown for classification.
			`{"timestamp":"2026-07-17T13:00:02Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pricing-turn","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":100,"output_tokens":50,"reasoning_output_tokens":25,"total_tokens":1075}}}}`,
			`{"timestamp":"2026-07-17T13:00:03Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"default"}}}`,
			// The cumulative totals produce a second request with exactly the same token delta.
			`{"timestamp":"2026-07-17T13:00:04Z","type":"event_msg","payload":{"type":"token_count","turn_id":"pricing-turn","info":{"total_token_usage":{"input_tokens":2000,"cached_input_tokens":200,"output_tokens":100,"reasoning_output_tokens":50,"total_tokens":2150}}}}`,
		},
	})

	messages := readAllMessages(t, src)
	unknown := findMessage(t, messages, func(entry stats.MessageEntry) bool {
		return entry.ID == "codex:unknown-pricing:pricing-turn:r0"
	})
	standard := findMessage(t, messages, func(entry stats.MessageEntry) bool {
		return entry.ID == "codex:unknown-pricing:pricing-turn:r1"
	})

	if unknown.ProcessingMode != stats.ProcessingModeUnknown {
		t.Errorf("unknown request processing mode = %q, want %q", unknown.ProcessingMode, stats.ProcessingModeUnknown)
	}
	if standard.ProcessingMode != stats.ProcessingModeStandard {
		t.Errorf("explicit request processing mode = %q, want %q", standard.ProcessingMode, stats.ProcessingModeStandard)
	}
	if unknown.ServiceTier != "" || standard.ServiceTier != "default" {
		t.Errorf("unknown/standard raw tiers = %q/%q, want empty/default", unknown.ServiceTier, standard.ServiceTier)
	}
	if unknown.CostStatus != stats.CostEstimatedAPIEquivalent || standard.CostStatus != stats.CostEstimatedAPIEquivalent {
		t.Fatalf("unknown/standard cost statuses = %q/%q, want both %q", unknown.CostStatus, standard.CostStatus, stats.CostEstimatedAPIEquivalent)
	}
	if unknown.Cost <= 0 || standard.Cost <= 0 {
		t.Fatalf("unknown/standard costs = %.9f/%.9f, want non-zero estimates", unknown.Cost, standard.Cost)
	}
	if !approxEqual(unknown.Cost, standard.Cost) {
		t.Errorf("unknown cost = %.9f, standard cost = %.9f; want equal Standard pricing for identical token usage", unknown.Cost, standard.Cost)
	}
	if unknown.Tokens == nil || standard.Tokens == nil || *unknown.Tokens != *standard.Tokens {
		t.Errorf("unknown/standard tokens = %+v/%+v, want identical per-request deltas", unknown.Tokens, standard.Tokens)
	}
	if unknown.CostProvenance == nil || unknown.CostProvenance.Status != stats.CostEstimatedAPIEquivalent {
		t.Fatalf("unknown cost provenance = %#v, want estimated API-equivalent provenance", unknown.CostProvenance)
	}
	note := strings.ToLower(unknown.CostProvenance.Note)
	if !strings.Contains(note, "unknown processing tier") || !strings.Contains(note, "standard pricing") {
		t.Errorf("unknown cost provenance note = %q, want explicit unknown-to-Standard pricing policy", unknown.CostProvenance.Note)
	}
}

func TestBundledCodexPricingCoversCurrentModels(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	tokens := stats.TokenStats{
		Input:     1_000_000,
		Output:    1_000_000,
		Reasoning: 1_000_000,
		Cache:     stats.CacheStats{Read: 1_000_000, Write: 1_000_000},
	}
	tests := []struct {
		model    string
		wantCost float64
	}{
		{model: "gpt-6-astra", wantCost: 123.5},
		{model: "gpt-5.6", wantCost: 49.4},
		{model: "gpt-5.6-sol", wantCost: 49.4},
		{model: "gpt-5.6-terra", wantCost: 28.7},
		{model: "gpt-5.6-luna", wantCost: 2.87},
		{model: "gpt-5.5", wantCost: 65.5},
		{model: "gpt-5.4", wantCost: 32.75},
		{model: "gpt-5.4-mini", wantCost: 9.825},
		{model: "gpt-5.3-codex", wantCost: 29.925},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := computeCost(tt.model, "openai", tokens, 100_000, pricing)
			if result.Status != stats.CostEstimatedAPIEquivalent {
				t.Fatalf("computeCost(%q) status = %q, want %q", tt.model, result.Status, stats.CostEstimatedAPIEquivalent)
			}
			if !approxEqual(result.Cost, tt.wantCost) {
				t.Errorf("computeCost(%q) = %.9f, want %.9f", tt.model, result.Cost, tt.wantCost)
			}
		})
	}
}

func TestBundledCodexPricingCoversPriorityAndFlexTiers(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	tokens := stats.TokenStats{
		Input:     1_000_000,
		Output:    1_000_000,
		Reasoning: 1_000_000,
		Cache:     stats.CacheStats{Read: 1_000_000, Write: 1_000_000},
	}
	tests := []struct {
		model         string
		priorityCost  float64
		flexCost      float64
		flexSupported bool
	}{
		{model: "gpt-6-astra", priorityCost: 247, flexCost: 61.75, flexSupported: true},
		{model: "gpt-5.6", priorityCost: 98.8, flexCost: 24.7, flexSupported: true},
		{model: "gpt-5.6-sol", priorityCost: 98.8, flexCost: 24.7, flexSupported: true},
		{model: "gpt-5.6-terra", priorityCost: 57.4, flexCost: 14.35, flexSupported: true},
		{model: "gpt-5.6-luna", priorityCost: 5.74, flexCost: 1.435, flexSupported: true},
		{model: "gpt-5.5", priorityCost: 163.75, flexCost: 32.75, flexSupported: true},
		{model: "gpt-5.4", priorityCost: 65.5, flexCost: 16.375, flexSupported: true},
		{model: "gpt-5.4-mini", priorityCost: 19.65, flexCost: 4.9125, flexSupported: true},
		{model: "gpt-5.3-codex", priorityCost: 59.85},
		{model: "gpt-5.2-codex", priorityCost: 59.85},
		{model: "gpt-5.1-codex", priorityCost: 42.75},
		{model: "gpt-5.1-codex-max", priorityCost: 42.75},
		{model: "gpt-5-codex", priorityCost: 42.75},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			priority := computeCost(tt.model, "openai", tokens, 100_000, pricing, stats.ProcessingModeFast)
			if priority.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(priority.Cost, tt.priorityCost) {
				t.Errorf("Priority computeCost(%q) = %.9f (%q), want %.9f (%q)", tt.model, priority.Cost, priority.Status, tt.priorityCost, stats.CostEstimatedAPIEquivalent)
			}
			flex := computeCost(tt.model, "openai", tokens, 100_000, pricing, stats.ProcessingModeFlex)
			if !tt.flexSupported {
				if flex.Status != stats.CostMissing || flex.Cost != 0 {
					t.Errorf("Flex computeCost(%q) = %.9f (%q), want missing because no official Flex catalog entry exists", tt.model, flex.Cost, flex.Status)
				}
			} else if flex.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(flex.Cost, tt.flexCost) {
				t.Errorf("Flex computeCost(%q) = %.9f (%q), want %.9f (%q)", tt.model, flex.Cost, flex.Status, tt.flexCost, stats.CostEstimatedAPIEquivalent)
			}
		})
	}
}

func TestCodexPriorityLongContextIsMissingWithoutOfficialRates(t *testing.T) {
	pricing := New(Options{PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json")}).loadPricing(testContext(t))
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.4-mini", "gpt-5.3-codex"} {
		result := computeCost(model, "openai", stats.TokenStats{Input: 272_001, Output: 1}, 272_001, pricing, stats.ProcessingModeFast)
		if result.Status != stats.CostMissing || result.Cost != 0 {
			t.Fatalf("long-context Priority result for %q = %#v, want missing zero-cost estimate", model, result)
		}
		if result.Provenance == nil || !strings.Contains(result.Provenance.Note, "Priority pricing is unavailable") {
			t.Errorf("long-context Priority provenance for %q = %#v, want explicit unavailable-rate reason", model, result.Provenance)
		}
	}
}

func TestGPT56AliasUsesSolPricing(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	tokens := stats.TokenStats{Input: 500_000, Output: 100_000, Cache: stats.CacheStats{Read: 250_000, Write: 50_000}}
	alias := computeCost("gpt-5.6", "openai", tokens, 250_000, pricing)
	sol := computeCost("gpt-5.6-sol", "openai", tokens, 250_000, pricing)
	if !approxEqual(alias.Cost, sol.Cost) {
		t.Errorf("gpt-5.6 cost = %.9f, gpt-5.6-sol cost = %.9f; want equal alias pricing", alias.Cost, sol.Cost)
	}
}

func TestCodexLongContextRulesByModel(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	tokens := stats.TokenStats{Input: 100_000, Output: 10_000, Cache: stats.CacheStats{Read: 20_000, Write: 5_000}}
	tests := []struct {
		model          string
		wantMultiplier bool
	}{
		{model: "gpt-6-astra", wantMultiplier: true},
		{model: "gpt-5.6", wantMultiplier: true},
		{model: "gpt-5.6-sol", wantMultiplier: true},
		{model: "gpt-5.6-terra", wantMultiplier: true},
		{model: "gpt-5.6-luna", wantMultiplier: true},
		{model: "gpt-5.5", wantMultiplier: true},
		{model: "gpt-5.4", wantMultiplier: true},
		{model: "gpt-5.4-mini", wantMultiplier: false},
		{model: "gpt-5.3-codex", wantMultiplier: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			standard := computeCost(tt.model, "openai", tokens, 272_000, pricing)
			long := computeCost(tt.model, "openai", tokens, 272_001, pricing)
			if tt.wantMultiplier && !(long.Cost > standard.Cost) {
				t.Errorf("long-context cost %.9f is not greater than standard cost %.9f", long.Cost, standard.Cost)
			}
			if !tt.wantMultiplier && !approxEqual(long.Cost, standard.Cost) {
				t.Errorf("long-context cost %.9f differs from standard cost %.9f", long.Cost, standard.Cost)
			}
		})
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
		{name: "unpriced Codex Spark preview is missing", model: "gpt-5.3-codex-spark"},
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
