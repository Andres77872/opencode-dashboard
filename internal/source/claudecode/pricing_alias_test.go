package claudecode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func (f *fakePricingAliases) ResolvePricingAlias(sourceID source.SourceID, providerID, modelID string) (source.PricingAliasTarget, bool) {
	return f.target(fakeAliasKey{sourceID: sourceID, providerID: providerID, modelID: modelID})
}

func (f *fakePricingAliases) PricingAliasRevision(sourceID source.SourceID) string {
	return f.revisions[sourceID]
}

func (f *fakePricingAliases) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	aliases := make(map[fakeAliasKey]source.PricingAliasTarget)
	for key := range f.aliases {
		if key.sourceID != sourceID {
			continue
		}
		if target, ok := f.target(key); ok {
			aliases[key] = target
		}
	}
	for key := range f.foreign {
		if key.sourceID != sourceID {
			continue
		}
		if target, ok := f.target(key); ok {
			aliases[key] = target
		}
	}
	return fakePricingAliasSnapshot{
		sourceID: sourceID,
		aliases:  aliases,
		revision: f.revisions[sourceID],
	}
}

type fakePricingAliasSnapshot struct {
	sourceID source.SourceID
	aliases  map[fakeAliasKey]source.PricingAliasTarget
	revision string
}

func (s fakePricingAliasSnapshot) ResolvePricingAlias(providerID, modelID string) (source.PricingAliasTarget, bool) {
	target, ok := s.aliases[fakeAliasKey{sourceID: s.sourceID, providerID: providerID, modelID: modelID}]
	return target, ok
}

func (s fakePricingAliasSnapshot) Revision() string { return s.revision }

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

type sequencedAliasState struct {
	revision  string
	target    string
	onResolve func()
}

type sequencedPricingAliases struct {
	mu        sync.Mutex
	modelID   string
	states    []sequencedAliasState
	captures  int
	liveCalls int
}

func (p *sequencedPricingAliases) ResolvePricingAlias(sourceID source.SourceID, _, _ string) (source.PricingAliasTarget, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.liveCalls++
	return source.PricingAliasTarget{SourceID: sourceID, ModelID: "claude-test-approx"}, true
}

func (p *sequencedPricingAliases) PricingAliasRevision(source.SourceID) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.liveCalls++
	return "live-revision"
}

func (p *sequencedPricingAliases) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	index := p.captures
	if index >= len(p.states) {
		index = len(p.states) - 1
	}
	state := p.states[index]
	p.captures++
	return &sequencedPricingAliasSnapshot{
		sourceID:  sourceID,
		modelID:   p.modelID,
		target:    state.target,
		revision:  state.revision,
		onResolve: state.onResolve,
	}
}

func (p *sequencedPricingAliases) counts() (captures, liveCalls int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.captures, p.liveCalls
}

type sequencedPricingAliasSnapshot struct {
	sourceID  source.SourceID
	modelID   string
	target    string
	revision  string
	onResolve func()
	once      sync.Once
}

func (s *sequencedPricingAliasSnapshot) ResolvePricingAlias(providerID, modelID string) (source.PricingAliasTarget, bool) {
	s.once.Do(func() {
		if s.onResolve != nil {
			s.onResolve()
		}
	})
	if s.sourceID != source.SourceClaudeCode || providerID != "anthropic" || modelID != s.modelID {
		return source.PricingAliasTarget{}, false
	}
	return source.PricingAliasTarget{SourceID: s.sourceID, ModelID: s.target}, true
}

func (s *sequencedPricingAliasSnapshot) Revision() string { return s.revision }

func TestEffectiveClaudePricingUsesOneCapturedAliasSnapshot(t *testing.T) {
	aliases := &sequencedPricingAliases{
		modelID: "captured-alias",
		states: []sequencedAliasState{
			{revision: "capture-one", target: "claude-test-computed"},
			{revision: "capture-two", target: "claude-test-approx"},
		},
	}
	src := New(Options{
		ClaudeHome:          t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})

	pricing := src.loadPricing(testContext(t))
	if got, want := pricing.ID, "anthropic-test-2026-01-02+aliases-capture-one"; got != want {
		t.Fatalf("effective pricing ID = %q, want %q", got, want)
	}
	match := pricing.resolve("anthropic", "captured-alias")
	if match.Kind != source.PricingResolutionUserAlias || match.CanonicalModelID != "claude-test-computed" {
		t.Fatalf("captured alias resolution = %#v", match)
	}
	if captures, liveCalls := aliases.counts(); captures != 1 || liveCalls != 0 {
		t.Fatalf("alias source calls = captures %d, live %d; want one capture and no live calls", captures, liveCalls)
	}
}

