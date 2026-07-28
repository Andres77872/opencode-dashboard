package source

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sort"
	"sync"
)

// PricingAliasTarget is the catalog entry an alias points at. SourceID may name
// a different source than the aliasing one: a proxy that serves another
// vendor's models under one CLI is priced by borrowing that vendor's catalog.
type PricingAliasTarget struct {
	SourceID SourceID
	ModelID  string
}

// PricingAliasResolver resolves user-managed model aliases without coupling a
// source adapter to the settings store that owns them.
type PricingAliasResolver interface {
	ResolvePricingAlias(sourceID SourceID, providerID, modelID string) (PricingAliasTarget, bool)
	PricingAliasRevision(sourceID SourceID) string
}

// PricingAliasSnapshot binds one source's immutable mappings and revision. A
// normalization pass captures this once so every message and its provenance use
// the same alias state even while the settings store is updated concurrently.
type PricingAliasSnapshot interface {
	ResolvePricingAlias(providerID, modelID string) (PricingAliasTarget, bool)
	Revision() string
}

// PricingAliasSnapshotProvider is implemented by resolvers that can atomically
// capture mappings and revision from one immutable store snapshot.
type PricingAliasSnapshotProvider interface {
	CapturePricingAliases(sourceID SourceID) PricingAliasSnapshot
}

// PricingAliasForeignTargets is an optional capability of a captured snapshot
// that reports whether any mapping leaves the source. Cross-source pricing
// depends on another catalog's rates, so such a snapshot must fold the catalog
// index revision into its own.
type PricingAliasForeignTargets interface {
	HasForeignTargets() bool
}

type livePricingAliasSnapshot struct {
	resolver PricingAliasResolver
	sourceID SourceID
	revision string
}

func (s livePricingAliasSnapshot) ResolvePricingAlias(providerID, modelID string) (PricingAliasTarget, bool) {
	return s.resolver.ResolvePricingAlias(s.sourceID, providerID, modelID)
}

func (s livePricingAliasSnapshot) Revision() string { return s.revision }

// foreignAwareSnapshot folds the bundled-catalog revision into an alias
// revision so a cross-source target's rates participate in the cache identity.
// Without it, upgrading the binary would change a foreign target's rates while
// the aliasing source kept serving costs cached under the old identity.
type foreignAwareSnapshot struct {
	inner    PricingAliasSnapshot
	revision string
}

func (s foreignAwareSnapshot) ResolvePricingAlias(providerID, modelID string) (PricingAliasTarget, bool) {
	return s.inner.ResolvePricingAlias(providerID, modelID)
}

func (s foreignAwareSnapshot) Revision() string { return s.revision }

// CapturePricingAliases returns one source-scoped alias view. Production stores
// implement PricingAliasSnapshotProvider; the fallback preserves compatibility
// for lightweight resolvers while still freezing the advertised revision.
//
// Callers must not hold their own pricing mutex while calling this. Capture may
// consult the catalog index, which reads other sources' base catalogs; taking
// two source locks in an order that depends on who aliases whom would deadlock.
func CapturePricingAliases(resolver PricingAliasResolver, rates PricingRateIndex, sourceID SourceID) PricingAliasSnapshot {
	if resolver == nil {
		return nil
	}
	var snapshot PricingAliasSnapshot
	if provider, ok := resolver.(PricingAliasSnapshotProvider); ok {
		snapshot = provider.CapturePricingAliases(sourceID)
	} else {
		snapshot = livePricingAliasSnapshot{
			resolver: resolver,
			sourceID: sourceID,
			revision: resolver.PricingAliasRevision(sourceID),
		}
	}
	if snapshot == nil || rates == nil || snapshot.Revision() == "" {
		return snapshot
	}
	foreign, ok := snapshot.(PricingAliasForeignTargets)
	if !ok || !foreign.HasForeignTargets() {
		return snapshot
	}
	return foreignAwareSnapshot{
		inner:    snapshot,
		revision: combineRevisions(snapshot.Revision(), rates.Revision()),
	}
}

// PricingResolutionKind describes how a detected model was matched to a
// pricing catalog entry.
type PricingResolutionKind string

const (
	PricingResolutionExact       PricingResolutionKind = "exact"
	PricingResolutionNativeAlias PricingResolutionKind = "native_alias"
	PricingResolutionFallback    PricingResolutionKind = "fallback"
	PricingResolutionUserAlias   PricingResolutionKind = "user_alias"
	PricingResolutionUnpriced    PricingResolutionKind = "unpriced"
	PricingResolutionUnknown     PricingResolutionKind = "unknown"
	PricingResolutionUnavailable PricingResolutionKind = "unavailable"
)

// PricingRateSummary is the common subset of per-token pricing exposed by all
// source catalogs. Values are denominated in the catalog currency per million
// tokens. This subset is also the transfer format for cross-source aliases:
// catalog-specific extras (processing tiers, long-context multipliers, split
// cache-creation durations) have no shared meaning and are not carried.
type PricingRateSummary struct {
	InputPerMillion       float64 `json:"input_per_million"`
	CachedInputPerMillion float64 `json:"cached_input_per_million"`
	// CacheWritePerMillion is zero for catalogs that bill cache writes at the
	// normal input rate rather than publishing a separate price.
	CacheWritePerMillion float64 `json:"cache_write_per_million,omitempty"`
	OutputPerMillion     float64 `json:"output_per_million"`
	Note                 string  `json:"note,omitempty"`
}

