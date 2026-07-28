package codex

import (
	"context"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

type fakeAliasKey struct {
	sourceID   source.SourceID
	providerID string
	modelID    string
}

type fakePricingAliases struct {
	// aliases holds same-source targets keyed by observed model.
	aliases map[fakeAliasKey]string
	// foreign holds targets that borrow another source's catalog.
	foreign   map[fakeAliasKey]source.PricingAliasTarget
	revisions map[source.SourceID]string
}

func (f *fakePricingAliases) target(key fakeAliasKey) (source.PricingAliasTarget, bool) {
	if target, ok := f.foreign[key]; ok {
		return target, true
	}
	if modelID, ok := f.aliases[key]; ok {
		return source.PricingAliasTarget{SourceID: key.sourceID, ModelID: modelID}, true
	}
	return source.PricingAliasTarget{}, false
}

type fakeAliasSnapshotKey struct {
	providerID string
	modelID    string
}

type fakePricingAliasSnapshot struct {
	aliases  map[fakeAliasSnapshotKey]source.PricingAliasTarget
	revision string
}

func (f *fakePricingAliases) ResolvePricingAlias(sourceID source.SourceID, providerID, modelID string) (source.PricingAliasTarget, bool) {
	return f.target(fakeAliasKey{sourceID: sourceID, providerID: providerID, modelID: modelID})
}

func (f *fakePricingAliases) PricingAliasRevision(sourceID source.SourceID) string {
	return f.revisions[sourceID]
}

func (f *fakePricingAliases) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	aliases := make(map[fakeAliasSnapshotKey]source.PricingAliasTarget)
	collect := func(key fakeAliasKey) {
		if key.sourceID != sourceID {
			return
		}
		if target, ok := f.target(key); ok {
			aliases[fakeAliasSnapshotKey{providerID: key.providerID, modelID: key.modelID}] = target
		}
	}
	for key := range f.aliases {
		collect(key)
	}
	for key := range f.foreign {
		collect(key)
	}
	return fakePricingAliasSnapshot{aliases: aliases, revision: f.revisions[sourceID]}
}

func (s fakePricingAliasSnapshot) ResolvePricingAlias(providerID, modelID string) (source.PricingAliasTarget, bool) {
	target, ok := s.aliases[fakeAliasSnapshotKey{providerID: providerID, modelID: modelID}]
	return target, ok
}

func (s fakePricingAliasSnapshot) Revision() string {
	return s.revision
}

// fakePricingRates is a fixed cross-source catalog index: it stands in for the
// bundled catalogs of the other sources without registering them.
type fakePricingRates struct {
	models   map[source.SourceID]map[string]source.PricingCatalogModel
	currency map[source.SourceID]string
	revision string
}

func (f *fakePricingRates) LookupPricingRate(sourceID source.SourceID, modelID string) (source.PricingCatalogModel, source.PricingCatalogMeta, bool) {
	model, ok := f.models[sourceID][modelID]
	if !ok {
		return source.PricingCatalogModel{}, source.PricingCatalogMeta{}, false
	}
	currency := f.currency[sourceID]
	if currency == "" {
		currency = "USD"
	}
	return model, source.PricingCatalogMeta{SourceID: sourceID, SnapshotID: string(sourceID) + "-snapshot", Currency: currency}, true
}

func (f *fakePricingRates) Revision() string { return f.revision }

