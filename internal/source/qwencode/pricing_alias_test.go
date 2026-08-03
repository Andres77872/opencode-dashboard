package qwencode

import (
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

type fakeSnapshotAliasKey struct {
	providerID string
	modelID    string
}

type fakePricingAliasSnapshot struct {
	aliases  map[fakeSnapshotAliasKey]source.PricingAliasTarget
	revision string
}

func (f *fakePricingAliases) ResolvePricingAlias(sourceID source.SourceID, providerID, modelID string) (source.PricingAliasTarget, bool) {
	return f.target(fakeAliasKey{sourceID: sourceID, providerID: providerID, modelID: modelID})
}

func (f *fakePricingAliases) PricingAliasRevision(sourceID source.SourceID) string {
	return f.revisions[sourceID]
}

func (f *fakePricingAliases) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	snapshot := &fakePricingAliasSnapshot{
		aliases:  make(map[fakeSnapshotAliasKey]source.PricingAliasTarget),
		revision: f.revisions[sourceID],
	}
	collect := func(key fakeAliasKey) {
		if key.sourceID != sourceID {
			return
		}
		if target, ok := f.target(key); ok {
			snapshot.aliases[fakeSnapshotAliasKey{providerID: key.providerID, modelID: key.modelID}] = target
		}
	}
	for key := range f.aliases {
		collect(key)
	}
	for key := range f.foreign {
		collect(key)
	}
	return snapshot
}

func (f *fakePricingAliasSnapshot) ResolvePricingAlias(providerID, modelID string) (source.PricingAliasTarget, bool) {
	target, ok := f.aliases[fakeSnapshotAliasKey{providerID: providerID, modelID: modelID}]
	return target, ok
}

func (f *fakePricingAliasSnapshot) Revision() string { return f.revision }

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