// UsablePricingRate reports whether a summary can price a request at all.
func UsablePricingRate(rate PricingRateSummary) bool {
	return rate.InputPerMillion > 0 && rate.OutputPerMillion > 0
}

// PricingCatalogModel is one target a user-managed alias may select.
type PricingCatalogModel struct {
	ModelID     string             `json:"model_id"`
	DisplayName string             `json:"display_name,omitempty"`
	Rate        PricingRateSummary `json:"rate"`
}

// PricingCatalog is a source adapter's user-visible bundled pricing catalog.
type PricingCatalog struct {
	SourceID   SourceID              `json:"source_id"`
	SnapshotID string                `json:"snapshot_id,omitempty"`
	Currency   string                `json:"currency,omitempty"`
	Models     []PricingCatalogModel `json:"models"`
	Note       string                `json:"note,omitempty"`
}

// PricingResolution reports how one detected provider/model pair maps to the
// source's pricing catalog. ProviderID is the provider identity the source
// actually resolved with, which is not always the caller's argument: a source
// with a fixed provider identity reports that identity instead, so callers can
// detect alias keys that could never match.
type PricingResolution struct {
	SourceID SourceID `json:"source_id"`
	// TargetSourceID names the catalog the rate came from. It differs from
	// SourceID only for a cross-source user alias.
	TargetSourceID SourceID              `json:"target_source_id,omitempty"`
	ProviderID     string                `json:"provider_id,omitempty"`
	ModelID        string                `json:"model_id"`
	TargetModelID  string                `json:"target_model_id,omitempty"`
	Kind           PricingResolutionKind `json:"kind"`
	// OverridesNative reports that a user alias won over an otherwise usable
	// native catalog match, so the displayed price is a deliberate override.
	OverridesNative bool                `json:"overrides_native,omitempty"`
	Rate            *PricingRateSummary `json:"rate,omitempty"`
	Note            string              `json:"note,omitempty"`
}

// PricingCatalogSource is implemented by source adapters that can expose their
// bundled catalog and explain model resolution decisions.
type PricingCatalogSource interface {
	PricingCatalog(context.Context) PricingCatalog
	ResolvePricing(context.Context, string, string) PricingResolution
}

// BasePricingCatalogSource exposes the bundled catalog with no user aliases
// folded in.
//
// Implementations must never consult the alias store or the catalog index —
// not even indirectly through their own Info, which folds the alias revision
// into the reported snapshot id. That is what keeps cross-source alias
// resolution a leaf call: source A can borrow source B's rates without B's own
// alias state being resolved in turn.
type BasePricingCatalogSource interface {
	BasePricingCatalog(context.Context) PricingCatalog
}

// PricingInvalidator is implemented by sources whose process-local snapshot
// can be discarded after a pricing alias changes.
type PricingInvalidator interface {
	Invalidate()
}

// PricingCatalogMeta identifies the catalog a borrowed rate came from.
type PricingCatalogMeta struct {
	SourceID   SourceID
	SnapshotID string
	Currency   string
}

// PricingRateIndex resolves one bundled catalog rate for any source. Source
// adapters use it only for alias targets that point outside themselves.
type PricingRateIndex interface {
	LookupPricingRate(sourceID SourceID, modelID string) (PricingCatalogModel, PricingCatalogMeta, bool)
	Revision() string
}

// CatalogIndex is the registry-backed PricingRateIndex.
//
// It is created before the registry exists — source adapters are constructed
// first and need the index in their options — so the registry is bound
// afterwards with Bind. Until then every lookup simply misses.
type CatalogIndex struct {
	// build serializes rebuilds so concurrent misses do one pass, and is never
	// held by a reader.
	build sync.Mutex
	// mu guards the fields below and is only ever held for map reads and the
	// final swap, never across a call into a source.
	mu sync.Mutex
	// sources is captured by Bind and Invalidate rather than read on demand.
	// Registry.Available holds the registry read lock while calling Info, and a
	// transcript source's Info reaches this index; re-entering the registry
	// lock from here would deadlock the moment a Register is waiting.
	sources  []Source
	loaded   bool
	models   map[SourceID]map[string]PricingCatalogModel
	meta     map[SourceID]PricingCatalogMeta
	revision string
}

var _ PricingRateIndex = (*CatalogIndex)(nil)

// NewCatalogIndex returns an unbound index. Lookups miss until Bind is called.
func NewCatalogIndex() *CatalogIndex { return &CatalogIndex{} }

// Bind captures the registry's sources, which supply the bundled catalogs. It
// must be called once every source is registered, and never while the caller
// holds the registry read lock.
func (i *CatalogIndex) Bind(registry *Registry) {
	if i == nil {
		return
	}
	sources := registry.registered()
	i.mu.Lock()
	defer i.mu.Unlock()
	i.sources = sources
	i.reset()
}