func TestCodexPricingAliasesResolutionAndCatalog(t *testing.T) {
	const revision = "codex-test-revision"
	aliases := &fakePricingAliases{
		aliases: map[fakeAliasKey]string{
			{source.SourceCodex, "openai", "custom-codex"}:             "gpt-5.2-codex",
			{source.SourceCodex, "azure", "custom-codex"}:              "gpt-5.1-codex-mini",
			{source.SourceClaudeCode, "openai", "other-source-only"}:   "gpt-5.2-codex",
			{source.SourceCodex, "openai", "gpt-5.5"}:                  "gpt-5.1-codex-mini",
			{source.SourceCodex, "openai", "gpt-5.5-2026-01-15"}:       "gpt-5.1-codex-mini",
			{source.SourceCodex, "openai", "invalid-target"}:           "gpt-5.5-2026-01-15",
			{source.SourceCodex, "openai", "zero-priced-observed"}:     "gpt-5.2-codex",
			{source.SourceCodex, "openai", "zero-fallback-2026-01-15"}: "gpt-5.2-codex",
			{source.SourceCodex, "openai", "unknown-target-observed"}:  "not-in-catalog",
		},
		revisions: map[source.SourceID]string{source.SourceCodex: revision},
	}
	src := New(Options{
		CodexHome:           t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})
	ctx := testContext(t)
	pricing := src.loadPricing(ctx)
	wantSnapshotID := "openai-codex-api-pricing-2026-07-27+aliases-" + revision
	if pricing.ID != wantSnapshotID {
		t.Fatalf("effective pricing ID = %q, want %q", pricing.ID, wantSnapshotID)
	}
	if got := src.Info(ctx).CostPolicy.PricingSnapshotID; got != wantSnapshotID {
		t.Fatalf("Info cost policy pricing ID = %q, want %q", got, wantSnapshotID)
	}

	resolution := src.ResolvePricing(ctx, "openai", "custom-codex")
	if resolution.Kind != source.PricingResolutionUserAlias || resolution.TargetModelID != "gpt-5.2-codex" || resolution.Rate == nil {
		t.Fatalf("custom alias resolution = %#v", resolution)
	}
	if resolution.ModelID != "custom-codex" {
		t.Fatalf("observed model changed to %q", resolution.ModelID)
	}
	azure := src.ResolvePricing(ctx, "azure", "custom-codex")
	if azure.Kind != source.PricingResolutionUserAlias || azure.TargetModelID != "gpt-5.1-codex-mini" {
		t.Fatalf("provider-scoped Azure resolution = %#v", azure)
	}
	if got := src.ResolvePricing(ctx, "other", "custom-codex"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("alias leaked to another provider: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "openai", "other-source-only"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("alias leaked from another source: %#v", got)
	}

	// A user alias outranks even an exact catalog row: the user, not the model
	// name, is the authority on what a proxied model really is.
	if got := src.ResolvePricing(ctx, "openai", "gpt-5.5"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "gpt-5.1-codex-mini" || !got.OverridesNative {
		t.Fatalf("user alias did not override native exact pricing: %#v", got)
	}
	// An alias also overrides a date-suffix fallback, which is the case it
	// exists for: suffix stripping guesses at what a proxied model is.
	if got := src.ResolvePricing(ctx, "openai", "gpt-5.5-2026-01-15"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "gpt-5.1-codex-mini" || !got.OverridesNative {
		t.Fatalf("user alias did not override the native date-suffix fallback: %#v", got)
	}
	// Without an alias the date-suffix fallback still applies.
	if got := src.ResolvePricing(ctx, "openai", "gpt-5.5-2027-02-02"); got.Kind != source.PricingResolutionFallback || got.TargetModelID != "gpt-5.5" {
		t.Fatalf("native date-suffix fallback did not win without an alias: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "openai", "invalid-target"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("alias target resolved through a fallback key: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "openai", "unknown-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("unknown alias target resolution = %#v", got)
	}

	zeroPricing := pricingSnapshot{
		Models: map[string]pricingRate{
			"zero-priced-observed": {},
			"zero-fallback":        {},
			"gpt-5.2-codex":        pricing.Models["gpt-5.2-codex"],
		},
		aliases: source.CapturePricingAliases(aliases, nil, source.SourceCodex),
	}
	if got := zeroPricing.resolve("openai", "zero-priced-observed"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "gpt-5.2-codex" {
		t.Fatalf("unpriced exact model did not consult user alias: %#v", got)
	}
	delete(aliases.aliases, fakeAliasKey{source.SourceCodex, "openai", "zero-priced-observed"})
	if got := zeroPricing.resolve("openai", "zero-priced-observed"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "gpt-5.2-codex" {
		t.Fatalf("captured aliases changed with the live resolver: %#v", got)
	}
	zeroPricing.aliases = source.CapturePricingAliases(aliases, nil, source.SourceCodex)
	if got := zeroPricing.resolve("openai", "zero-priced-observed"); got.Kind != source.PricingResolutionUnpriced || got.CanonicalModelID != "zero-priced-observed" {
		t.Fatalf("unpriced exact model resolution after recapture = %#v", got)
	}
	// The date-suffix fallback is unpriced, so the alias is the only thing that
	// can price this model. Nothing usable is displaced, so it is not an override.
	if got := zeroPricing.resolve("openai", "zero-fallback-2026-01-15"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "gpt-5.2-codex" || got.OverridesNative {
		t.Fatalf("unpriced date fallback did not fall through to the user alias: %#v", got)
	}

	result := computeCost("custom-codex", "openai", stats.TokenStats{Input: 1_000_000, Output: 1_000_000}, 100_000, pricing)
	if result.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(result.Cost, 15.75) {
		t.Fatalf("aliased computeCost = %#v, want gpt-5.2-codex standard pricing", result)
	}
	if result.Provenance == nil || result.Provenance.PricingSnapshotID != wantSnapshotID {
		t.Fatalf("aliased cost provenance = %#v", result.Provenance)
	}

	aliasHome := writeTempCodexHome(t, map[string][]string{
		"sessions/2026/07/27/rollout-2026-07-27T12-00-00Z-alias.jsonl": {
			`{"timestamp":"2026-07-27T12:00:00Z","type":"session_meta","payload":{"id":"alias-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-27T12:00:01Z","type":"turn_context","payload":{"turn_id":"alias-turn","model":"custom-codex","model_provider":"openai"}}`,
			`{"timestamp":"2026-07-27T12:00:02Z","type":"event_msg","payload":{"type":"token_count","turn_id":"alias-turn","info":{"total_token_usage":{"input_tokens":1000000,"cached_input_tokens":0,"output_tokens":1000000,"reasoning_output_tokens":0,"total_tokens":2000000}}}}`,
		},
	})
	aliasSource := New(Options{
		CodexHome:           aliasHome,
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})
	entry := findMessage(t, readAllMessages(t, aliasSource), func(entry stats.MessageEntry) bool {
		return entry.Role == "assistant"
	})
	if entry.ModelID != "custom-codex" || entry.ProviderID != "openai" {
		t.Fatalf("aliased entry raw model/provider = %q/%q, want custom-codex/openai", entry.ModelID, entry.ProviderID)
	}
	if entry.CostStatus != stats.CostEstimatedAPIEquivalent || !approxEqual(entry.Cost, 15.75) {
		t.Fatalf("aliased entry cost = %.9f (%q), want 15.75 estimated", entry.Cost, entry.CostStatus)
	}
	if entry.CostProvenance == nil || entry.CostProvenance.PricingSnapshotID != wantSnapshotID || entry.CostProvenance.PricingSource != pricing.Source {
		t.Fatalf("aliased entry provenance = %#v", entry.CostProvenance)
	}

	catalog := src.PricingCatalog(ctx)
	if catalog.SnapshotID != wantSnapshotID || catalog.SourceID != source.SourceCodex || catalog.Currency != "USD" {
		t.Fatalf("pricing catalog metadata = %#v", catalog)
	}
	if len(catalog.Models) == 0 {
		t.Fatal("pricing catalog is empty")
	}
	modelsByID := make(map[string]source.PricingCatalogModel, len(catalog.Models))
	for i, model := range catalog.Models {
		if model.Rate.InputPerMillion <= 0 || model.Rate.OutputPerMillion <= 0 {
			t.Errorf("catalog model %q has unusable rates: %#v", model.ModelID, model.Rate)
		}
		modelsByID[model.ModelID] = model
		if i > 0 && strings.Compare(catalog.Models[i-1].ModelID, model.ModelID) >= 0 {
			t.Fatalf("catalog is not sorted at %q, %q", catalog.Models[i-1].ModelID, model.ModelID)
		}
	}
	if got := modelsByID["gpt-5.2-codex"].Rate; got.InputPerMillion != 1.75 || got.CachedInputPerMillion != 0.175 || got.OutputPerMillion != 14 {
		t.Fatalf("catalog gpt-5.2-codex rate = %#v", got)
	}
	for _, excluded := range []string{"gpt-5.3-codex-spark", "gpt-5-codex-mini"} {
		if _, ok := modelsByID[excluded]; ok {
			t.Errorf("unresolved model %q unexpectedly appears in catalog", excluded)
		}
	}
}

func TestCodexPricingUsesOneImmutableAliasCapture(t *testing.T) {
	const (
		firstRevision  = "first-revision"
		secondRevision = "second-revision"
	)
	key := fakeAliasKey{sourceID: source.SourceCodex, providerID: "openai", modelID: "custom-codex"}
	aliases := &fakePricingAliases{
		aliases:   map[fakeAliasKey]string{key: "gpt-5.2-codex"},
		revisions: map[source.SourceID]string{source.SourceCodex: firstRevision},
	}
	src := New(Options{
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})

	first := src.loadPricing(testContext(t))
	aliases.aliases[key] = "gpt-5.1-codex"
	aliases.revisions[source.SourceCodex] = secondRevision

	if first.ID != "openai-codex-api-pricing-2026-07-27+aliases-"+firstRevision {
		t.Fatalf("first effective pricing ID = %q", first.ID)
	}
	if got := first.resolve("openai", "custom-codex"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "gpt-5.2-codex" {
		t.Fatalf("first captured alias changed with live resolver state: %#v", got)
	}

	second := src.loadPricing(testContext(t))
	if second.ID != "openai-codex-api-pricing-2026-07-27+aliases-"+secondRevision {
		t.Fatalf("second effective pricing ID = %q", second.ID)
	}
	if got := second.resolve("openai", "custom-codex"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "gpt-5.1-codex" {
		t.Fatalf("second captured alias = %#v", got)
	}
	if got := first.resolve("openai", "custom-codex"); got.CanonicalModelID != "gpt-5.2-codex" {
		t.Fatalf("first pricing snapshot was not immutable after recapture: %#v", got)
	}
}

func TestCodexHistoricalPricingRowsHaveOfficialTierAvailability(t *testing.T) {
	pricing := New(Options{PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json")}).loadPricing(testContext(t))
	tests := map[string]struct {
		standard     pricingRate
		priority     *tierPricingRate
		priorityCost float64
	}{
		"gpt-5.2-codex": {
			standard:     pricingRate{InputPerMillion: 1.75, CachedInputPerMillion: 0.175, OutputPerMillion: 14},
			priority:     &tierPricingRate{InputPerMillion: 3.50, CachedInputPerMillion: 0.35, OutputPerMillion: 28},
			priorityCost: 31.85,
		},
		"gpt-5.1-codex": {
			standard:     pricingRate{InputPerMillion: 1.25, CachedInputPerMillion: 0.125, OutputPerMillion: 10},
			priority:     &tierPricingRate{InputPerMillion: 2.50, CachedInputPerMillion: 0.25, OutputPerMillion: 20},
			priorityCost: 22.75,
		},
		"gpt-5.1-codex-max": {
			standard:     pricingRate{InputPerMillion: 1.25, CachedInputPerMillion: 0.125, OutputPerMillion: 10},
			priority:     &tierPricingRate{InputPerMillion: 2.50, CachedInputPerMillion: 0.25, OutputPerMillion: 20},
			priorityCost: 22.75,
		},
		"gpt-5.1-codex-mini": {
			standard: pricingRate{InputPerMillion: 0.25, CachedInputPerMillion: 0.025, OutputPerMillion: 2},
		},
		"gpt-5-codex": {
			standard:     pricingRate{InputPerMillion: 1.25, CachedInputPerMillion: 0.125, OutputPerMillion: 10},
			priority:     &tierPricingRate{InputPerMillion: 2.50, CachedInputPerMillion: 0.25, OutputPerMillion: 20},
			priorityCost: 22.75,
		},
		"codex-mini-latest": {
			standard: pricingRate{InputPerMillion: 1.50, CachedInputPerMillion: 0.375, OutputPerMillion: 6},
		},
	}
	tokens := stats.TokenStats{Input: 1_000_000, Output: 1_000_000, Cache: stats.CacheStats{Read: 1_000_000}}
	for modelID, want := range tests {
		rate, ok := pricing.Models[modelID]
		if !ok {
			t.Errorf("historical model %q missing", modelID)
			continue
		}
		if rate.InputPerMillion != want.standard.InputPerMillion || rate.CachedInputPerMillion != want.standard.CachedInputPerMillion || rate.OutputPerMillion != want.standard.OutputPerMillion {
			t.Errorf("historical model %q Standard rate = %#v, want %#v", modelID, rate, want.standard)
		}
		if rate.Flex != nil {
			t.Errorf("historical model %q unexpectedly has Flex pricing: %#v", modelID, rate.Flex)
		}
		if want.priority == nil {
			if rate.Priority != nil {
				t.Errorf("Standard-only model %q unexpectedly has Priority pricing: %#v", modelID, rate.Priority)
			}
		} else if rate.Priority == nil || *rate.Priority != *want.priority {
			t.Errorf("historical model %q Priority rate = %#v, want %#v", modelID, rate.Priority, want.priority)
		}

		standard := computeCost(modelID, "openai", tokens, 100_000, pricing)
		if standard.Status != stats.CostEstimatedAPIEquivalent || standard.Cost <= 0 {
			t.Errorf("historical model %q Standard cost = %#v", modelID, standard)
		}
		priority := computeCost(modelID, "openai", tokens, 100_000, pricing, stats.ProcessingModeFast)
		if want.priority == nil {
			if priority.Status != stats.CostMissing || priority.Cost != 0 {
				t.Errorf("Standard-only model %q Priority cost = %#v, want missing", modelID, priority)
			}
		} else if priority.Status != stats.CostEstimatedAPIEquivalent || !approxEqual(priority.Cost, want.priorityCost) {
			t.Errorf("historical model %q Priority cost = %#v, want %.3f estimated", modelID, priority, want.priorityCost)
		}
		flex := computeCost(modelID, "openai", tokens, 100_000, pricing, stats.ProcessingModeFlex)
		if flex.Status != stats.CostMissing || flex.Cost != 0 {
			t.Errorf("historical model %q Flex cost = %#v, want missing", modelID, flex)
		}
	}
	for _, modelID := range []string{"gpt-5.3-codex-spark", "gpt-5-codex-mini"} {
		if got := pricing.resolve("openai", modelID); got.Kind != source.PricingResolutionUnknown {
			t.Errorf("excluded model %q unexpectedly resolves: %#v", modelID, got)
		}
	}
}

func TestCodexPricingResolutionUnavailableAndInvalidateClearsSnapshots(t *testing.T) {
	if got := (pricingSnapshot{}).resolve("openai", "model"); got.Kind != source.PricingResolutionUnavailable {
		t.Fatalf("empty catalog resolution = %#v", got)
	}

	src := New(Options{PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json")})
	pricing := src.loadPricing(testContext(t))
	src.mu.Lock()
	src.snapshot = &snapshot{}
	src.loadedAt = time.Now()
	src.bounded = &snapshot{}
	src.boundedFrom = time.Now().Add(-time.Hour)
	src.boundedLoadedAt = time.Now()
	src.mu.Unlock()

	src.Invalidate()
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.generation != 1 {
		t.Fatalf("Invalidate generation = %d, want 1", src.generation)
	}
	if src.snapshot != nil || !src.loadedAt.IsZero() || src.bounded != nil || !src.boundedFrom.IsZero() || !src.boundedLoadedAt.IsZero() {
		t.Fatalf("Invalidate left normalized snapshots cached: full=%v loaded=%v bounded=%v from=%v boundedLoaded=%v", src.snapshot, src.loadedAt, src.bounded, src.boundedFrom, src.boundedLoadedAt)
	}
	if src.pricing.ID != pricing.ID {
		t.Fatalf("Invalidate discarded static pricing cache: got %q, want %q", src.pricing.ID, pricing.ID)
	}
}

// A borrowed catalog publishes one rate per model, not one per OpenAI
// processing tier. Fast and Flex requests must still get a cost, computed at
// the target's standard rate and labelled as such, rather than reporting
// missing purely because the other vendor has no tier concept.
func TestCodexCrossSourceAliasPricesEveryTierAtStandardRates(t *testing.T) {
	ctx := context.Background()
	aliases := &fakePricingAliases{
		foreign: map[fakeAliasKey]source.PricingAliasTarget{
			{source.SourceCodex, "openai", "proxied-claude"}: {SourceID: source.SourceClaudeCode, ModelID: "claude-opus-5"},
		},
		revisions: map[source.SourceID]string{source.SourceCodex: "codex-cross-source-revision"},
	}
	rates := &fakePricingRates{
		models: map[source.SourceID]map[string]source.PricingCatalogModel{
			source.SourceClaudeCode: {
				"claude-opus-5": {ModelID: "claude-opus-5", Rate: source.PricingRateSummary{
					InputPerMillion: 15, CachedInputPerMillion: 1.5, CacheWritePerMillion: 18.75, OutputPerMillion: 75,
				}},
			},
		},
		currency: map[source.SourceID]string{source.SourceClaudeCode: "USD"},
		revision: "catalog-revision",
	}
	src := New(Options{
		CodexHome:           t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
		PricingRates:        rates,
	})
	pricing := src.loadPricing(ctx)

	tokens := stats.TokenStats{Input: 1_000_000, Output: 1_000_000}
	for _, mode := range []stats.ProcessingMode{
		stats.ProcessingModeStandard,
		stats.ProcessingModeFast,
		stats.ProcessingModeFlex,
		stats.ProcessingModeUnknown,
	} {
		result := computeCost("proxied-claude", "openai", tokens, 100_000, pricing, mode)
		if result.Status != stats.CostEstimatedAPIEquivalent {
			t.Errorf("%s cost status = %q, want an estimate rather than a gap", mode, result.Status)
			continue
		}
		if !approxEqual(result.Cost, 90) {
			t.Errorf("%s cost = %v, want the target's standard 15+75 rates", mode, result.Cost)
		}
		if result.Provenance == nil || !strings.Contains(result.Provenance.Note, "claude_code") {
			t.Errorf("%s provenance does not name the borrowed catalog: %#v", mode, result.Provenance)
		}
	}

	// The Priority input-token ceiling is an OpenAI tier rule, so it cannot
	// disqualify a rate borrowed from a vendor that has no such tier.
	above := computeCost("proxied-claude", "openai", tokens, priorityMaxInputTokens+1, pricing, stats.ProcessingModeFast)
	if above.Status != stats.CostEstimatedAPIEquivalent {
		t.Errorf("cross-source Fast above the Priority threshold = %#v, want a cost", above)
	}
}
