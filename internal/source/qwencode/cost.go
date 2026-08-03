package qwencode

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

//go:embed pricing_snapshot.json
var defaultPricingFS embed.FS

const apiEquivalentNote = "Estimated from Alibaba Cloud Model Studio list prices as an API-equivalent value. Qwen Code OAuth coding plans and Token Plan bundles are not billed per transcript token, so this is not actual subscription spend."

type pricingSnapshot struct {
	ID                   string                 `json:"id"`
	RetrievedAt          string                 `json:"retrieved_at"`
	Source               string                 `json:"source"`
	Currency             string                 `json:"currency"`
	Models               map[string]pricingRate `json:"models"`
	Aliases              map[string]string      `json:"aliases"`
	pricingAliases       source.PricingAliasSnapshot
	pricingAliasRevision string
	pricingRates         source.PricingRateIndex
}

type pricingRate struct {
	DisplayName        string  `json:"display_name"`
	ContextTokens      int64   `json:"context_tokens"`
	InputPerMillion    float64 `json:"input_per_million"`
	CacheHitPerMillion float64 `json:"cache_hit_input_per_million"`
	// CacheWritePerMillion is zero for the models whose Model Studio listing
	// publishes no separate cache-write price; those bill writes at the input
	// rate. See cacheWritePerMillion.
	CacheWritePerMillion float64 `json:"cache_write_per_million,omitempty"`
	OutputPerMillion     float64 `json:"output_per_million"`
	AvailabilityNote     string  `json:"availability_note,omitempty"`
}

// cacheWritePerMillion reports the rate cache-write tokens bill at, falling back
// to the plain input rate for models with no published cache-write price.
func (r pricingRate) cacheWritePerMillion() float64 {
	if r.CacheWritePerMillion > 0 {
		return r.CacheWritePerMillion
	}
	return r.InputPerMillion
}

type pricingMatch struct {
	Rate             pricingRate
	CanonicalModelID string
	Kind             source.PricingResolutionKind
	// TargetSourceID is set only when a user alias borrowed another source's
	// catalog, in which case Rate was adapted from the shared rate summary.
	TargetSourceID source.SourceID
	// OverridesNative reports that the user alias displaced an otherwise usable
	// native catalog match.
	OverridesNative bool
}

func (m pricingMatch) foreign() bool {
	return m.TargetSourceID != "" && m.TargetSourceID != source.SourceQwenCode
}

type costResult struct {
	Cost       float64
	Status     stats.CostStatus
	Provenance *stats.CostProvenance
}

func (s *Source) loadPricing(ctx context.Context) pricingSnapshot {
	return s.bindPricingAliases(s.loadBasePricing(ctx))
}

// loadBasePricing returns the bundled snapshot with no alias state attached. It
// is the leaf that BasePricingCatalog and cross-source lookups read, so it must
// never touch the alias store or the catalog index.
func (s *Source) loadBasePricing(ctx context.Context) pricingSnapshot {
	s.mu.Lock()
	if s.pricing.ID != "" || s.pricingErr != nil {
		pricing := s.pricing
		s.mu.Unlock()
		return pricing
	}
	s.mu.Unlock()

	var content []byte
	var err error
	if s.opts.PricingSnapshotPath != "" {
		content, err = os.ReadFile(s.opts.PricingSnapshotPath)
	} else {
		content, err = defaultPricingFS.ReadFile("pricing_snapshot.json")
	}
	if err == nil && ctx != nil {
		err = ctx.Err()
	}
	var pricing pricingSnapshot
	if err == nil {
		err = json.Unmarshal(content, &pricing)
	}
	if pricing.Currency == "" {
		pricing.Currency = "USD"
	}
	if err == nil && pricing.isStale(time.Now().UTC()) {
		err = os.ErrInvalid
		pricing = pricingSnapshot{Currency: "USD"}
	}

	s.mu.Lock()
	if err != nil {
		s.pricingErr = err
	} else {
		s.pricing = pricing
	}
	result := s.pricing
	s.mu.Unlock()
	return result
}

