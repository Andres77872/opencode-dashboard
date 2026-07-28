package codex

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

//go:embed pricing_snapshot.json
var defaultPricingFS embed.FS

const apiEquivalentNote = "API-equivalent estimate in USD using official OpenAI API per-token rates for each request's requested processing tier (Fast maps to Priority, Flex maps to Flex, and Standard maps to Standard). An unknown processing tier remains unclassified and defaults to Standard pricing for this estimate only. Local Codex transcripts expose the requested tier, not the tier actually served; this is not actual API-billed spend."

const (
	priorityMaxInputTokens    = 272_000
	standardAPIEquivalentNote = "API-equivalent estimate in USD using official OpenAI Standard API per-token rates for the requested Standard tier. Local Codex transcripts expose the requested tier, not the tier actually served; this is not actual API-billed spend."
	unknownAPIEquivalentNote  = "API-equivalent estimate in USD using official OpenAI Standard API per-token rates because the processing tier is unknown. The unknown processing tier remains classified as unknown and defaults to Standard pricing for this estimate only; this does not mean Standard was requested, served, or billed, and this is not actual API-billed spend."
	priorityAPIEquivalentNote = "API-equivalent estimate in USD using official OpenAI Priority API per-token rates because Fast was requested. Local Codex transcripts expose the requested tier, not the tier actually served; this is not actual API-billed spend."
	flexAPIEquivalentNote     = "API-equivalent estimate in USD using official OpenAI Flex API per-token rates for the requested Flex tier. Local Codex transcripts expose the requested tier, not the tier actually served; this is not actual API-billed spend."
)

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
	InputPerMillion             float64          `json:"input_per_million"`
	CachedInputPerMillion       float64          `json:"cached_input_per_million"`
	OutputPerMillion            float64          `json:"output_per_million"`
	CacheWriteInputPerMillion   float64          `json:"cache_write_input_per_million"`
	ReasoningOutputBilledAs     string           `json:"reasoning_output_billed_as"`
	LongContextThresholdTokens  int64            `json:"long_context_threshold_input_tokens"`
	LongContextInputMultiplier  float64          `json:"long_context_input_multiplier"`
	LongContextOutputMultiplier float64          `json:"long_context_output_multiplier"`
	CacheWriteNote              string           `json:"cache_write_note"`
	Priority                    *tierPricingRate `json:"priority,omitempty"`
	Flex                        *tierPricingRate `json:"flex,omitempty"`
}

