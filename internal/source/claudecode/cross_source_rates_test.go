package claudecode

import (
	"context"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/codex"
)

type staticAliases struct {
	targets map[string]source.PricingAliasTarget
}

func (s staticAliases) ResolvePricingAlias(_ source.SourceID, _, modelID string) (source.PricingAliasTarget, bool) {
	t, ok := s.targets[modelID]
	return t, ok
}
func (s staticAliases) PricingAliasRevision(source.SourceID) string { return "rev1" }

// TestCrossSourceAliasUsesUpdatedCatalogRates drives the same wiring main.go
// builds: a Claude Code alias borrowing rates from the Codex catalog through
// the registry-backed catalog index.
func TestCrossSourceAliasUsesUpdatedCatalogRates(t *testing.T) {
	ctx := context.Background()
	aliases := staticAliases{targets: map[string]source.PricingAliasTarget{
		"proxy-astra": {SourceID: source.SourceCodex, ModelID: "gpt-6-astra"},
		"proxy-terra": {SourceID: source.SourceCodex, ModelID: "gpt-5.6-terra"},
		"proxy-luna":  {SourceID: source.SourceCodex, ModelID: "gpt-5.6-luna"},
		"proxy-sol":   {SourceID: source.SourceCodex, ModelID: "gpt-5.6-sol"},
	}}
	index := source.NewCatalogIndex()
	claude := New(Options{ClaudeHome: t.TempDir(), PricingAliases: aliases, PricingRates: index})
	cdx := codex.New(codex.Options{CodexHome: t.TempDir(), PricingAliases: aliases, PricingRates: index})
	registry := source.NewRegistry(source.SourceClaudeCode)
	if err := registry.Register(claude); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cdx); err != nil {
		t.Fatal(err)
	}
	index.Bind(registry)

	want := map[string]struct{ in, cached, write, out float64 }{
		"proxy-astra": {10.0, 1.0, 12.5, 50.0},
		"proxy-sol":   {4.0, 0.4, 5.0, 20.0},
		"proxy-terra": {2.0, 0.2, 2.5, 12.0},
		"proxy-luna":  {0.2, 0.02, 0.25, 1.2},
	}
	for model, w := range want {
		res := claude.ResolvePricing(ctx, "anthropic", model)
		if res.Kind != source.PricingResolutionUserAlias {
			t.Errorf("%s: kind = %q, want user_alias", model, res.Kind)
			continue
		}
		if res.TargetSourceID != source.SourceCodex {
			t.Errorf("%s: target source = %q, want codex", model, res.TargetSourceID)
		}
		if res.Rate == nil {
			t.Errorf("%s: no rate resolved", model)
			continue
		}
		got := *res.Rate
		if got.InputPerMillion != w.in || got.CachedInputPerMillion != w.cached ||
			got.CacheWritePerMillion != w.write || got.OutputPerMillion != w.out {
			t.Errorf("%s rates = in %v / cached %v / write %v / out %v, want %v / %v / %v / %v",
				model, got.InputPerMillion, got.CachedInputPerMillion, got.CacheWritePerMillion, got.OutputPerMillion,
				w.in, w.cached, w.write, w.out)
		}
		t.Logf("%-12s -> codex/%-14s in=%-5v cached=%-5v write=%-5v out=%-5v  1h-cache=%v",
			model, res.TargetModelID, got.InputPerMillion, got.CachedInputPerMillion,
			got.CacheWritePerMillion, got.OutputPerMillion, foreignRate(got).cacheCreate1hPerMillion())
	}

	catalog := cdx.BasePricingCatalog(ctx)
	t.Logf("codex catalog snapshot: %s (%d models)", catalog.SnapshotID, len(catalog.Models))
}