// bindPricingAliases attaches alias state outside s.mu. Capture may read other
// sources' base catalogs, so holding this source's lock here would make the
// lock order depend on which source aliases which.
func (s *Source) bindPricingAliases(pricing pricingSnapshot) pricingSnapshot {
	aliases := source.CapturePricingAliases(s.pricingAliases, s.pricingRates, source.SourceQwenCode)
	revision := ""
	if aliases != nil {
		revision = aliases.Revision()
	}
	pricing.pricingAliases = aliases
	pricing.pricingAliasRevision = revision
	pricing.pricingRates = s.pricingRates
	pricing.ID = source.EffectivePricingSnapshotIDForAliases(pricing.ID, aliases)
	return pricing
}

func (p pricingSnapshot) isStale(now time.Time) bool {
	if p.RetrievedAt == "" {
		return true
	}
	retrieved, err := time.Parse(time.RFC3339Nano, p.RetrievedAt)
	if err != nil {
		return true
	}
	return now.Sub(retrieved.UTC()) > 365*24*time.Hour
}

func (p pricingSnapshot) nativePricing(model string) (pricingMatch, bool) {
	normalized := strings.TrimSpace(model)
	if rate, ok := p.Models[normalized]; ok {
		return pricingMatch{Rate: rate, CanonicalModelID: normalized, Kind: source.PricingResolutionExact}, true
	}
	canonical, ok := p.Aliases[normalized]
	if !ok {
		return pricingMatch{}, false
	}
	rate, ok := p.Models[canonical]
	if !ok {
		return pricingMatch{}, false
	}
	return pricingMatch{Rate: rate, CanonicalModelID: canonical, Kind: source.PricingResolutionNativeAlias}, true
}

func (p pricingSnapshot) resolve(providerID, model string) pricingMatch {
	native, nativeFound := p.nativePricing(model)
	nativeUsable := nativeFound && usablePricingRate(native.Rate)

	// The user alias is tried first: it is an explicit statement about what the
	// model really is, so it outranks even an exact catalog row.
	if alias, ok := p.aliasPricing(providerID, model); ok {
		alias.OverridesNative = nativeUsable
		return alias
	}
	if nativeUsable {
		return native
	}
	if len(p.Models) == 0 {
		return pricingMatch{Kind: source.PricingResolutionUnavailable}
	}
	if nativeFound {
		native.Kind = source.PricingResolutionUnpriced
		return native
	}
	return pricingMatch{Kind: source.PricingResolutionUnknown}
}

func (p pricingSnapshot) aliasPricing(providerID, model string) (pricingMatch, bool) {
	if p.pricingAliases == nil {
		return pricingMatch{}, false
	}
	target, ok := p.pricingAliases.ResolvePricingAlias(providerID, model)
	if !ok || target.ModelID == "" {
		return pricingMatch{}, false
	}
	if target.SourceID == "" || target.SourceID == source.SourceQwenCode {
		rate, exact := p.Models[target.ModelID]
		if !exact || !usablePricingRate(rate) {
			return pricingMatch{}, false
		}
		return pricingMatch{
			Rate:             rate,
			CanonicalModelID: target.ModelID,
			Kind:             source.PricingResolutionUserAlias,
			TargetSourceID:   source.SourceQwenCode,
		}, true
	}
	if p.pricingRates == nil {
		return pricingMatch{}, false
	}
	foreign, meta, found := p.pricingRates.LookupPricingRate(target.SourceID, target.ModelID)
	if !found || !source.UsablePricingRate(foreign.Rate) {
		return pricingMatch{}, false
	}
	// Rates are per-million values in the catalog's own currency; borrowing one
	// denominated differently would silently mix units into a single total.
	if meta.Currency != defaultCurrency(p) {
		return pricingMatch{}, false
	}
	return pricingMatch{
		Rate:             foreignRate(foreign.Rate),
		CanonicalModelID: target.ModelID,
		Kind:             source.PricingResolutionUserAlias,
		TargetSourceID:   target.SourceID,
	}, true
}

