package codex

import (
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestBundledGPT6SolAndLunaCatalogAndResolution(t *testing.T) {
	src := New(Options{})
	ctx := testContext(t)
	want := map[string]source.PricingRateSummary{
		"gpt-6-sol":  {InputPerMillion: 2, CachedInputPerMillion: 0.2, CacheWritePerMillion: 2.5, OutputPerMillion: 10},
		"gpt-6-luna": {InputPerMillion: 0.1, CachedInputPerMillion: 0.01, CacheWritePerMillion: 0.125, OutputPerMillion: 0.5},
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
		"gpt-6-sol":             source.PricingResolutionExact,
		"gpt-6-luna":            source.PricingResolutionExact,
		"gpt-6-sol-2026-09-22":  source.PricingResolutionFallback,
		"gpt-6-luna-2026-09-22": source.PricingResolutionFallback,
		"gpt-6-sol-pro":         source.PricingResolutionUnknown,
		"gpt-6":                 source.PricingResolutionUnknown,
	} {
		got := src.ResolvePricing(ctx, "openai", model)
		if got.Kind != kind {
			t.Errorf("resolution(%q) = %#v, want %s", model, got, kind)
		}
	}
}

func TestGPT6SolAndLunaContextThreshold(t *testing.T) {
	pricing := New(Options{}).loadPricing(testContext(t))
	for _, tc := range []struct {
		model string
		input int64
		want  float64
	}{
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