func TestQwenPricingAliasesResolutionAndCatalog(t *testing.T) {
	const revision = "qwen-test-revision"
	aliases := &fakePricingAliases{
		aliases: map[fakeAliasKey]string{
			{source.SourceQwenCode, "qwen", "custom-qwen"}:             "qwen3.7-plus",
			{source.SourceQwenCode, "openai", "custom-qwen"}:           "qwen3.7-max",
			{source.SourceQwenCode, "qwen", "qwen3.7-plus"}:            "qwen3.7-max",
			{source.SourceQwenCode, "qwen", "coder-model"}:             "qwen3-coder-plus",
			{source.SourceQwenCode, "qwen", "bundled-target-observed"}: "coder-model",
			{source.SourceQwenCode, "qwen", "unknown-target-observed"}: "not-in-catalog",
		},
		revisions: map[source.SourceID]string{source.SourceQwenCode: revision},
	}
	src := New(Options{QwenHome: t.TempDir(), PricingAliases: aliases})
	ctx := testContext(t)
	pricing := src.loadPricing(ctx)
	wantSnapshotID := "qwen-modelstudio-pricing-2026-08-02+aliases-" + revision
	if pricing.ID != wantSnapshotID {
		t.Fatalf("effective pricing ID = %q, want %q", pricing.ID, wantSnapshotID)
	}
	if pricing.pricingAliases == nil || pricing.pricingAliasRevision != revision {
		t.Fatalf("captured pricing aliases = %#v revision %q", pricing.pricingAliases, pricing.pricingAliasRevision)
	}
	if got := src.Info(ctx).CostPolicy.PricingSnapshotID; got != wantSnapshotID {
		t.Fatalf("Info cost policy pricing ID = %q, want %q", got, wantSnapshotID)
	}

	resolution := src.ResolvePricing(ctx, "qwen", "custom-qwen")
	if resolution.Kind != source.PricingResolutionUserAlias || resolution.TargetModelID != "qwen3.7-plus" || resolution.Rate == nil {
		t.Fatalf("custom Qwen alias resolution = %#v", resolution)
	}
	if resolution.ModelID != "custom-qwen" {
		t.Fatalf("observed model changed to %q", resolution.ModelID)
	}
	openAI := src.ResolvePricing(ctx, "openai", "custom-qwen")
	if openAI.Kind != source.PricingResolutionUserAlias || openAI.TargetModelID != "qwen3.7-max" {
		t.Fatalf("provider-scoped OpenAI resolution = %#v", openAI)
	}
	if got := src.ResolvePricing(ctx, "other", "custom-qwen"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("alias leaked to another provider: %#v", got)
	}
	// A user alias outranks even an exact catalog row: the user, not the model
	// name, is the authority on what a proxied model really is.
	if got := src.ResolvePricing(ctx, "qwen", "qwen3.7-plus"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "qwen3.7-max" || !got.OverridesNative {
		t.Fatalf("user alias did not override native exact pricing: %#v", got)
	}
	// A user alias also overrides a bundled alias mapping.
	if got := src.ResolvePricing(ctx, "qwen", "coder-model"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "qwen3-coder-plus" || !got.OverridesNative {
		t.Fatalf("user alias did not override the bundled Qwen alias: %#v", got)
	}
	// Without a user alias the bundled mapping still applies.
	if got := src.ResolvePricing(ctx, "other", "coder-model"); got.Kind != source.PricingResolutionNativeAlias || got.TargetModelID != "qwen3.7-max" {
		t.Fatalf("bundled Qwen alias did not win without a user alias: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "qwen", "bundled-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("user alias resolved through bundled alias key: %#v", got)
	}
	if got := src.ResolvePricing(ctx, "qwen", "unknown-target-observed"); got.Kind != source.PricingResolutionUnknown {
		t.Fatalf("unknown alias target resolution = %#v", got)
	}
	result := computeCost("custom-qwen", "qwen", stats.TokenStats{Input: 1_000_000, Output: 1_000_000}, pricing)
	if result.Status != stats.CostEstimatedAPIEquivalent || result.Cost != 2.0 {
		t.Fatalf("aliased computeCost = %#v, want qwen3.7-plus pricing", result)
	}
	if result.Provenance == nil || result.Provenance.PricingSnapshotID != wantSnapshotID {
		t.Fatalf("aliased cost provenance = %#v", result.Provenance)
	}

	catalog := src.PricingCatalog(ctx)
	if catalog.SnapshotID != wantSnapshotID || catalog.SourceID != source.SourceQwenCode || catalog.Currency != "USD" {
		t.Fatalf("pricing catalog metadata = %#v", catalog)
	}
	if len(catalog.Models) != len(pricing.Models) || len(catalog.Models) == 0 {
		t.Fatalf("pricing catalog models = %#v", catalog.Models)
	}
	foundCacheWrite := false
	for i, model := range catalog.Models {
		if i > 0 && strings.Compare(catalog.Models[i-1].ModelID, model.ModelID) >= 0 {
			t.Fatalf("catalog is not sorted at %q, %q", catalog.Models[i-1].ModelID, model.ModelID)
		}
		if model.ModelID == "qwen3.8-max" {
			foundCacheWrite = true
			// The published cache-write price must survive the trip through
			// rateSummary; other sources borrow it by cross-source alias.
			if model.Rate.CacheWritePerMillion != 2.5 {
				t.Fatalf("qwen3.8-max catalog rate = %#v, want cache write 2.5", model.Rate)
			}
		}
	}
	if !foundCacheWrite {
		t.Fatal("qwen3.8-max is missing from the pricing catalog")
	}
}

// The catalog must never invent a price for a listed-but-unpriced model. No
// bundled Qwen row is unpriced today, so this rides on a fixture snapshot.
func TestQwenUnpricedCatalogRowStaysMissingAndAliasable(t *testing.T) {
	const revision = "qwen-unpriced-revision"
	aliases := &fakePricingAliases{
		aliases:   map[fakeAliasKey]string{},
		revisions: map[source.SourceID]string{source.SourceQwenCode: revision},
	}
	src := New(Options{
		QwenHome:            t.TempDir(),
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
		PricingAliases:      aliases,
	})
	ctx := testContext(t)
	pricing := src.loadPricing(ctx)

	got := src.ResolvePricing(ctx, "qwen", "qwen-test-unpriced")
	if got.Kind != source.PricingResolutionUnpriced || got.TargetModelID != "qwen-test-unpriced" || got.Rate != nil {
		t.Fatalf("unpriced exact model resolution = %#v", got)
	}
	cost := computeCost("qwen-test-unpriced", "qwen", stats.TokenStats{Input: 1000, Output: 100}, pricing)
	if cost.Cost != 0 || cost.Status != stats.CostMissing {
		t.Fatalf("unpriced model cost = %#v, want missing rather than guessed", cost)
	}

	// It stays visible in the catalog with zero rates, so the UI can show that
	// the model is known but has no price rather than hiding it.
	catalog := src.PricingCatalog(ctx)
	found := false
	for _, model := range catalog.Models {
		if model.ModelID == "qwen-test-unpriced" {
			found = true
			if model.Rate.InputPerMillion != 0 || model.Rate.OutputPerMillion != 0 {
				t.Fatalf("unpriced catalog entry unexpectedly has rates: %#v", model)
			}
		}
	}
	if !found {
		t.Fatal("unpriced exact catalog entry was omitted")
	}

	// A user alias is still consulted for it, which is how a user prices a model
	// the bundled snapshot cannot.
	aliases.aliases[fakeAliasKey{source.SourceQwenCode, "qwen", "qwen-test-unpriced"}] = "qwen-test-priced"
	repriced := src.ResolvePricing(ctx, "qwen", "qwen-test-unpriced")
	if repriced.Kind != source.PricingResolutionUserAlias || repriced.TargetModelID != "qwen-test-priced" {
		t.Fatalf("unpriced exact model did not consult user alias: %#v", repriced)
	}
}

func fixturePath(t *testing.T, elems ...string) string {
	t.Helper()
	return filepath.Join(append([]string{"testdata"}, elems...)...)
}

func TestQwenPricingSnapshotCapturesAliasesAndRevision(t *testing.T) {
	key := fakeAliasKey{sourceID: source.SourceQwenCode, providerID: "qwen", modelID: "qwen-custom"}
	aliases := &fakePricingAliases{
		aliases:   map[fakeAliasKey]string{key: "qwen3.7-plus"},
		revisions: map[source.SourceID]string{source.SourceQwenCode: "revision-1"},
	}
	src := New(Options{QwenHome: t.TempDir(), PricingAliases: aliases})
	pricing := src.loadPricing(testContext(t))

	aliases.aliases[key] = "qwen3.7-max"
	aliases.revisions[source.SourceQwenCode] = "revision-2"

	oldResult := computeCost("qwen-custom", "qwen", stats.TokenStats{Input: 1_000_000, Output: 1_000_000}, pricing)
	if oldResult.Cost != 2 || pricing.pricingAliasRevision != "revision-1" || !strings.HasSuffix(pricing.ID, "+aliases-revision-1") {
		t.Fatalf("captured pricing changed with resolver mutation: result=%#v revision=%q id=%q", oldResult, pricing.pricingAliasRevision, pricing.ID)
	}

	updated := src.loadPricing(testContext(t))
	updatedResult := computeCost("qwen-custom", "qwen", stats.TokenStats{Input: 1_000_000, Output: 1_000_000}, updated)
	if updatedResult.Cost != 10 || updated.pricingAliasRevision != "revision-2" || !strings.HasSuffix(updated.ID, "+aliases-revision-2") {
		t.Fatalf("new pricing did not capture resolver update: result=%#v revision=%q id=%q", updatedResult, updated.pricingAliasRevision, updated.ID)
	}
}

func TestQwenPricingResolutionUnavailableAndInvalidateClearsSnapshots(t *testing.T) {
	if got := (pricingSnapshot{}).resolve("qwen", "model"); got.Kind != source.PricingResolutionUnavailable {
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
	generation := src.generation
	src.mu.Unlock()

	src.Invalidate()
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.generation != generation+1 {
		t.Fatalf("Invalidate generation = %d, want %d", src.generation, generation+1)
	}
	if src.snapshot != nil || !src.loadedAt.IsZero() || src.bounded != nil || !src.boundedFrom.IsZero() || !src.boundedLoadedAt.IsZero() {
		t.Fatalf("Invalidate left normalized snapshots cached: full=%v loaded=%v bounded=%v from=%v boundedLoaded=%v", src.snapshot, src.loadedAt, src.bounded, src.boundedFrom, src.boundedLoadedAt)
	}
	if src.pricing.ID != pricing.ID {
		t.Fatalf("Invalidate discarded static pricing cache: got %q, want %q", src.pricing.ID, pricing.ID)
	}
}

type blockingPricingAliases struct {
	mu       sync.Mutex
	target   string
	revision string
	block    bool
	started  chan struct{}
	release  chan struct{}
	captures int
}

func (b *blockingPricingAliases) ResolvePricingAlias(source.SourceID, string, string) (source.PricingAliasTarget, bool) {
	panic("mutable pricing alias resolver consulted")
}

func (b *blockingPricingAliases) PricingAliasRevision(source.SourceID) string {
	panic("mutable pricing alias resolver revision consulted")
}

func (b *blockingPricingAliases) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	b.mu.Lock()
	b.captures++
	target := b.target
	revision := b.revision
	block := b.block
	started := b.started
	release := b.release
	b.block = false
	b.mu.Unlock()

	if block {
		close(started)
		<-release
	}
	aliases := make(map[fakeSnapshotAliasKey]source.PricingAliasTarget)
	if sourceID == source.SourceQwenCode && target != "" {
		aliases[fakeSnapshotAliasKey{providerID: "qwen", modelID: "qwen-custom"}] = source.PricingAliasTarget{SourceID: sourceID, ModelID: target}
	}
	return &fakePricingAliasSnapshot{aliases: aliases, revision: revision}
}

func (b *blockingPricingAliases) update(target, revision string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.target = target
	b.revision = revision
}

func (b *blockingPricingAliases) captureCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.captures
}

func TestQwenUnlockedLoadsRetryAfterInvalidation(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		name := "full"
		if bounded {
			name = "bounded"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			writeChat(t, home, "-tmp-project", "retry", []string{
				`{"type":"assistant","uuid":"a1","sessionId":"retry","timestamp":"2026-07-27T10:00:00.000Z","cwd":"/tmp/project","model":"qwen-custom","message":{"role":"model","parts":[{"text":"done"}]},"usageMetadata":{"promptTokenCount":1000000,"candidatesTokenCount":1000000,"cachedContentTokenCount":0,"thoughtsTokenCount":0,"totalTokenCount":2000000}}`,
			})
			aliases := &blockingPricingAliases{
				target:   "qwen3.7-plus",
				revision: "revision-1",
				block:    true,
				started:  make(chan struct{}),
				release:  make(chan struct{}),
			}
			src := New(Options{
				QwenHome:       home,
				PricingAliases: aliases,
				SnapshotTTL:    time.Hour,
				ScanTimeout:    5 * time.Second,
			})
			type loadResult struct {
				snapshot *snapshot
				err      error
			}
			resultCh := make(chan loadResult, 1)
			ctx := testContext(t)
			go func() {
				var snap *snapshot
				var err error
				if bounded {
					snap, err = src.loadBoundedSnapshot(ctx, time.Now().Add(-time.Hour))
				} else {
					snap, err = src.loadSnapshot(ctx)
				}
				resultCh <- loadResult{snapshot: snap, err: err}
			}()

			select {
			case <-aliases.started:
			case <-time.After(5 * time.Second):
				t.Fatal("pricing alias capture did not start")
			}
			aliases.update("qwen3.7-max", "revision-2")
			src.Invalidate()
			close(aliases.release)

			var result loadResult
			select {
			case result = <-resultCh:
			case <-time.After(5 * time.Second):
				t.Fatal("snapshot load did not finish")
			}
			if result.err != nil {
				t.Fatalf("snapshot load: %v", result.err)
			}
			if aliases.captureCount() < 2 {
				t.Fatalf("pricing alias captures = %d, want retry capture", aliases.captureCount())
			}
			if result.snapshot == nil || len(result.snapshot.ordered) != 1 {
				t.Fatalf("snapshot = %#v", result.snapshot)
			}
			entry := result.snapshot.ordered[0].Entry
			if entry.Cost != 10 || entry.CostProvenance == nil || !strings.HasSuffix(entry.CostProvenance.PricingSnapshotID, "+aliases-revision-2") {
				t.Fatalf("retried snapshot used stale pricing: %#v", entry)
			}

			src.mu.Lock()
			defer src.mu.Unlock()
			if src.generation != 1 {
				t.Fatalf("generation = %d, want 1", src.generation)
			}
			if bounded {
				if src.bounded != result.snapshot || src.snapshot != nil {
					t.Fatalf("bounded retry caches: full=%p bounded=%p returned=%p", src.snapshot, src.bounded, result.snapshot)
				}
			} else if src.snapshot != result.snapshot || src.bounded != nil {
				t.Fatalf("full retry caches: full=%p bounded=%p returned=%p", src.snapshot, src.bounded, result.snapshot)
			}
		})
	}
}