// foreignRate adapts another catalog's shared rate summary to this source's
// rate shape. Only the four shared dimensions carry over; a borrowed catalog's
// tier- or duration-specific rates have no equivalent here. A zero cache-write
// price stays zero so cacheWritePerMillion falls back to the input rate, which
// is what the source catalog meant by omitting it.
func foreignRate(summary source.PricingRateSummary) pricingRate {
	return pricingRate{
		InputPerMillion:      summary.InputPerMillion,
		CacheHitPerMillion:   summary.CachedInputPerMillion,
		CacheWritePerMillion: summary.CacheWritePerMillion,
		OutputPerMillion:     summary.OutputPerMillion,
	}
}

func crossSourceNote(match pricingMatch) string {
	return "priced from the " + string(match.TargetSourceID) + " catalog model " + match.CanonicalModelID +
		" by a user pricing alias; only input, cached input, cache write and output rates carry across catalogs"
}

func usablePricingRate(rate pricingRate) bool {
	return rate.InputPerMillion > 0 && rate.OutputPerMillion > 0
}

func computeCost(model, providerID string, tokens stats.TokenStats, pricing pricingSnapshot) costResult {
	currency := defaultCurrency(pricing)
	match := pricing.resolve(providerID, model)
	if !usablePricingRate(match.Rate) {
		return missingCost(currency)
	}
	// Cache hits bill at the discounted cached-input rate and reasoning at the
	// output rate. Cache writes are always zero in recorded Qwen data — the
	// usage log reports no cache-write counter — so their rate only matters for
	// a proxied model aliased into this catalog.
	cost := (float64(tokens.Input)*match.Rate.InputPerMillion +
		float64(tokens.Cache.Write)*match.Rate.cacheWritePerMillion() +
		float64(tokens.Cache.Read)*match.Rate.CacheHitPerMillion +
		float64(tokens.Output+tokens.Reasoning)*match.Rate.OutputPerMillion) / 1_000_000
	note := apiEquivalentNote
	if match.foreign() {
		note = apiEquivalentNote + " " + crossSourceNote(match)
	}
	return costResult{
		Cost:   cost,
		Status: stats.CostEstimatedAPIEquivalent,
		Provenance: &stats.CostProvenance{
			Status:            stats.CostEstimatedAPIEquivalent,
			Currency:          currency,
			PricingSnapshotID: pricing.ID,
			PricingSource:     pricing.Source,
			ComputedCount:     1,
			Note:              note,
		},
	}
}

func rateSummary(rate pricingRate) source.PricingRateSummary {
	return source.PricingRateSummary{
		InputPerMillion:       rate.InputPerMillion,
		CachedInputPerMillion: rate.CacheHitPerMillion,
		// Left zero when unpublished, which the shared type documents as
		// "bills at the input rate" rather than "free".
		CacheWritePerMillion: rate.CacheWritePerMillion,
		OutputPerMillion:     rate.OutputPerMillion,
		Note:                 rate.AvailabilityNote,
	}
}

func (s *Source) PricingCatalog(ctx context.Context) source.PricingCatalog {
	return catalogFrom(s.loadPricing(ctx))
}

// BasePricingCatalog reports the bundled catalog without alias state, which is
// what other sources borrow rates from. It reads loadBasePricing directly so it
// stays a leaf call in cross-source resolution.
func (s *Source) BasePricingCatalog(ctx context.Context) source.PricingCatalog {
	return catalogFrom(s.loadBasePricing(ctx))
}

func catalogFrom(pricing pricingSnapshot) source.PricingCatalog {
	catalog := source.PricingCatalog{
		SourceID:   source.SourceQwenCode,
		SnapshotID: pricing.ID,
		Currency:   defaultCurrency(pricing),
		Models:     []source.PricingCatalogModel{},
		Note:       apiEquivalentNote,
	}
	keys := make([]string, 0, len(pricing.Models))
	for modelID := range pricing.Models {
		keys = append(keys, modelID)
	}
	sort.Strings(keys)
	for _, modelID := range keys {
		rate := pricing.Models[modelID]
		catalog.Models = append(catalog.Models, source.PricingCatalogModel{
			ModelID:     modelID,
			DisplayName: rate.DisplayName,
			Rate:        rateSummary(rate),
		})
	}
	return catalog
}

