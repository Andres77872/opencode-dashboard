package source

import (
	"context"
	"strings"
	"testing"
	"time"
)

type testPricingAliasResolver struct {
	revisions map[SourceID]string
}

func (r testPricingAliasResolver) ResolvePricingAlias(SourceID, string, string) (PricingAliasTarget, bool) {
	return PricingAliasTarget{}, false
}

func (r testPricingAliasResolver) PricingAliasRevision(sourceID SourceID) string {
	return r.revisions[sourceID]
}

func TestEffectivePricingSnapshotID(t *testing.T) {
	revision := strings.Repeat("a", 64)
	resolver := testPricingAliasResolver{revisions: map[SourceID]string{SourceCodex: revision}}

	if got := EffectivePricingSnapshotID("pricing-v1", nil, nil, SourceCodex); got != "pricing-v1" {
		t.Fatalf("nil resolver snapshot id = %q", got)
	}
	if got := EffectivePricingSnapshotID("pricing-v1", resolver, nil, SourceClaudeCode); got != "pricing-v1" {
		t.Fatalf("empty revision snapshot id = %q", got)
	}
	if got := EffectivePricingSnapshotID("pricing-v1", resolver, nil, SourceCodex); got != "pricing-v1+aliases-"+revision {
		t.Fatalf("effective snapshot id = %q", got)
	}
	if got := EffectivePricingSnapshotID("", resolver, nil, SourceCodex); got != "aliases-"+revision {
		t.Fatalf("alias-only snapshot id = %q", got)
	}
}

type stubCatalogSource struct {
	Source
	info     SourceInfo
	catalog  PricingCatalog
	baseHits int
}

func (s *stubCatalogSource) Info(context.Context) SourceInfo { return s.info }

func (s *stubCatalogSource) BasePricingCatalog(context.Context) PricingCatalog {
	s.baseHits++
	return s.catalog
}

func newStubCatalogSource(id SourceID, models ...PricingCatalogModel) *stubCatalogSource {
	return &stubCatalogSource{
		info: SourceInfo{ID: id, Label: string(id), Available: true},
		catalog: PricingCatalog{
			SourceID:   id,
			SnapshotID: string(id) + "-v1",
			Currency:   "USD",
			Models:     models,
		},
	}
}

func TestCatalogIndexLooksUpAnySourceExactly(t *testing.T) {
	codexSrc := newStubCatalogSource(SourceCodex, PricingCatalogModel{
		ModelID: "gpt-5.6-sol",
		Rate:    PricingRateSummary{InputPerMillion: 5, CacheWritePerMillion: 6.25, OutputPerMillion: 30},
	})
	claudeSrc := newStubCatalogSource(SourceClaudeCode, PricingCatalogModel{
		ModelID: "claude-opus-5",
		Rate:    PricingRateSummary{InputPerMillion: 15, OutputPerMillion: 75},
	})

	index := NewCatalogIndex()
	// Sources exist before the registry does, so an unbound index simply misses
	// rather than panicking.
	if _, _, ok := index.LookupPricingRate(SourceCodex, "gpt-5.6-sol"); ok {
		t.Fatal("unbound index resolved a rate")
	}

	registry := NewRegistry(SourceCodex)
	for _, src := range []*stubCatalogSource{codexSrc, claudeSrc} {
		if err := registry.Register(src); err != nil {
			t.Fatalf("register %s: %v", src.info.ID, err)
		}
	}
	index.Bind(registry)

	model, meta, ok := index.LookupPricingRate(SourceCodex, "gpt-5.6-sol")
	if !ok || model.Rate.CacheWritePerMillion != 6.25 || meta.Currency != "USD" || meta.SnapshotID != "codex-v1" {
		t.Fatalf("codex lookup = (%#v, %#v, %v)", model, meta, ok)
	}
	if _, _, ok := index.LookupPricingRate(SourceClaudeCode, "gpt-5.6-sol"); ok {
		t.Error("a model id resolved against the wrong source's catalog")
	}
	if _, _, ok := index.LookupPricingRate(SourceCodex, "gpt-5.6"); ok {
		t.Error("lookup is not exact: a prefix resolved")
	}

	// The merged table is cached, and Invalidate is what rebuilds it.
	hits := codexSrc.baseHits
	index.LookupPricingRate(SourceCodex, "gpt-5.6-sol")
	if codexSrc.baseHits != hits {
		t.Errorf("cached index re-read the base catalog")
	}
	index.Invalidate()
	index.LookupPricingRate(SourceCodex, "gpt-5.6-sol")
	if codexSrc.baseHits == hits {
		t.Error("Invalidate did not rebuild the index")
	}
}

func TestCatalogIndexRevisionTracksCatalogIdentity(t *testing.T) {
	src := newStubCatalogSource(SourceCodex, PricingCatalogModel{
		ModelID: "gpt-5.6", Rate: PricingRateSummary{InputPerMillion: 5, OutputPerMillion: 30},
	})
	registry := NewRegistry(SourceCodex)
	if err := registry.Register(src); err != nil {
		t.Fatal(err)
	}
	index := NewCatalogIndex()
	index.Bind(registry)

	first := index.Revision()
	if first == "" {
		t.Fatal("bound index has no revision")
	}
	src.catalog.SnapshotID = "codex-v2"
	index.Invalidate()
	if index.Revision() == first {
		t.Fatal("revision did not change when a bundled snapshot changed")
	}
}

type foreignAwareResolver struct {
	testPricingAliasResolver
	foreign bool
}

