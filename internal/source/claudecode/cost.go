package claudecode

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

//go:embed pricing_snapshot.json
var defaultPricingFS embed.FS

type pricingSnapshot struct {
	ID          string                 `json:"id"`
	RetrievedAt string                 `json:"retrieved_at"`
	Source      string                 `json:"source"`
	Currency    string                 `json:"currency"`
	Models      map[string]pricingRate `json:"models"`
	aliases     source.PricingAliasSnapshot
	rates       source.PricingRateIndex
}

type pricingRate struct {
	InputPerMillion         float64 `json:"input_per_million"`
	OutputPerMillion        float64 `json:"output_per_million"`
	CacheReadPerMillion     float64 `json:"cache_read_input_per_million"`
	CacheCreatePerMillion   float64 `json:"cache_creation_input_per_million"`
	CacheCreate1hPerMillion float64 `json:"cache_creation_input_1h_per_million"`
	Approximate             bool    `json:"approximate"`
	Note                    string  `json:"note"`
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
	return m.TargetSourceID != "" && m.TargetSourceID != source.SourceClaudeCode
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
	pricing.aliases = source.CapturePricingAliases(s.pricingAliases, s.pricingRates, source.SourceClaudeCode)
	pricing.rates = s.pricingRates
	pricing.ID = source.EffectivePricingSnapshotIDForAliases(pricing.ID, pricing.aliases)
	return pricing
}

func computeCost(model, providerID string, usage tokenUsage, hasUsage bool, reported *float64, pricing pricingSnapshot) costResult {
	currency := pricing.Currency
	if currency == "" {
		currency = "USD"
	}
	if reported != nil {
		return costResult{
			Cost:   *reported,
			Status: stats.CostReported,
			Provenance: &stats.CostProvenance{
				Status:        stats.CostReported,
				Currency:      currency,
				ReportedCount: 1,
				Note:          "cost reported by Claude Code transcript data",
			},
		}
	}
	if !hasUsage || model == "" {
		return missingCost(currency)
	}
	match := pricing.resolve(providerID, model)
	if !usablePricingRate(match.Rate) {
		return missingCost(currency)
	}
	cacheCreate5m, cacheCreate1h := usage.cacheCreateForPricing()
	cost := (float64(usage.Input)*match.Rate.InputPerMillion +
		float64(usage.Output)*match.Rate.OutputPerMillion +
		float64(usage.CacheRead)*match.Rate.CacheReadPerMillion +
		float64(cacheCreate5m)*match.Rate.CacheCreatePerMillion +
		float64(cacheCreate1h)*match.Rate.cacheCreate1hPerMillion()) / 1_000_000
	status := stats.CostComputed
	note := "cost computed from transcript token usage and bundled pricing snapshot"
	if match.Kind == source.PricingResolutionFallback || match.Rate.Approximate {
		status = stats.CostApproximate
		note = "cost approximated from transcript token usage and family-level bundled pricing"
		if match.Rate.Note != "" {
			note = match.Rate.Note
		}
	}
	if match.foreign() {
		// A borrowed catalog carries no cache-creation duration split and no
		// vendor-specific adjustments, so the result is an approximation even
		// though every rate it does use is exact.
		status = stats.CostApproximate
		note = "cost approximated from transcript token usage; " + crossSourceNote(match)
	}
	return costResult{
		Cost:   cost,
		Status: status,
		Provenance: &stats.CostProvenance{
			Status:            status,
			Currency:          currency,
			PricingSnapshotID: pricing.ID,
			PricingSource:     pricing.Source,
			ComputedCount:     1,
			Note:              note,
		},
	}
}

func (u tokenUsage) cacheCreateForPricing() (int64, int64) {
	if u.CacheCreate5m == 0 && u.CacheCreate1h == 0 {
		return u.CacheCreate, 0
	}
	return u.CacheCreate5m, u.CacheCreate1h
}

func (r pricingRate) cacheCreate1hPerMillion() float64 {
	if r.CacheCreate1hPerMillion != 0 {
		return r.CacheCreate1hPerMillion
	}
	if r.InputPerMillion != 0 {
		return r.InputPerMillion * 2
	}
	return r.CacheCreatePerMillion
}

func missingCost(currency string) costResult {
	return costResult{
		Status: stats.CostMissing,
		Provenance: &stats.CostProvenance{
			Status:       stats.CostMissing,
			Currency:     currency,
			MissingCount: 1,
			Note:         "cost is missing from Claude Code transcript data and cannot be computed from available usage/model fields",
		},
	}
}

func (p pricingSnapshot) nativePricing(model string) (pricingMatch, bool) {
	if rate, ok := p.Models[model]; ok {
		return pricingMatch{Rate: rate, CanonicalModelID: model, Kind: source.PricingResolutionExact}, true
	}
	keys := make([]string, 0, len(p.Models))
	for key := range p.Models {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		if modelPricingKeyMatches(model, key) {
			rate := p.Models[key]
			rate.Approximate = true
			return pricingMatch{Rate: rate, CanonicalModelID: key, Kind: source.PricingResolutionFallback}, true
		}
	}
	return pricingMatch{}, false
}