func TestClaudePricingAliasesResolutionCatalogAndReportedPrecedence(t *testing.T) {
	const revision = "claude-test-revision"
	aliases := &fakePricingAliases{
		aliases: map[fakeAliasKey]string{
			{source.SourceCodex, "anthropic", "other-source-only"}:                  "claude-test-computed",
			{source.SourceClaudeCode, "anthropic", "custom-claude"}:                 "claude-test-computed",
			{source.SourceClaudeCode, "other", "other-provider-only"}:               "claude-test-computed",
			{source.SourceClaudeCode, "anthropic", "claude-test-computed"}:          "claude-test-approx",
			{source.SourceClaudeCode, "anthropic", "claude-test-computed-20260101"}: "claude-test-approx",
			{source.SourceClaudeCode, "anthropic", "unknown-target-observed"}:       "not-in-catalog",
			{source.SourceClaudeCode, "anthropic", "zero-priced-observed"}:          "claude-test-computed",
			{source.SourceClaudeCode, "anthropic", "zero-family-child"}:             "claude-test-computed",
			{source.SourceClaudeCode, "anthropic", "family-target-observed"}:        "claude-opus-4",
		},
		revisions: map[source.SourceID]string{source.SourceClaudeCode: revision},
	}
	src := New(Options{
		ClaudeHome:          t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})
	ctx := testContext(t)
	pricing := src.loadPricing(ctx)
	wantSnapshotID := "anthropic-test-2026-01-02+aliases-" + revision
	if pricing.ID != wantSnapshotID {
		t.Fatalf("effective pricing ID = %q, want %q", pricing.ID, wantSnapshotID)
	}
	if got := src.Info(ctx).CostPolicy.PricingSnapshotID; got != wantSnapshotID {
		t.Fatalf("Info cost policy pricing ID = %q, want %q", got, wantSnapshotID)
	}

	resolution := src.ResolvePricing(ctx, "ignored-provider", "custom-claude")
	if resolution.ProviderID != "anthropic" || resolution.Kind != source.PricingResolutionUserAlias || resolution.TargetModelID != "claude-test-computed" || resolution.Rate == nil {
		t.Fatalf("custom Claude alias resolution = %#v", resolution)
	}
	if resolution.ModelID != "custom-claude" {
		t.Fatalf("observed model changed to %q", resolution.ModelID)
	}
	if got := src.ResolvePricing(ctx, "other", "other-provider-only"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("non-Anthropic alias resolved for Claude Code: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "anthropic", "other-source-only"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("alias from another source resolved for Claude Code: %#v", got)
	}
	// A user alias outranks even an exact catalog row: the user, not the model
	// name, is the authority on what a proxied model really is.
	if got := src.ResolvePricing(ctx, "anthropic", "claude-test-computed"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "claude-test-approx" || !got.OverridesNative {
		t.Fatalf("user alias did not override native exact pricing: %#v", got)
	}
	// An alias also overrides a family fallback, which is the case it exists
	// for: prefix matching guesses at what a proxied model is.
	if got := src.ResolvePricing(ctx, "anthropic", "claude-test-computed-20260101"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "claude-test-approx" || !got.OverridesNative {
		t.Fatalf("user alias did not override the native family fallback: %#v", got)
	}
	// Without an alias the family fallback still applies.
	if got := src.ResolvePricing(ctx, "anthropic", "claude-test-computed-20270202"); got.Kind != source.PricingResolutionFallback || got.TargetModelID != "claude-test-computed" {
		t.Fatalf("native family fallback did not win without an alias: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "anthropic", "unknown-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("unknown alias target resolution = %#v", got)
	}

	zeroPricing := pricingSnapshot{
		Models: map[string]pricingRate{
			"zero-priced-observed": {},
			"claude-test-computed": pricing.Models["claude-test-computed"],
		},
		aliases: source.CapturePricingAliases(aliases, nil, source.SourceClaudeCode),
	}
	if got := zeroPricing.resolve("anthropic", "zero-priced-observed"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "claude-test-computed" {
		t.Fatalf("unpriced exact model did not consult user alias: %#v", got)
	}
	delete(aliases.aliases, fakeAliasKey{source.SourceClaudeCode, "anthropic", "zero-priced-observed"})
	zeroPricing.aliases = source.CapturePricingAliases(aliases, nil, source.SourceClaudeCode)
	if got := zeroPricing.resolve("anthropic", "zero-priced-observed"); got.Kind != source.PricingResolutionUnpriced || got.CanonicalModelID != "zero-priced-observed" {
		t.Fatalf("unpriced exact model resolution = %#v", got)
	}

	fallbackUnpriced := pricingSnapshot{
		Models: map[string]pricingRate{
			"zero-family": {
				Approximate: true,
			},
			"claude-test-computed": pricing.Models["claude-test-computed"],
		},
		aliases: source.CapturePricingAliases(aliases, nil, source.SourceClaudeCode),
	}
	// The family fallback is unpriced, so the alias is the only thing that can
	// price this model. Nothing usable is displaced, so it is not an override.
	if got := fallbackUnpriced.resolve("anthropic", "zero-family-child"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "claude-test-computed" || got.OverridesNative {
		t.Fatalf("unpriced family fallback did not fall through to the user alias: %#v", got)
	}

	result := computeCost("custom-claude", "anthropic", tokenUsage{Input: 1_000_000, Output: 1_000_000}, true, nil, pricing)
	if result.Status != stats.CostComputed || !approxEqual(result.Cost, 18.0) {
		t.Fatalf("aliased computeCost = %#v, want claude-test-computed pricing", result)
	}
	if result.Provenance == nil || result.Provenance.PricingSnapshotID != wantSnapshotID {
		t.Fatalf("aliased cost provenance = %#v", result.Provenance)
	}
	reported := 0.123
	reportedResult := computeCost("unknown-model", "anthropic", tokenUsage{}, false, &reported, pricing)
	if reportedResult.Status != stats.CostReported || reportedResult.Cost != reported || reportedResult.Provenance == nil || reportedResult.Provenance.PricingSnapshotID != "" {
		t.Fatalf("reported cost precedence changed: %#v", reportedResult)
	}

	catalog := src.PricingCatalog(ctx)
	if catalog.SnapshotID != wantSnapshotID || catalog.SourceID != source.SourceClaudeCode || catalog.Currency != "USD" {
		t.Fatalf("pricing catalog metadata = %#v", catalog)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("fixture catalog models = %#v, want 2 exact entries", catalog.Models)
	}
	for i := 1; i < len(catalog.Models); i++ {
		if strings.Compare(catalog.Models[i-1].ModelID, catalog.Models[i].ModelID) >= 0 {
			t.Fatalf("catalog is not sorted at %q, %q", catalog.Models[i-1].ModelID, catalog.Models[i].ModelID)
		}
	}

	bundled := New(Options{ClaudeHome: t.TempDir(), PricingAliases: aliases})
	if got := bundled.ResolvePricing(ctx, "anthropic", "family-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("user alias resolved through Claude family fallback key: %#v", got)
	}
	bundledPricing := bundled.loadPricing(ctx)
	bundledCatalog := bundled.PricingCatalog(ctx)
	if len(bundledCatalog.Models) == 0 {
		t.Fatal("bundled exact pricing catalog is empty")
	}
	for _, model := range bundledCatalog.Models {
		if bundledPricing.isFamilyFallbackKey(model.ModelID) {
			t.Fatalf("family fallback key %q leaked into exact pricing catalog", model.ModelID)
		}
	}
}

func TestInvalidateDuringClaudeSnapshotLoadRetriesWithFreshAliasCapture(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		name := "full"
		if bounded {
			name = "bounded"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			projectDir := filepath.Join(home, "projects", "-tmp-alias-generation")
			if err := os.MkdirAll(projectDir, 0o755); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			line := fmt.Sprintf(`{"type":"assistant","uuid":"generation-assistant","session_id":"generation-session","timestamp":%q,"cwd":"/tmp/alias-generation","message":{"role":"assistant","model":"generation-alias","content":[{"type":"text","text":"captured aliases stay consistent"}],"usage":{"input_tokens":1000000,"output_tokens":1000000}}}`, now.Format(time.RFC3339Nano))
			if err := os.WriteFile(filepath.Join(projectDir, "generation-session.jsonl"), []byte(line+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			var src *Source
			aliases := &sequencedPricingAliases{
				modelID: "generation-alias",
				states: []sequencedAliasState{
					{
						revision: "generation-one",
						target:   "claude-test-approx",
						onResolve: func() {
							src.Invalidate()
						},
					},
					{revision: "generation-two", target: "claude-test-computed"},
				},
			}
			src = New(Options{
				ClaudeHome:          home,
				PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
				PricingAliases:      aliases,
			})

			query := stats.PeriodQuery{Period: "all"}
			if bounded {
				query = stats.PeriodQuery{FromTime: now.Add(-time.Hour)}
			}
			messages, err := src.Messages(testContext(t), query, 1, 10, stats.DefaultMessageSort())
			if err != nil {
				t.Fatalf("Messages() error = %v", err)
			}
			if messages.Total != 1 {
				t.Fatalf("Messages() total = %d, want 1", messages.Total)
			}
			entry := messages.Messages[0]
			if entry.CostStatus != stats.CostComputed || !approxEqual(entry.Cost, 18) {
				t.Fatalf("retried message cost = %.6f/%q, want 18/computed", entry.Cost, entry.CostStatus)
			}
			wantSnapshotID := "anthropic-test-2026-01-02+aliases-generation-two"
			if entry.CostProvenance == nil || entry.CostProvenance.PricingSnapshotID != wantSnapshotID {
				t.Fatalf("retried cost provenance = %#v, want snapshot %q", entry.CostProvenance, wantSnapshotID)
			}
			if captures, liveCalls := aliases.counts(); captures != 2 || liveCalls != 0 {
				t.Fatalf("alias source calls = captures %d, live %d; want two captures and no live calls", captures, liveCalls)
			}

			src.mu.Lock()
			generation := src.generation
			fullCached := src.snapshot != nil
			boundedCached := src.bounded != nil
			src.mu.Unlock()
			if generation != 1 {
				t.Fatalf("generation = %d, want 1 after one invalidation", generation)
			}
			if bounded {
				if fullCached || !boundedCached {
					t.Fatalf("cache publication after bounded retry: full=%v bounded=%v", fullCached, boundedCached)
				}
			} else if !fullCached || boundedCached {
				t.Fatalf("cache publication after full retry: full=%v bounded=%v", fullCached, boundedCached)
			}
		})
	}
}

func TestClaudePricingResolutionUnavailableAndInvalidateClearsSnapshots(t *testing.T) {
	if got := (pricingSnapshot{}).resolve("anthropic", "model"); got.Kind != source.PricingResolutionUnavailable {
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

// The screenshot case: Claude Code driven through a proxy reports gpt-5.6-sol,
// which no Claude catalog row can price. Borrowing Codex's rates is the only
// way to get a correct cost, and cache creation must use the target's published
// cache-write rate rather than silently costing nothing.
func TestClaudeCrossSourcePricingAlias(t *testing.T) {
	ctx := context.Background()
	const revision = "claude-cross-source-revision"
	aliases := &fakePricingAliases{
		foreign: map[fakeAliasKey]source.PricingAliasTarget{
			{source.SourceClaudeCode, "anthropic", "gpt-5.6-sol"}:    {SourceID: source.SourceCodex, ModelID: "gpt-5.6-sol"},
			{source.SourceClaudeCode, "anthropic", "missing-target"}: {SourceID: source.SourceCodex, ModelID: "not-in-catalog"},
			{source.SourceClaudeCode, "anthropic", "other-currency"}: {SourceID: source.SourceKimiCode, ModelID: "kimi-k3"},
		},
		revisions: map[source.SourceID]string{source.SourceClaudeCode: revision},
	}
	rates := &fakePricingRates{
		models: map[source.SourceID]map[string]source.PricingCatalogModel{
			source.SourceCodex: {
				"gpt-5.6-sol": {ModelID: "gpt-5.6-sol", Rate: source.PricingRateSummary{
					InputPerMillion: 5, CachedInputPerMillion: 0.5, CacheWritePerMillion: 6.25, OutputPerMillion: 30,
				}},
			},
			source.SourceKimiCode: {
				"kimi-k3": {ModelID: "kimi-k3", Rate: source.PricingRateSummary{InputPerMillion: 1, OutputPerMillion: 5}},
			},
		},
		currency: map[source.SourceID]string{source.SourceCodex: "USD", source.SourceKimiCode: "CNY"},
		revision: "catalog-revision",
	}
	src := New(Options{
		ClaudeHome:          t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
		PricingRates:        rates,
	})

	resolution := src.ResolvePricing(ctx, "anthropic", "gpt-5.6-sol")
	if resolution.Kind != source.PricingResolutionUserAlias || resolution.TargetSourceID != source.SourceCodex || resolution.TargetModelID != "gpt-5.6-sol" {
		t.Fatalf("cross-source resolution = %#v", resolution)
	}
	if resolution.Rate == nil || resolution.Rate.InputPerMillion != 5 || resolution.Rate.OutputPerMillion != 30 || resolution.Rate.CachedInputPerMillion != 0.5 {
		t.Fatalf("borrowed rate = %#v, want the Codex rates", resolution.Rate)
	}
	if !strings.Contains(resolution.Note, "codex") {
		t.Errorf("cross-source note does not name the target source: %q", resolution.Note)
	}

	pricing := src.loadPricing(ctx)
	// 1M input at 5.00, 1M cache reads at 0.50, 1M cache creations at 6.25 and
	// 1M output at 30.00. Cache creation is the field a summary-only transfer
	// would have dropped, so it is asserted explicitly.
	result := computeCost("gpt-5.6-sol", "anthropic", tokenUsage{
		Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheCreate: 1_000_000,
	}, true, nil, pricing)
	if !approxEqual(result.Cost, 41.75) {
		t.Fatalf("cross-source cost = %v, want 41.75", result.Cost)
	}
	if result.Status != stats.CostApproximate {
		t.Errorf("cross-source cost status = %q, want approximate", result.Status)
	}
	if result.Provenance == nil || !strings.Contains(result.Provenance.Note, "gpt-5.6-sol") {
		t.Errorf("cross-source provenance = %#v", result.Provenance)
	}

	// A target the other catalog does not have leaves the model unpriced rather
	// than silently falling back to this source's own rates.
	if got := src.ResolvePricing(ctx, "anthropic", "missing-target"); got.Kind != source.PricingResolutionUnknown {
		t.Errorf("missing cross-source target = %#v, want unknown", got)
	}
	// Rates are per-million values in their own currency, so mixing them would
	// produce a meaningless total.
	if got := src.ResolvePricing(ctx, "anthropic", "other-currency"); got.Kind != source.PricingResolutionUnknown {
		t.Errorf("cross-currency target = %#v, want unknown", got)
	}
}

// The bundled catalog other sources borrow from must never fold alias state in;
// that is what keeps cross-source lookups from recursing back through aliases.
func TestClaudeBasePricingCatalogIgnoresAliases(t *testing.T) {
	ctx := context.Background()
	aliases := &fakePricingAliases{
		aliases:   map[fakeAliasKey]string{{source.SourceClaudeCode, "anthropic", "custom"}: "claude-test-computed"},
		revisions: map[source.SourceID]string{source.SourceClaudeCode: "some-revision"},
	}
	src := New(Options{
		ClaudeHome:          t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})

	base := src.BasePricingCatalog(ctx)
	if strings.Contains(base.SnapshotID, "aliases-") {
		t.Fatalf("base catalog snapshot id folds alias state: %q", base.SnapshotID)
	}
	if effective := src.PricingCatalog(ctx); !strings.Contains(effective.SnapshotID, "aliases-") {
		t.Fatalf("effective catalog snapshot id = %q, want alias state folded in", effective.SnapshotID)
	}
	if len(base.Models) != len(src.PricingCatalog(ctx).Models) {
		t.Errorf("base and effective catalogs list different models")
	}
}
