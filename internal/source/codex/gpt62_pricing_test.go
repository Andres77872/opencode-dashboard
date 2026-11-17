package codex

import (
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestBundledGPT62CatalogAndResolution(t *testing.T) {
	src := New(Options{})
	ctx := testContext(t)
	want := map[string]source.PricingRateSummary{
		"gpt-6.2-astra": {InputPerMillion: 8, CachedInputPerMillion: 0.4, CacheWritePerMillion: 10, OutputPerMillion: 40},
		"gpt-6.2-sol":   {InputPerMillion: 2, CachedInputPerMillion: 0.1, CacheWritePerMillion: 2.5, OutputPerMillion: 10},
	}
	for _, model := range src.PricingCatalog(ctx).Models {
		rate, ok := want[model.ModelID]
		if !ok {
			continue
		}
		delete(want, model.ModelID)
		got := model.Rate
		if got.InputPerMillion != rate.InputPerMillion || got.CachedInputPerMillion != rate.CachedInputPerMillion ||
			got.CacheWritePerMillion != rate.CacheWritePerMillion || got.OutputPerMillion != rate.OutputPerMillion {
			t.Errorf("%s catalog rates = %#v, want %#v", model.ModelID, got, rate)
		}
	}
	for model := range want {
		t.Errorf("%s missing from the shipped catalog", model)
	}
	for model, kind := range map[string]source.PricingResolutionKind{
		"gpt-6.2-astra": source.PricingResolutionExact,
		"gpt-6.2-sol":   source.PricingResolutionExact,
		// Synthetic date suffixes test the fallback, not announced snapshots.
		"gpt-6.2-astra-2026-11-17": source.PricingResolutionFallback,
		"gpt-6.2-sol-2026-11-17":   source.PricingResolutionFallback,
		"gpt-6.2-astra-pro":        source.PricingResolutionUnknown,
		"gpt-6.2-sol-pro":          source.PricingResolutionUnknown,
		"gpt-6.2-terra":            source.PricingResolutionUnknown,
		"gpt-6.2-luna":             source.PricingResolutionUnknown,
		"gpt-6.2":                  source.PricingResolutionUnknown,
	} {
		got := src.ResolvePricing(ctx, "openai", model)
		if got.Kind != kind {
			t.Errorf("resolution(%q) = %#v, want %s", model, got, kind)
		}
	}
}

func TestGPT62ContextThresholdAndTiers(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	for _, tc := range []struct {
		model string
		input int64
		want  float64
	}{
		{"gpt-6.2-astra", 272_000, 1.3168},
		{"gpt-6.2-astra", 272_001, 2.433616},
		{"gpt-6.2-sol", 272_000, 0.3292},
		{"gpt-6.2-sol", 272_001, 0.608404},
	} {
		tokens := stats.TokenStats{Input: tc.input - 172_000, Output: 8_000, Reasoning: 2_000, Cache: stats.CacheStats{Read: 167_000, Write: 5_000}}
		// Fast is 2x Standard at both context sizes, including above 272K.
		for mode, factor := range map[stats.ProcessingMode]float64{stats.ProcessingModeStandard: 1, stats.ProcessingModeUnknown: 1, stats.ProcessingModeFast: 2} {
			got := computeCost(tc.model, "openai", tokens, tc.input, pricing, mode)
			if got.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(got.Cost, tc.want*factor) {
				t.Errorf("%s/%s at %d input = %#v, want %v", tc.model, mode, tc.input, got, tc.want*factor)
			}
		}
		// OpenAI publishes no Flex rates for GPT-6.2, so Flex stays unpriced.
		if got := computeCost(tc.model, "openai", tokens, tc.input, pricing, stats.ProcessingModeFlex); got.Status != stats.CostMissing || got.Cost != 0 {
			t.Errorf("%s/flex at %d input = %#v, want missing", tc.model, tc.input, got)
		}
	}
}

func TestGPT62PricingAliases(t *testing.T) {
	aliases := &fakePricingAliases{
		aliases: map[fakeAliasKey]string{
			{source.SourceCodex, "openai", "custom-astra-next"}: "gpt-6.2-astra",
			{source.SourceCodex, "openai", "gpt-6.2-sol"}:       "gpt-6.1-sol",
		},
		revisions: map[source.SourceID]string{source.SourceCodex: "gpt62-alias"},
	}
	src := New(Options{PricingAliases: aliases})
	ctx := testContext(t)
	for _, tc := range []struct {
		model, target string
		input, output float64
		overrides     bool
	}{
		{model: "custom-astra-next", target: "gpt-6.2-astra", input: 8, output: 40},
		{model: "gpt-6.2-sol", target: "gpt-6.1-sol", input: 2, output: 10, overrides: true},
	} {
		got := src.ResolvePricing(ctx, "openai", tc.model)
		if got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != tc.target || got.OverridesNative != tc.overrides || got.Rate == nil || got.Rate.InputPerMillion != tc.input || got.Rate.OutputPerMillion != tc.output {
			t.Errorf("resolution(%q) = %#v, want alias to %s with input %v, output %v, overrides native %v", tc.model, got, tc.target, tc.input, tc.output, tc.overrides)
		}
	}
}
