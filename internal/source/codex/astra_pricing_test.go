package codex

import (
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestBundledAstraCatalogAndResolution(t *testing.T) {
	src := New(Options{})
	ctx := testContext(t)
	catalog := src.PricingCatalog(ctx)
	if catalog.SnapshotID != "openai-codex-api-pricing-2026-09-05" || catalog.Currency != "USD" {
		t.Fatalf("catalog metadata = %#v", catalog)
	}
	found := false
	for _, model := range catalog.Models {
		if model.ModelID != "gpt-6-astra" {
			continue
		}
		found = true
		if model.Rate.InputPerMillion != 10 || model.Rate.CachedInputPerMillion != 1 || model.Rate.CacheWritePerMillion != 12.5 || model.Rate.OutputPerMillion != 50 {
			t.Fatalf("Astra catalog rates = %#v", model.Rate)
		}
	}
	if !found {
		t.Fatal("Astra missing from the shipped catalog")
	}
	for model, kind := range map[string]source.PricingResolutionKind{
		"gpt-6-astra":            source.PricingResolutionExact,
		"gpt-6-astra-2026-09-05": source.PricingResolutionFallback,
		"gpt-6-astra-pro":        source.PricingResolutionUnknown,
	} {
		got := src.ResolvePricing(ctx, "openai", model)
		if got.Kind != kind {
			t.Errorf("resolution(%q) = %#v, want %s", model, got, kind)
		}
	}
}

func TestBundledFastLongContextRates(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	// Each disjoint bucket contributes independently, including reasoning and
	// cache writes. Long-context multipliers apply to the entire request.
	tokens := stats.TokenStats{Input: 1_000_000, Output: 1_000_000, Reasoning: 1_000_000, Cache: stats.CacheStats{Read: 1_000_000, Write: 1_000_000}}
	for model, standardLong := range map[string]float64{
		"gpt-6-astra": 197, "gpt-5.6": 78.8, "gpt-5.6-sol": 78.8,
		"gpt-5.6-terra": 45.4, "gpt-5.6-luna": 4.54,
	} {
		for mode, factor := range map[stats.ProcessingMode]float64{
			stats.ProcessingModeStandard: 1, stats.ProcessingModeUnknown: 1,
			stats.ProcessingModeFast: 2, stats.ProcessingModeFlex: 0.5,
		} {
			got := computeCost(model, "openai", tokens, 272_001, pricing, mode)
			if got.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(got.Cost, standardLong*factor) {
				t.Errorf("%s/%s long-context cost = %#v, want %v", model, mode, got, standardLong*factor)
			}
		}
	}
	// Models without published long-context Fast rates must stay unpriced.
	for _, model := range []string{"gpt-5.5", "gpt-5.4", "gpt-5.3-codex"} {
		got := computeCost(model, "openai", tokens, 272_001, pricing, stats.ProcessingModeFast)
		if got.Status != stats.CostMissing {
			t.Errorf("%s unexpectedly has long-context Fast pricing: %#v", model, got)
		}
	}
}

func TestAstraContextThresholdAndAlias(t *testing.T) {
	aliases := &fakePricingAliases{
		aliases:   map[fakeAliasKey]string{{source.SourceCodex, "openai", "custom-astra"}: "gpt-6-astra"},
		revisions: map[source.SourceID]string{source.SourceCodex: "astra-alias"},
	}
	pricing := New(Options{PricingAliases: aliases}).loadPricing(testContext(t))
	for _, model := range []string{"gpt-6-astra", "custom-astra"} {
		for _, tc := range []struct {
			input int64
			want  float64
		}{{272_000, 1.7295}, {272_001, 3.20902}} {
			tokens := stats.TokenStats{Input: tc.input - 172_000, Output: 8_000, Reasoning: 2_000, Cache: stats.CacheStats{Read: 167_000, Write: 5_000}}
			for mode, factor := range map[stats.ProcessingMode]float64{stats.ProcessingModeStandard: 1, stats.ProcessingModeFast: 2, stats.ProcessingModeFlex: 0.5} {
				got := computeCost(model, "openai", tokens, tc.input, pricing, mode)
				if got.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(got.Cost, tc.want*factor) {
					t.Errorf("%s/%s at %d input = %#v, want %v", model, mode, tc.input, got, tc.want*factor)
				}
			}
		}
	}
}

func TestAstraRolloutUsesPerRequestContextAndProcessingTier(t *testing.T) {
	home := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/09/05/rollout-2026-09-05T12-00-00Z-astra.jsonl": {
			`{"timestamp":"2026-09-05T12:00:00Z","type":"session_meta","payload":{"id":"astra-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-09-05T12:00:01Z","type":"turn_context","payload":{"turn_id":"astra-turn","model":"gpt-6-astra","model_provider":"openai"}}`,
			`{"timestamp":"2026-09-05T12:00:02Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"fast"}}}`,
			`{"timestamp":"2026-09-05T12:00:03Z","type":"event_msg","payload":{"type":"token_count","turn_id":"astra-turn","info":{"total_token_usage":{"input_tokens":300000,"cached_input_tokens":200000,"output_tokens":10000,"reasoning_output_tokens":4000,"total_tokens":310000}}}}`,
			`{"timestamp":"2026-09-05T12:00:04Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"default"}}}`,
			`{"timestamp":"2026-09-05T12:00:05Z","type":"event_msg","payload":{"type":"token_count","turn_id":"astra-turn","info":{"total_token_usage":{"input_tokens":301000,"cached_input_tokens":200100,"output_tokens":10050,"reasoning_output_tokens":4025,"total_tokens":311050}}}}`,
		},
	})
	src := New(Options{CodexHome: home})
	messages := readAllMessages(t, src)
	for _, tc := range []struct {
		id   string
		mode stats.ProcessingMode
		cost float64
	}{
		{"codex:astra-session:astra-turn:r0", stats.ProcessingModeFast, 6.3},
		{"codex:astra-session:astra-turn:r1", stats.ProcessingModeStandard, 0.0116},
	} {
		entry := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.ID == tc.id })
		if entry.ModelID != "gpt-6-astra" || entry.ProcessingMode != tc.mode || entry.CostStatus != stats.CostEstimatedAPIEquivalent || !approxEqual(entry.Cost, tc.cost) {
			t.Errorf("request %s = %#v, want Astra/%s cost %v", tc.id, entry, tc.mode, tc.cost)
		}
		if entry.CostProvenance == nil || entry.CostProvenance.PricingSnapshotID != "openai-codex-api-pricing-2026-09-05" {
			t.Errorf("request %s provenance = %#v", tc.id, entry.CostProvenance)
		}
	}
}