func (p pricingSnapshot) resolve(providerID, model string) pricingMatch {
	native, nativeFound := p.nativePricing(model)
	nativeUsable := nativeFound && usablePricingRate(native.Rate)

	// The user alias is tried first: it is an explicit statement about what the
	// model really is, so it outranks even an exact catalog row. That is what
	// lets a proxy model whose name merely prefix-matches a Claude family be
	// corrected instead of silently priced by that family.
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
	if p.aliases == nil {
		return pricingMatch{}, false
	}
	target, ok := p.aliases.ResolvePricingAlias(providerID, model)
	if !ok || target.ModelID == "" {
		return pricingMatch{}, false
	}
	if target.SourceID == "" || target.SourceID == source.SourceClaudeCode {
		rate, exact := p.Models[target.ModelID]
		if !exact || !usablePricingRate(rate) || !p.isExactCatalogTarget(target.ModelID) {
			return pricingMatch{}, false
		}
		return pricingMatch{
			Rate:             rate,
			CanonicalModelID: target.ModelID,
			Kind:             source.PricingResolutionUserAlias,
			TargetSourceID:   source.SourceClaudeCode,
		}, true
	}
	if p.rates == nil {
		return pricingMatch{}, false
	}
	foreign, meta, found := p.rates.LookupPricingRate(target.SourceID, target.ModelID)
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

// foreignRate adapts another catalog's shared rate summary to Claude's rate
// shape. Only input, cached input, cache write and output carry across; the
// 1h cache-creation tier has no cross-vendor equivalent and falls back to the
// existing input-derived estimate. Catalogs that bill cache writes at the
// input rate publish no separate price, so input stands in.
func foreignRate(summary source.PricingRateSummary) pricingRate {
	cacheCreate := summary.CacheWritePerMillion
	if cacheCreate <= 0 {
		cacheCreate = summary.InputPerMillion
	}
	return pricingRate{
		InputPerMillion:       summary.InputPerMillion,
		OutputPerMillion:      summary.OutputPerMillion,
		CacheReadPerMillion:   summary.CachedInputPerMillion,
		CacheCreatePerMillion: cacheCreate,
		Approximate:           true,
	}
}

func (p pricingSnapshot) isFamilyFallbackKey(target string) bool {
	rate, ok := p.Models[target]
	if !ok || !rate.Approximate {
		return false
	}
	for modelID := range p.Models {
		if modelID != target && modelPricingKeyMatches(modelID, target) {
			return true
		}
	}
	return false
}

// isExactCatalogTarget identifies rows exposed as user-selectable alias
// targets. Family-only fallback keys stay internal to native resolution.
func (p pricingSnapshot) isExactCatalogTarget(modelID string) bool {
	_, ok := p.Models[modelID]
	return ok && !p.isFamilyFallbackKey(modelID)
}

func usablePricingRate(rate pricingRate) bool {
	return rate.InputPerMillion > 0 && rate.OutputPerMillion > 0
}

// lookup preserves the native lookup helper used by focused resolution tests.
func (p pricingSnapshot) lookup(model string) (pricingRate, bool, bool) {
	match, ok := p.nativePricing(model)
	return match.Rate, match.Kind == source.PricingResolutionExact, ok
}

func modelPricingKeyMatches(model string, key string) bool {
	if !strings.HasPrefix(model, key) {
		return false
	}
	return len(model) == len(key) || model[len(key)] == '-'
}

func rateSummary(rate pricingRate) source.PricingRateSummary {
	return source.PricingRateSummary{
		InputPerMillion:       rate.InputPerMillion,
		CachedInputPerMillion: rate.CacheReadPerMillion,
		CacheWritePerMillion:  rate.CacheCreatePerMillion,
		OutputPerMillion:      rate.OutputPerMillion,
		Note:                  rate.Note,
	}
}

func defaultCurrency(pricing pricingSnapshot) string {
	if pricing.Currency != "" {
		return pricing.Currency
	}
	return "USD"
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
		SourceID:   source.SourceClaudeCode,
		SnapshotID: pricing.ID,
		Currency:   defaultCurrency(pricing),
		Models:     []source.PricingCatalogModel{},
		Note:       "Claude costs are reported when present, otherwise computed from this bundled pricing catalog",
	}
	keys := make([]string, 0, len(pricing.Models))
	for modelID := range pricing.Models {
		if pricing.isExactCatalogTarget(modelID) {
			keys = append(keys, modelID)
		}
	}
	sort.Strings(keys)
	for _, modelID := range keys {
		catalog.Models = append(catalog.Models, source.PricingCatalogModel{
			ModelID: modelID,
			Rate:    rateSummary(pricing.Models[modelID]),
		})
	}
	return catalog
}

func (s *Source) ResolvePricing(ctx context.Context, _ string, modelID string) source.PricingResolution {
	const providerID = "anthropic"
	pricing := s.loadPricing(ctx)
	match := pricing.resolve(providerID, modelID)
	resolution := source.PricingResolution{
		SourceID:        source.SourceClaudeCode,
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

func crossSourceNote(match pricingMatch) string {
	return "priced from the " + string(match.TargetSourceID) + " catalog model " + match.CanonicalModelID +
		" by a user pricing alias; only input, cached input, cache write and output rates carry across catalogs"
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
	if status == stats.CostMixed {
		prov.Note = "aggregate mixes reported, computed, approximate, or missing Claude Code costs"
	} else if status == stats.CostMissing {
		prov.Note = "aggregate cost is unknown because Claude Code cost data is missing"
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
