package codex

import (
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestBundledGPT6CatalogAndResolution(t *testing.T) {
	src := New(Options{})
	ctx := testContext(t)
	want := map[string]source.PricingRateSummary{
		"gpt-6.1-sol":   {InputPerMillion: 2, CachedInputPerMillion: 0.1, CacheWritePerMillion: 2.5, OutputPerMillion: 10},
		"gpt-6.1-terra": {InputPerMillion: 1, CachedInputPerMillion: 0.05, CacheWritePerMillion: 1.25, OutputPerMillion: 5},
		"gpt-6-sol":     {InputPerMillion: 2, CachedInputPerMillion: 0.2, CacheWritePerMillion: 2.5, OutputPerMillion: 10},
		"gpt-6-luna":    {InputPerMillion: 0.1, CachedInputPerMillion: 0.01, CacheWritePerMillion: 0.125, OutputPerMillion: 0.5},
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
		"gpt-6.1-sol":            source.PricingResolutionExact,
		"gpt-6.1-terra":          source.PricingResolutionExact,
		"gpt-6-sol":              source.PricingResolutionExact,
		"gpt-6-luna":             source.PricingResolutionExact,
		"gpt-6.1-sol-2026-09-29": source.PricingResolutionFallback,
		// Synthetic date suffix tests the fallback, not an announced snapshot.
		"gpt-6.1-terra-2026-10-26": source.PricingResolutionFallback,
		"gpt-6-sol-2026-09-22":     source.PricingResolutionFallback,
		"gpt-6-luna-2026-09-22":    source.PricingResolutionFallback,
		"gpt-6-sol-pro":            source.PricingResolutionUnknown,
		"gpt-6.1-terra-pro":        source.PricingResolutionUnknown,
		"gpt-6":                    source.PricingResolutionUnknown,
		"gpt-6.1":                  source.PricingResolutionUnknown,
	} {
		got := src.ResolvePricing(ctx, "openai", model)
		if got.Kind != kind {
			t.Errorf("resolution(%q) = %#v, want %s", model, got, kind)
		}
	}
}

func TestGPT6ContextThreshold(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	for _, tc := range []struct {
		model string
		input int64
		want  float64
	}{
		{"gpt-6.1-sol", 272_000, 0.3292},
		{"gpt-6.1-sol", 272_001, 0.608404},
		{"gpt-6.1-terra", 272_000, 0.1646},
		{"gpt-6.1-terra", 272_001, 0.304202},
		{"gpt-6-sol", 272_000, 0.3459},
		{"gpt-6-sol", 272_001, 0.641804},
		{"gpt-6-luna", 272_000, 0.017295},
		{"gpt-6-luna", 272_001, 0.0320902},
	} {
		tokens := stats.TokenStats{Input: tc.input - 172_000, Output: 8_000, Reasoning: 2_000, Cache: stats.CacheStats{Read: 167_000, Write: 5_000}}
		for mode, factor := range map[stats.ProcessingMode]float64{stats.ProcessingModeStandard: 1, stats.ProcessingModeFast: 2, stats.ProcessingModeFlex: 0.5} {
			got := computeCost(tc.model, "openai", tokens, tc.input, pricing, mode)
			if got.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(got.Cost, tc.want*factor) {
				t.Errorf("%s/%s at %d input = %#v, want %v", tc.model, mode, tc.input, got, tc.want*factor)
			}
		}
	}
}

func TestGPT61TerraPricingAliases(t *testing.T) {
	aliases := &fakePricingAliases{
		aliases: map[fakeAliasKey]string{
			{source.SourceCodex, "openai", "custom-terra"}:  "gpt-6.1-terra",
			{source.SourceCodex, "openai", "gpt-6.1-terra"}: "gpt-6.1-sol",
		},
		revisions: map[source.SourceID]string{source.SourceCodex: "terra-alias"},
	}
	src := New(Options{PricingAliases: aliases})
	ctx := testContext(t)
	for _, tc := range []struct {
		model, target string
		input, output float64
		overrides     bool
	}{
		{model: "custom-terra", target: "gpt-6.1-terra", input: 1, output: 5},
		{model: "gpt-6.1-terra", target: "gpt-6.1-sol", input: 2, output: 10, overrides: true},
	} {
		got := src.ResolvePricing(ctx, "openai", tc.model)
		if got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != tc.target || got.OverridesNative != tc.overrides || got.Rate == nil || got.Rate.InputPerMillion != tc.input || got.Rate.OutputPerMillion != tc.output {
			t.Errorf("resolution(%q) = %#v, want alias to %s with input %v, output %v, overrides native %v", tc.model, got, tc.target, tc.input, tc.output, tc.overrides)
		}
	}
}