func (s *Source) ResolvePricing(ctx context.Context, providerID, modelID string) source.PricingResolution {
	pricing := s.loadPricing(ctx)
	match := pricing.resolve(providerID, modelID)
	resolution := source.PricingResolution{
		SourceID:        source.SourceQwenCode,
		TargetSourceID:  match.TargetSourceID,
		ProviderID:      providerID,
		ModelID:         modelID,
		TargetModelID:   match.CanonicalModelID,
		Kind:            match.Kind,
		OverridesNative: match.OverridesNative,
	}
	if usablePricingRate(match.Rate) {
		rate := rateSummary(match.Rate)
		resolution.Rate = &rate
	}
	if match.foreign() {
		resolution.Note = crossSourceNote(match)
	}
	return resolution
}

func missingCost(currency string) costResult {
	return costResult{
		Status: stats.CostMissing,
		Provenance: &stats.CostProvenance{
			Status:       stats.CostMissing,
			Currency:     currency,
			MissingCount: 1,
			Note:         "Qwen Code cost is unknown because supported model pricing or request usage is unavailable",
		},
	}
}

func aggregateCostProvenance(messages []*messageRecord) (float64, stats.TokenStats, stats.CostStatus, *stats.CostProvenance) {
	var totalCost float64
	var tokens stats.TokenStats
	prov := &stats.CostProvenance{Currency: "USD"}
	statuses := make(map[stats.CostStatus]bool)
	for _, msg := range messages {
		if msg.Entry.Role != "assistant" {
			continue
		}
		totalCost += msg.Entry.Cost
		if msg.Entry.Tokens != nil {
			tokens.Input += msg.Entry.Tokens.Input
			tokens.Output += msg.Entry.Tokens.Output
			tokens.Reasoning += msg.Entry.Tokens.Reasoning
			tokens.Cache.Read += msg.Entry.Tokens.Cache.Read
			tokens.Cache.Write += msg.Entry.Tokens.Cache.Write
		}
		status := msg.Entry.CostStatus
		if status == "" {
			status = stats.CostMissing
		}
		statuses[status] = true
		if cp := msg.Entry.CostProvenance; cp != nil {
			prov.MissingCount += cp.MissingCount
			prov.ComputedCount += cp.ComputedCount
			prov.ReportedCount += cp.ReportedCount
			if prov.PricingSnapshotID == "" {
				prov.PricingSnapshotID = cp.PricingSnapshotID
			}
			if prov.PricingSource == "" {
				prov.PricingSource = cp.PricingSource
			}
			if cp.Currency != "" {
				prov.Currency = cp.Currency
			}
		}
	}
	status := combineStatuses(statuses)
	prov.Status = status
	switch status {
	case stats.CostEstimatedAPIEquivalent:
		prov.Note = apiEquivalentNote
	case stats.CostMixed:
		prov.Note = "aggregate mixes estimated API-equivalent and missing Qwen Code costs"
	case stats.CostMissing:
		prov.Note = "aggregate Qwen Code cost is unknown because supported model pricing or request usage is unavailable"
	}
	return totalCost, tokens, status, prov
}

func combineStatuses(statuses map[stats.CostStatus]bool) stats.CostStatus {
	if len(statuses) == 0 {
		return stats.CostMissing
	}
	if len(statuses) > 1 {
		return stats.CostMixed
	}
	for status := range statuses {
		return status
	}
	return stats.CostMissing
}

func cloneProvenance(in *stats.CostProvenance) *stats.CostProvenance {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneTokens(in *stats.TokenStats) *stats.TokenStats {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func defaultCurrency(pricing pricingSnapshot) string {
	if pricing.Currency != "" {
		return pricing.Currency
	}
	return "USD"
}