type tierPricingRate struct {
	InputPerMillion           float64 `json:"input_per_million"`
	CachedInputPerMillion     float64 `json:"cached_input_per_million"`
	OutputPerMillion          float64 `json:"output_per_million"`
	CacheWriteInputPerMillion float64 `json:"cache_write_input_per_million"`
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
	return m.TargetSourceID != "" && m.TargetSourceID != source.SourceCodex
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
	aliases := source.CapturePricingAliases(s.pricingAliases, s.pricingRates, source.SourceCodex)
	pricing.ID = source.EffectivePricingSnapshotIDForAliases(pricing.ID, aliases)
	pricing.aliases = aliases
	pricing.rates = s.pricingRates
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

func computeCost(model, providerID string, tokens stats.TokenStats, maxInputSnapshot int64, pricing pricingSnapshot, processingModes ...stats.ProcessingMode) costResult {
	currency := pricing.Currency
	if currency == "" {
		currency = "USD"
	}
	if model == "" {
		return missingCost(currency)
	}
	match := pricing.resolve(providerID, model)
	if !usablePricingRate(match.Rate) {
		return missingCost(currency)
	}
	processingMode := stats.ProcessingModeUnknown
	if len(processingModes) > 0 {
		processingMode = processingModes[0]
	}
	selectedRate, note, ok := selectTierRate(match, processingMode)
	if !ok {
		return missingTierCost(currency, processingMode, "official per-token rates are unavailable for this model")
	}
	if !match.foreign() && processingMode == stats.ProcessingModeFast && maxInputSnapshot > priorityMaxInputTokens {
		return missingTierCost(currency, processingMode, "official Priority pricing is unavailable above the model's 272K input-token threshold")
	}
	// TokenStats buckets are disjoint: Input excludes cache reads/writes, and
	// Output excludes reasoning. Reasoning bills at the output rate.
	normalInput := tokens.Input
	cachedInput := tokens.Cache.Read
	cacheWriteInput := tokens.Cache.Write
	outputBillable := tokens.Output + tokens.Reasoning
	inputMultiplier := 1.0
	outputMultiplier := 1.0
	if match.Rate.LongContextThresholdTokens > 0 && maxInputSnapshot > match.Rate.LongContextThresholdTokens {
		inputMultiplier = nonZero(match.Rate.LongContextInputMultiplier, 1)
		outputMultiplier = nonZero(match.Rate.LongContextOutputMultiplier, 1)
	}
	cost := ((float64(normalInput)*selectedRate.InputPerMillion + float64(cachedInput)*selectedRate.CachedInputPerMillion + float64(cacheWriteInput)*selectedRate.CacheWriteInputPerMillion) * inputMultiplier / 1_000_000) +
		(float64(outputBillable) * selectedRate.OutputPerMillion * outputMultiplier / 1_000_000)
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

func selectTierRate(match pricingMatch, processingMode stats.ProcessingMode) (tierPricingRate, string, bool) {
	rate := match.Rate
	if match.foreign() {
		// A borrowed catalog publishes one rate per model, not one per OpenAI
		// processing tier. Charging the standard rate for every tier keeps the
		// cost present and says so, which beats reporting Fast/Flex requests as
		// unpriced purely because the target vendor has no tier concept.
		return standardTierRate(rate), crossSourceNote(match), true
	}
	switch processingMode {
	case stats.ProcessingModeFast:
		if rate.Priority == nil || rate.Priority.InputPerMillion == 0 || rate.Priority.OutputPerMillion == 0 {
			return tierPricingRate{}, priorityAPIEquivalentNote, false
		}
		return *rate.Priority, priorityAPIEquivalentNote, true
	case stats.ProcessingModeFlex:
		if rate.Flex == nil || rate.Flex.InputPerMillion == 0 || rate.Flex.OutputPerMillion == 0 {
			return tierPricingRate{}, flexAPIEquivalentNote, false
		}
		return *rate.Flex, flexAPIEquivalentNote, true
	case stats.ProcessingModeStandard:
		return standardTierRate(rate), standardAPIEquivalentNote, true
	default:
		return standardTierRate(rate), unknownAPIEquivalentNote, true
	}
}

func standardTierRate(rate pricingRate) tierPricingRate {
	return tierPricingRate{
		InputPerMillion:           rate.InputPerMillion,
		CachedInputPerMillion:     rate.CachedInputPerMillion,
		OutputPerMillion:          rate.OutputPerMillion,
		CacheWriteInputPerMillion: rate.CacheWriteInputPerMillion,
	}
}

// dateSuffixPattern matches dated model releases like "gpt-5.5-2026-01-15".
var dateSuffixPattern = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// nativePricing resolves a transcript model id against the bundled snapshot:
// exact key first, then with one trailing release-date suffix stripped. Named
// variants deliberately stay unresolved because they can carry different rates.
func (p pricingSnapshot) nativePricing(model string) (pricingMatch, bool) {
	if rate, ok := p.Models[model]; ok {
		return pricingMatch{Rate: rate, CanonicalModelID: model, Kind: source.PricingResolutionExact}, true
	}
	if stripped := dateSuffixPattern.ReplaceAllString(model, ""); stripped != model {
		if rate, ok := p.Models[stripped]; ok {
			return pricingMatch{Rate: rate, CanonicalModelID: stripped, Kind: source.PricingResolutionFallback}, true
		}
	}
	return pricingMatch{}, false
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
	if p.aliases == nil {
		return pricingMatch{}, false
	}
	target, ok := p.aliases.ResolvePricingAlias(providerID, model)
	if !ok || target.ModelID == "" {
		return pricingMatch{}, false
	}
	if target.SourceID == "" || target.SourceID == source.SourceCodex {
		rate, exact := p.Models[target.ModelID]
		if !exact || !usablePricingRate(rate) {
			return pricingMatch{}, false
		}
		return pricingMatch{
			Rate:             rate,
			CanonicalModelID: target.ModelID,
			Kind:             source.PricingResolutionUserAlias,
			TargetSourceID:   source.SourceCodex,
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

// foreignRate adapts another catalog's shared rate summary to Codex's rate
// shape. Processing-tier rates and long-context multipliers are OpenAI-specific
// and have no cross-vendor equivalent, so they are deliberately absent and
// selectTierRate falls back to the standard rate for every tier.
func foreignRate(summary source.PricingRateSummary) pricingRate {
	return pricingRate{
		InputPerMillion:           summary.InputPerMillion,
		CachedInputPerMillion:     summary.CachedInputPerMillion,
		OutputPerMillion:          summary.OutputPerMillion,
		CacheWriteInputPerMillion: summary.CacheWritePerMillion,
	}
}

func crossSourceNote(match pricingMatch) string {
	return "priced from the " + string(match.TargetSourceID) + " catalog model " + match.CanonicalModelID +
		" by a user pricing alias; only input, cached input, cache write and output rates carry across catalogs, so processing-tier and long-context rates do not apply"
}

func usablePricingRate(rate pricingRate) bool {
	return rate.InputPerMillion > 0 && rate.OutputPerMillion > 0
}

func rateSummary(rate pricingRate) source.PricingRateSummary {
	return source.PricingRateSummary{
		InputPerMillion:       rate.InputPerMillion,
		CachedInputPerMillion: rate.CachedInputPerMillion,
		CacheWritePerMillion:  rate.CacheWriteInputPerMillion,
		OutputPerMillion:      rate.OutputPerMillion,
		Note:                  rate.CacheWriteNote,
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
		SourceID:   source.SourceCodex,
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
		catalog.Models = append(catalog.Models, source.PricingCatalogModel{
			ModelID: modelID,
			Rate:    rateSummary(pricing.Models[modelID]),
		})
	}
	return catalog
}

func (s *Source) ResolvePricing(ctx context.Context, providerID, modelID string) source.PricingResolution {
	pricing := s.loadPricing(ctx)
	match := pricing.resolve(providerID, modelID)
	resolution := source.PricingResolution{
		SourceID:        source.SourceCodex,
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

func nonZero(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func missingCost(currency string) costResult {
	return costResult{
		Status: stats.CostMissing,
		Provenance: &stats.CostProvenance{
			Status:       stats.CostMissing,
			Currency:     currency,
			MissingCount: 1,
			Note:         "Codex cost is unknown because supported pricing/model usage is unavailable",
		},
	}
}

func missingTierCost(currency string, processingMode stats.ProcessingMode, reason string) costResult {
	tier := string(processingMode)
	if processingMode == stats.ProcessingModeFast {
		tier = "Priority (Fast requested)"
	}
	return costResult{
		Status: stats.CostMissing,
		Provenance: &stats.CostProvenance{
			Status:       stats.CostMissing,
			Currency:     currency,
			MissingCount: 1,
			Note:         "Codex " + tier + " API-equivalent cost is unknown because " + reason,
		},
	}
}

func aggregateCostProvenance(messages []*messageRecord) (float64, stats.TokenStats, stats.CostStatus, *stats.CostProvenance) {
	var totalCost float64
	var tokens stats.TokenStats
	prov := &stats.CostProvenance{Currency: "USD"}
	statuses := make(map[stats.CostStatus]bool)
	for _, msg := range messages {
		// User-prompt rows carry no cost/tokens; only assistant API-request rows
		// contribute to cost status and token totals.
		if msg.Entry.Role != "assistant" {
			continue
		}
		if msg.Entry.Tokens != nil {
			tokens.Input += msg.Entry.Tokens.Input
			tokens.Output += msg.Entry.Tokens.Output
			tokens.Reasoning += msg.Entry.Tokens.Reasoning
			tokens.Cache.Read += msg.Entry.Tokens.Cache.Read
			tokens.Cache.Write += msg.Entry.Tokens.Cache.Write
		}
		totalCost += msg.Entry.Cost
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
		prov.Note = "aggregate mixes estimated API-equivalent and missing Codex costs"
	case stats.CostMissing:
		prov.Note = "aggregate Codex cost is unknown because supported pricing/model usage is unavailable"
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