func (r foreignAwareResolver) CapturePricingAliases(sourceID SourceID) PricingAliasSnapshot {
	return foreignAwareTestSnapshot{revision: r.revisions[sourceID], foreign: r.foreign}
}

type foreignAwareTestSnapshot struct {
	revision string
	foreign  bool
}

func (s foreignAwareTestSnapshot) ResolvePricingAlias(string, string) (PricingAliasTarget, bool) {
	return PricingAliasTarget{}, false
}
func (s foreignAwareTestSnapshot) Revision() string        { return s.revision }
func (s foreignAwareTestSnapshot) HasForeignTargets() bool { return s.foreign }

// A cross-source alias depends on another catalog's rates, so upgrading the
// bundled catalogs has to change the aliasing source's cache identity too.
func TestCapturePricingAliasesFoldsCatalogRevisionForForeignTargets(t *testing.T) {
	src := newStubCatalogSource(SourceCodex, PricingCatalogModel{
		ModelID: "gpt-5.6", Rate: PricingRateSummary{InputPerMillion: 5, OutputPerMillion: 30},
	})
	registry := NewRegistry(SourceCodex)
	if err := registry.Register(src); err != nil {
		t.Fatal(err)
	}
	index := NewCatalogIndex()
	index.Bind(registry)

	revisions := map[SourceID]string{SourceClaudeCode: strings.Repeat("b", 64)}
	sameSource := foreignAwareResolver{testPricingAliasResolver{revisions: revisions}, false}
	crossSource := foreignAwareResolver{testPricingAliasResolver{revisions: revisions}, true}

	if got := CapturePricingAliases(sameSource, index, SourceClaudeCode).Revision(); got != revisions[SourceClaudeCode] {
		t.Fatalf("same-source revision = %q, want the store revision unchanged", got)
	}
	foreignRevision := CapturePricingAliases(crossSource, index, SourceClaudeCode).Revision()
	if foreignRevision == revisions[SourceClaudeCode] {
		t.Fatal("cross-source revision did not fold in the catalog revision")
	}

	src.catalog.SnapshotID = "codex-v2"
	index.Invalidate()
	if CapturePricingAliases(crossSource, index, SourceClaudeCode).Revision() == foreignRevision {
		t.Fatal("cross-source revision did not follow the catalog revision")
	}
	// Without an index there is nothing to fold, so the store revision stands.
	if got := CapturePricingAliases(crossSource, nil, SourceClaudeCode).Revision(); got != revisions[SourceClaudeCode] {
		t.Fatalf("revision without an index = %q, want the store revision", got)
	}
}

// reentrantCatalogSource mirrors a transcript adapter: its Info folds the alias
// revision into the reported snapshot id, and capturing that revision consults
// the catalog index. The index must therefore never reach Info.
type reentrantCatalogSource struct {
	*stubCatalogSource
	index    *CatalogIndex
	resolver PricingAliasResolver
	infoHits int
}

func (s *reentrantCatalogSource) Info(ctx context.Context) SourceInfo {
	s.infoHits++
	info := s.stubCatalogSource.Info(ctx)
	info.CostPolicy.PricingSnapshotID = EffectivePricingSnapshotID(s.catalog.SnapshotID, s.resolver, s.index, info.ID)
	return info
}

// Regression: the index used to enumerate sources through Registry.Available,
// which calls Info on each. For a source whose Info reaches this index that is
// unbounded recursion, and it wedges the whole registry because Available holds
// the read lock throughout.
func TestCatalogIndexDoesNotRecurseThroughSourceInfo(t *testing.T) {
	resolver := foreignAwareResolver{
		testPricingAliasResolver{revisions: map[SourceID]string{
			SourceClaudeCode: strings.Repeat("c", 64),
			SourceCodex:      strings.Repeat("d", 64),
		}},
		true,
	}
	index := NewCatalogIndex()
	registry := NewRegistry(SourceClaudeCode)
	for _, id := range []SourceID{SourceClaudeCode, SourceCodex} {
		src := &reentrantCatalogSource{
			stubCatalogSource: newStubCatalogSource(id, PricingCatalogModel{
				ModelID: string(id) + "-model",
				Rate:    PricingRateSummary{InputPerMillion: 1, OutputPerMillion: 2},
			}),
			index:    index,
			resolver: resolver,
		}
		if err := registry.Register(src); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	index.Bind(registry)
	// Register itself reads Info once per source; only calls beyond that would
	// come from the index.
	baseline := map[SourceID]int{}
	for _, candidate := range registry.registered() {
		src := candidate.(*reentrantCatalogSource)
		baseline[src.info.ID] = src.infoHits
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, _, ok := index.LookupPricingRate(SourceCodex, "codex-model"); !ok {
			t.Error("cross-source lookup missed")
		}
		// The invariant, checked directly rather than only by not hanging:
		// building the index must not have consulted any source's Info.
		for _, candidate := range registry.registered() {
			src := candidate.(*reentrantCatalogSource)
			if extra := src.infoHits - baseline[src.info.ID]; extra != 0 {
				t.Errorf("%s Info consulted %d extra times while building the index", src.info.ID, extra)
			}
		}
		// Available calls Info on every source, and Info consults the index.
		// This is the exact call order that used to deadlock.
		for _, src := range registry.Available(context.Background()) {
			_ = src.Info(context.Background())
		}
		// A registration racing an in-flight lookup must not wedge the registry.
		index.Invalidate()
		_ = index.Revision()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("catalog index recursed into source Info and deadlocked")
	}
}