// Invalidate drops the cached merged catalog so the next lookup rebuilds it.
func (i *CatalogIndex) Invalidate() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.reset()
}

func (i *CatalogIndex) reset() {
	i.loaded = false
	i.models = nil
	i.meta = nil
	i.revision = ""
}

// LookupPricingRate returns one bundled catalog model from any registered
// source. The lookup is exact: cross-source aliases never inherit a source's
// family-prefix or date-suffix fallbacks, whose semantics are catalog-specific.
func (i *CatalogIndex) LookupPricingRate(sourceID SourceID, modelID string) (PricingCatalogModel, PricingCatalogMeta, bool) {
	if i == nil || sourceID == "" || modelID == "" {
		return PricingCatalogModel{}, PricingCatalogMeta{}, false
	}
	i.load()
	i.mu.Lock()
	defer i.mu.Unlock()
	models, ok := i.models[sourceID]
	if !ok {
		return PricingCatalogModel{}, PricingCatalogMeta{}, false
	}
	model, ok := models[modelID]
	if !ok {
		return PricingCatalogModel{}, PricingCatalogMeta{}, false
	}
	return model, i.meta[sourceID], true
}

// Revision identifies the set of bundled catalogs currently indexed. It changes
// when a catalog's snapshot id or currency changes, which is what lets an alias
// revision detect that a borrowed rate may have moved.
func (i *CatalogIndex) Revision() string {
	if i == nil {
		return ""
	}
	i.load()
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.revision
}

// load rebuilds the merged catalog if needed.
//
// Every call into a source happens with no index lock held: those calls take
// the source's own mutex, and holding i.mu across them would make the lock
// order depend on which source aliases which.
func (i *CatalogIndex) load() {
	i.mu.Lock()
	loaded, sources := i.loaded, i.sources
	i.mu.Unlock()
	if loaded {
		return
	}

	i.build.Lock()
	defer i.build.Unlock()
	i.mu.Lock()
	loaded, sources = i.loaded, i.sources
	i.mu.Unlock()
	if loaded {
		return
	}

	models := map[SourceID]map[string]PricingCatalogModel{}
	meta := map[SourceID]PricingCatalogMeta{}
	ctx := context.Background()
	// Only BasePricingCatalog is called here. It is the one accessor that does
	// not fold alias state in, so it cannot lead back into this index.
	for _, candidate := range sources {
		base, ok := candidate.(BasePricingCatalogSource)
		if !ok {
			continue
		}
		catalog := base.BasePricingCatalog(ctx)
		if catalog.SourceID == "" || len(catalog.Models) == 0 {
			continue
		}
		catalogModels := make(map[string]PricingCatalogModel, len(catalog.Models))
		for _, model := range catalog.Models {
			if model.ModelID == "" {
				continue
			}
			catalogModels[model.ModelID] = model
		}
		if len(catalogModels) == 0 {
			continue
		}
		models[catalog.SourceID] = catalogModels
		currency := catalog.Currency
		if currency == "" {
			currency = "USD"
		}
		meta[catalog.SourceID] = PricingCatalogMeta{
			SourceID:   catalog.SourceID,
			SnapshotID: catalog.SnapshotID,
			Currency:   currency,
		}
	}

	i.mu.Lock()
	// A concurrent Bind may have replaced the source set this pass was built
	// from; discarding the result keeps the index consistent with what is set.
	if sameSources(i.sources, sources) {
		i.models, i.meta, i.revision, i.loaded = models, meta, catalogRevision(meta), true
	}
	i.mu.Unlock()
}

func sameSources(left, right []Source) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func catalogRevision(meta map[SourceID]PricingCatalogMeta) string {
	if len(meta) == 0 {
		return ""
	}
	ids := make([]string, 0, len(meta))
	for sourceID := range meta {
		ids = append(ids, string(sourceID))
	}
	sort.Strings(ids)
	digest := sha256.New()
	for _, id := range ids {
		entry := meta[SourceID(id)]
		writeHashField(digest, id)
		writeHashField(digest, entry.SnapshotID)
		writeHashField(digest, entry.Currency)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func combineRevisions(values ...string) string {
	digest := sha256.New()
	for _, value := range values {
		writeHashField(digest, value)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeHashField(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}

// EffectivePricingSnapshotID folds the current full per-source alias revision
// into a bundled pricing snapshot identifier. With no aliases, callers retain
// the original identifier so existing cache identities remain stable.
func EffectivePricingSnapshotID(baseID string, resolver PricingAliasResolver, rates PricingRateIndex, sourceID SourceID) string {
	return EffectivePricingSnapshotIDForAliases(baseID, CapturePricingAliases(resolver, rates, sourceID))
}

func EffectivePricingSnapshotIDForAliases(baseID string, aliases PricingAliasSnapshot) string {
	if aliases == nil || aliases.Revision() == "" {
		return baseID
	}
	if baseID == "" {
		return "aliases-" + aliases.Revision()
	}
	return baseID + "+aliases-" + aliases.Revision()
}
