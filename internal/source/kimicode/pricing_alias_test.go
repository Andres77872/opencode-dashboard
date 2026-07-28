package kimicode

import (
	"context"
	"errors"
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

func TestKimiPricingAliasesResolutionAndCatalog(t *testing.T) {
	const revision = "kimi-test-revision"
	aliases := &fakePricingAliases{
		aliases: map[fakeAliasKey]string{
			{source.SourceKimiCode, "kimi", "custom-kimi"}:              "kimi-k2.6",
			{source.SourceKimiCode, "other", "custom-kimi"}:             "kimi-k3",
			{source.SourceKimiCode, "kimi", "kimi-k2.6"}:                "kimi-k3",
			{source.SourceKimiCode, "kimi", "kimi-code/k3"}:             "kimi-k2.6",
			{source.SourceKimiCode, "kimi", "bundled-target-observed"}:  "k3",
			{source.SourceKimiCode, "kimi", "unknown-target-observed"}:  "not-in-catalog",
			{source.SourceKimiCode, "kimi", "unpriced-target-observed"}: "catalog-unpriced",
			{source.SourceKimiCode, "kimi", "zero-priced-observed"}:     "kimi-k2.6",
		},
		revisions: map[source.SourceID]string{source.SourceKimiCode: revision},
	}
	src := New(Options{KimiHome: t.TempDir(), PricingAliases: aliases})
	ctx := testContext(t)
	pricing := src.loadPricing(ctx)
	wantSnapshotID := "kimi-api-pricing-2026-07-16+aliases-" + revision
	if pricing.ID != wantSnapshotID {
		t.Fatalf("effective pricing ID = %q, want %q", pricing.ID, wantSnapshotID)
	}
	if pricing.pricingAliases == nil || pricing.pricingAliasRevision != revision || pricing.pricingAliases.Revision() != revision {
		t.Fatalf("captured pricing aliases = %#v revision %q", pricing.pricingAliases, pricing.pricingAliasRevision)
	}
	if got := src.Info(ctx).CostPolicy.PricingSnapshotID; got != wantSnapshotID {
		t.Fatalf("Info cost policy pricing ID = %q, want %q", got, wantSnapshotID)
	}

	resolution := src.ResolvePricing(ctx, "kimi", "custom-kimi")
	if resolution.Kind != source.PricingResolutionUserAlias || resolution.TargetModelID != "kimi-k2.6" || resolution.Rate == nil {
		t.Fatalf("custom Kimi alias resolution = %#v", resolution)
	}
	if resolution.ProviderID != "kimi" || resolution.ModelID != "custom-kimi" {
		t.Fatalf("observed IDs changed: provider=%q model=%q", resolution.ProviderID, resolution.ModelID)
	}
	other := src.ResolvePricing(ctx, "other", "custom-kimi")
	if other.Kind != source.PricingResolutionUserAlias || other.TargetModelID != "kimi-k3" {
		t.Fatalf("provider-scoped alternate resolution = %#v", other)
	}
	if got := src.ResolvePricing(ctx, "unknown-provider", "custom-kimi"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("alias leaked to another provider: %#v", got)
	}
	// A user alias outranks even an exact catalog row: the user, not the model
	// name, is the authority on what a proxied model really is.
	if got := src.ResolvePricing(ctx, "kimi", "kimi-k2.6"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "kimi-k3" || !got.OverridesNative {
		t.Fatalf("user alias did not override native exact pricing: %#v", got)
	}
	// A user alias also overrides a bundled alias mapping.
	if got := src.ResolvePricing(ctx, "kimi", "kimi-code/k3"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "kimi-k2.6" || !got.OverridesNative {
		t.Fatalf("user alias did not override the bundled Kimi alias: %#v", got)
	}
	// Without a user alias the bundled mapping still applies.
	if got := src.ResolvePricing(ctx, "unknown-provider", "kimi-code/k3"); got.Kind != source.PricingResolutionNativeAlias || got.TargetModelID != "kimi-k3" {
		t.Fatalf("bundled Kimi alias did not win without a user alias: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "kimi", "bundled-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("user alias resolved through bundled alias key: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "kimi", "unknown-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("unknown alias target resolution = %#v", got)
	}
	unpricedTarget := pricingSnapshot{
		Models: map[string]pricingRate{
			"catalog-unpriced": {},
		},
		pricingAliases: source.CapturePricingAliases(aliases, nil, source.SourceKimiCode),
	}
	if got := unpricedTarget.resolve("kimi", "unpriced-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("unpriced alias target resolution = %#v", got)
	}

	zeroPricing := pricingSnapshot{
		Models: map[string]pricingRate{
			"zero-priced-observed": {},
			"kimi-k2.6":            pricing.Models["kimi-k2.6"],
		},
		pricingAliases: source.CapturePricingAliases(aliases, nil, source.SourceKimiCode),
	}
	if got := zeroPricing.resolve("kimi", "zero-priced-observed"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "kimi-k2.6" {
		t.Fatalf("unpriced exact model did not consult user alias: %#v", got)
	}
	delete(aliases.aliases, fakeAliasKey{source.SourceKimiCode, "kimi", "zero-priced-observed"})
	if got := zeroPricing.resolve("kimi", "zero-priced-observed"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "kimi-k2.6" {
		t.Fatalf("captured aliases changed after resolver mutation: %#v", got)
	}
	zeroPricing.pricingAliases = source.CapturePricingAliases(aliases, nil, source.SourceKimiCode)
	if got := zeroPricing.resolve("kimi", "zero-priced-observed"); got.Kind != source.PricingResolutionUnpriced || got.CanonicalModelID != "zero-priced-observed" {
		t.Fatalf("recaptured unpriced exact model resolution = %#v", got)
	}

	tokens := stats.TokenStats{Input: 1_000_000, Output: 1_000_000}
	message := &messageRecord{Entry: stats.MessageEntry{
		Role:       "assistant",
		ProviderID: "kimi",
		ModelID:    "custom-kimi",
		Tokens:     &tokens,
	}}
	message.recomputeCost(pricing)
	if message.Entry.CostStatus != stats.CostEstimatedAPIEquivalent || message.Entry.Cost != 4.95 {
		t.Fatalf("aliased message cost = %#v, want kimi-k2.6 pricing", message.Entry)
	}
	if message.Entry.ProviderID != "kimi" || message.Entry.ModelID != "custom-kimi" {
		t.Fatalf("aliased message changed raw IDs: provider=%q model=%q", message.Entry.ProviderID, message.Entry.ModelID)
	}
	if message.Entry.CostProvenance == nil || message.Entry.CostProvenance.PricingSnapshotID != wantSnapshotID {
		t.Fatalf("aliased cost provenance = %#v", message.Entry.CostProvenance)
	}

	catalog := src.PricingCatalog(ctx)
	if catalog.SnapshotID != wantSnapshotID || catalog.SourceID != source.SourceKimiCode || catalog.Currency != "USD" {
		t.Fatalf("pricing catalog metadata = %#v", catalog)
	}
	if len(catalog.Models) != len(pricing.Models) || len(catalog.Models) == 0 {
		t.Fatalf("pricing catalog models = %#v", catalog.Models)
	}
	var catalogK26 *source.PricingCatalogModel
	for i := range catalog.Models {
		model := &catalog.Models[i]
		if i > 0 && strings.Compare(catalog.Models[i-1].ModelID, model.ModelID) >= 0 {
			t.Fatalf("catalog is not sorted at %q, %q", catalog.Models[i-1].ModelID, model.ModelID)
		}
		if model.ModelID == "kimi-k2.6" {
			catalogK26 = model
		}
	}
	if catalogK26 == nil || catalogK26.DisplayName != "Kimi K2.6" ||
		catalogK26.Rate.InputPerMillion != 0.95 || catalogK26.Rate.CachedInputPerMillion != 0.16 ||
		catalogK26.Rate.OutputPerMillion != 4.00 {
		t.Fatalf("Kimi K2.6 catalog entry = %#v", catalogK26)
	}

	aliases.aliases[fakeAliasKey{source.SourceKimiCode, "kimi", "custom-kimi"}] = "kimi-k3"
	aliases.revisions[source.SourceKimiCode] = "kimi-test-revision-2"
	message.recomputeCost(pricing)
	if message.Entry.Cost != 4.95 || message.Entry.CostProvenance == nil || message.Entry.CostProvenance.PricingSnapshotID != wantSnapshotID {
		t.Fatalf("captured pricing changed after resolver mutation: %#v", message.Entry)
	}
	updated := src.loadPricing(ctx)
	if updated.pricingAliasRevision != "kimi-test-revision-2" || updated.ID != "kimi-api-pricing-2026-07-16+aliases-kimi-test-revision-2" {
		t.Fatalf("recaptured pricing metadata = ID %q revision %q", updated.ID, updated.pricingAliasRevision)
	}
	if got := updated.resolve("kimi", "custom-kimi"); got.Kind != source.PricingResolutionUserAlias || got.CanonicalModelID != "kimi-k3" {
		t.Fatalf("recaptured alias resolution = %#v", got)
	}
}

func TestKimiPricingResolutionUnavailableAndInvalidateClearsSnapshots(t *testing.T) {
	if got := (pricingSnapshot{}).resolve("kimi", "model"); got.Kind != source.PricingResolutionUnavailable {
		t.Fatalf("empty catalog resolution = %#v", got)
	}
	src := New(Options{})
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
	if src.snapshot != nil || !src.loadedAt.IsZero() || src.bounded != nil || !src.boundedFrom.IsZero() || !src.boundedLoadedAt.IsZero() {
		t.Fatalf("Invalidate left normalized snapshots cached: full=%v loaded=%v bounded=%v from=%v boundedLoaded=%v", src.snapshot, src.loadedAt, src.bounded, src.boundedFrom, src.boundedLoadedAt)
	}
	if src.generation != 1 {
		t.Fatalf("Invalidate generation = %d, want 1", src.generation)
	}
	if src.pricing.ID != pricing.ID {
		t.Fatalf("Invalidate discarded static pricing cache: got %q, want %q", src.pricing.ID, pricing.ID)
	}
}

type blockingPricingAliases struct {
	mu            sync.Mutex
	aliases       map[fakeAliasKey]string
	revision      string
	captures      int
	firstCaptured chan struct{}
	releaseFirst  chan struct{}
	releaseOnce   sync.Once
}

func newBlockingPricingAliases(target, revision string) *blockingPricingAliases {
	return &blockingPricingAliases{
		aliases: map[fakeAliasKey]string{
			{sourceID: source.SourceKimiCode, providerID: "kimi", modelID: "custom-kimi"}: target,
		},
		revision:      revision,
		firstCaptured: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
}

func (a *blockingPricingAliases) ResolvePricingAlias(source.SourceID, string, string) (source.PricingAliasTarget, bool) {
	panic("mutable pricing alias resolver consulted")
}

func (a *blockingPricingAliases) PricingAliasRevision(source.SourceID) string {
	panic("mutable pricing alias resolver revision consulted")
}

func (a *blockingPricingAliases) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	a.mu.Lock()
	aliases := make(map[fakeAliasKey]source.PricingAliasTarget)
	for key, target := range a.aliases {
		if key.sourceID == sourceID {
			aliases[key] = source.PricingAliasTarget{SourceID: sourceID, ModelID: target}
		}
	}
	a.captures++
	first := a.captures == 1
	revision := ""
	if sourceID == source.SourceKimiCode {
		revision = a.revision
	}
	a.mu.Unlock()

	if first {
		close(a.firstCaptured)
		<-a.releaseFirst
	}
	return fakePricingAliasSnapshot{sourceID: sourceID, aliases: aliases, revision: revision}
}

func (a *blockingPricingAliases) update(target, revision string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.aliases[fakeAliasKey{sourceID: source.SourceKimiCode, providerID: "kimi", modelID: "custom-kimi"}] = target
	a.revision = revision
}

func (a *blockingPricingAliases) release() {
	a.releaseOnce.Do(func() { close(a.releaseFirst) })
}

func (a *blockingPricingAliases) captureCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.captures
}

type snapshotLoadResult struct {
	snapshot *snapshot
	err      error
}

func TestKimiSnapshotLoadsRetryAfterInvalidate(t *testing.T) {
	loads := []struct {
		name string
		load func(*Source, context.Context) (*snapshot, error)
	}{
		{
			name: "full",
			load: func(src *Source, ctx context.Context) (*snapshot, error) {
				return src.loadSnapshot(ctx)
			},
		},
		{
			name: "bounded",
			load: func(src *Source, ctx context.Context) (*snapshot, error) {
				return src.loadBoundedSnapshot(ctx, time.Now().Add(-time.Hour))
			},
		},
	}

	for _, test := range loads {
		t.Run(test.name, func(t *testing.T) {
			home := writeKimiHome(t, map[string]sessionFixture{
				"generation": {
					State: sessionState{Agents: map[string]agentMeta{"main": {Type: "main"}}},
					Wires: map[string][]string{
						"main": {
							`{"type":"llm.request","kind":"loop","provider":"kimi","model":"custom-kimi","modelAlias":"custom-kimi","turnStep":"0.1","time":1784196001200}`,
							`{"type":"usage.record","model":"custom-kimi","usage":{"inputOther":1000000,"output":1000000},"usageScope":"turn","time":1784196001700}`,
						},
					},
				},
			})
			aliases := newBlockingPricingAliases("kimi-k2.6", "generation-1")
			t.Cleanup(aliases.release)
			src := New(Options{KimiHome: home, PricingAliases: aliases, SnapshotTTL: time.Hour})
			resultCh := make(chan snapshotLoadResult, 1)
			ctx := testContext(t)
			go func() {
				snap, err := test.load(src, ctx)
				resultCh <- snapshotLoadResult{snapshot: snap, err: err}
			}()

			select {
			case <-aliases.firstCaptured:
			case <-time.After(5 * time.Second):
				t.Fatal("pricing alias capture did not start")
			}
			aliases.update("kimi-k3", "generation-2")
			src.Invalidate()
			aliases.release()

			var result snapshotLoadResult
			select {
			case result = <-resultCh:
			case <-time.After(5 * time.Second):
				t.Fatal("snapshot load did not finish")
			}
			if result.err != nil {
				t.Fatalf("snapshot load error: %v", result.err)
			}
			if aliases.captureCount() < 2 {
				t.Fatalf("pricing alias captures = %d, want retry after invalidation", aliases.captureCount())
			}
			if result.snapshot == nil || len(result.snapshot.ordered) != 1 {
				t.Fatalf("loaded snapshot = %#v", result.snapshot)
			}
			message := result.snapshot.ordered[0]
			if message.Entry.Cost != 18 || message.Entry.CostProvenance == nil ||
				message.Entry.CostProvenance.PricingSnapshotID != "kimi-api-pricing-2026-07-16+aliases-generation-2" {
				t.Fatalf("retried message pricing = %#v", message.Entry)
			}

			src.mu.Lock()
			defer src.mu.Unlock()
			if src.generation != 1 {
				t.Fatalf("generation = %d, want 1", src.generation)
			}
			switch test.name {
			case "full":
				if src.snapshot != result.snapshot || src.bounded != nil {
					t.Fatalf("full cache publication = full %p bounded %p, returned %p", src.snapshot, src.bounded, result.snapshot)
				}
			case "bounded":
				if src.bounded != result.snapshot || src.snapshot != nil {
					t.Fatalf("bounded cache publication = bounded %p full %p, returned %p", src.bounded, src.snapshot, result.snapshot)
				}
			}
		})
	}
}

func TestKimiSnapshotRetryKeepsOriginalContext(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"context": {
			State: sessionState{Agents: map[string]agentMeta{"main": {Type: "main"}}},
			Wires: map[string][]string{
				"main": {
					`{"type":"usage.record","model":"custom-kimi","usage":{"inputOther":1,"output":1},"usageScope":"turn","time":1784196001700}`,
				},
			},
		},
	})
	aliases := newBlockingPricingAliases("kimi-k2.6", "generation-1")
	t.Cleanup(aliases.release)
	src := New(Options{KimiHome: home, PricingAliases: aliases, SnapshotTTL: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan snapshotLoadResult, 1)
	go func() {
		snap, err := src.loadSnapshot(ctx)
		resultCh <- snapshotLoadResult{snapshot: snap, err: err}
	}()

	select {
	case <-aliases.firstCaptured:
	case <-time.After(5 * time.Second):
		t.Fatal("pricing alias capture did not start")
	}
	src.Invalidate()
	cancel()
	aliases.release()

	select {
	case result := <-resultCh:
		if !errors.Is(result.err, context.Canceled) || result.snapshot != nil {
			t.Fatalf("canceled retry = snapshot %p error %v", result.snapshot, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled snapshot load did not finish")
	}
	if aliases.captureCount() != 1 {
		t.Fatalf("pricing alias captures after cancellation = %d, want 1", aliases.captureCount())
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.snapshot != nil || src.bounded != nil {
		t.Fatalf("canceled retry published caches: full %p bounded %p", src.snapshot, src.bounded)
	}
}
