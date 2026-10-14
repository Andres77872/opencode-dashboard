package antigravity

import (
	"context"
	_ "embed"
	"encoding/json"
	"sort"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

//go:embed pricing_snapshot.json
var bundledPricing []byte

const apiEquivalentNote = "Estimated USD equivalent using Gemini standard API token prices, including thinking output. This is not Antigravity subscription spend or AI-credit consumption; tool fees and cache storage are excluded. Unknown internal models remain unpriced."

type datedRate struct {
	Effective string  `json:"effective"`
	Input     float64 `json:"input_per_million"`
	Cached    float64 `json:"cached_input_per_million"`
	Output    float64 `json:"output_per_million"`
}
type modelRate struct {
	Name    string      `json:"display_name"`
	Input   float64     `json:"input_per_million"`
	Cached  float64     `json:"cached_input_per_million"`
	Output  float64     `json:"output_per_million"`
	Changes []datedRate `json:"changes"`
}
type pricingSnapshot struct {
	ID       string               `json:"id"`
	Source   string               `json:"source"`
	Currency string               `json:"currency"`
	Models   map[string]modelRate `json:"models"`
	aliases  source.PricingAliasSnapshot
	rates    source.PricingRateIndex
}

func (s *Source) loadPricing(ctx context.Context) pricingSnapshot {
	p := s.basePricing
	p.aliases = source.CapturePricingAliases(s.opts.PricingAliases, s.opts.PricingRates, source.SourceAntigravity)
	p.rates = s.opts.PricingRates
	p.ID = source.EffectivePricingSnapshotIDForAliases(p.ID, p.aliases)
	return p
}

func (r modelRate) at(at time.Time) source.PricingRateSummary {
	result := source.PricingRateSummary{InputPerMillion: r.Input, CachedInputPerMillion: r.Cached, OutputPerMillion: r.Output}
	for _, change := range r.Changes {
		start, _ := time.Parse("2006-01-02", change.Effective)
		if !at.Before(start) {
			result.InputPerMillion = change.Input
			result.CachedInputPerMillion = change.Cached
			result.OutputPerMillion = change.Output
		}
	}
	return result
}

func (p pricingSnapshot) resolve(provider, model string, at time.Time) (source.PricingResolution, bool) {
	result := source.PricingResolution{SourceID: source.SourceAntigravity, TargetSourceID: source.SourceAntigravity, ProviderID: provider, ModelID: model, Kind: source.PricingResolutionUnpriced}
	target := source.PricingAliasTarget{SourceID: source.SourceAntigravity, ModelID: model}
	if p.aliases != nil {
		if alias, ok := p.aliases.ResolvePricingAlias(provider, model); ok {
			target = alias
			result.Kind = source.PricingResolutionUserAlias
			_, result.OverridesNative = p.Models[model]
		}
	}
	if target.SourceID == "" {
		target.SourceID = source.SourceAntigravity
	}
	var rate source.PricingRateSummary
	if target.SourceID == source.SourceAntigravity {
		modelRate, ok := p.Models[target.ModelID]
		if !ok {
			return result, false
		}
		rate = modelRate.at(at)
	} else {
		if p.rates == nil {
			return result, false
		}
		catalog, _, ok := p.rates.LookupPricingRate(target.SourceID, target.ModelID)
		if !ok {
			return result, false
		}
		rate = catalog.Rate
	}
	if !source.UsablePricingRate(rate) {
		return result, false
	}
	if result.Kind == source.PricingResolutionUnpriced {
		result.Kind = source.PricingResolutionExact
	}
	result.TargetSourceID = target.SourceID
	result.TargetModelID = target.ModelID
	result.Rate = &rate
	return result, true
}

func (p pricingSnapshot) price(msg *messageRecord) {
	prov := &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", PricingSnapshotID: p.ID, PricingSource: p.Source, MissingCount: 1, Note: apiEquivalentNote}
	msg.Entry.CostStatus = stats.CostMissing
	msg.Entry.CostProvenance = prov
	match, ok := p.resolve(msg.Entry.ProviderID, msg.Entry.ModelID, msg.Entry.TimeCreated)
	if !ok || msg.Entry.Tokens == nil {
		return
	}
	u := msg.Entry.Tokens
	rate := match.Rate
	write := rate.CacheWritePerMillion
	if write == 0 {
		write = rate.InputPerMillion
	}
	msg.Entry.Cost = (float64(u.Input)*rate.InputPerMillion + float64(u.Cache.Read)*rate.CachedInputPerMillion + float64(u.Cache.Write)*write + float64(u.Output+u.Reasoning)*rate.OutputPerMillion) / 1e6
	msg.Entry.CostStatus = stats.CostEstimatedAPIEquivalent
	prov.Status = msg.Entry.CostStatus
	prov.MissingCount = 0
	prov.ComputedCount = 1
}

func (s *Source) BasePricingCatalog(ctx context.Context) source.PricingCatalog {
	p := s.basePricing
	catalog := source.PricingCatalog{SourceID: source.SourceAntigravity, SnapshotID: p.ID, Currency: p.Currency, Models: []source.PricingCatalogModel{}, Note: apiEquivalentNote}
	for model, rate := range p.Models {
		catalog.Models = append(catalog.Models, source.PricingCatalogModel{ModelID: model, DisplayName: rate.Name, Rate: rate.at(time.Now().UTC())})
	}
	sort.Slice(catalog.Models, func(i, j int) bool { return catalog.Models[i].ModelID < catalog.Models[j].ModelID })
	return catalog
}
func (s *Source) PricingCatalog(ctx context.Context) source.PricingCatalog {
	catalog := s.BasePricingCatalog(ctx)
	catalog.SnapshotID = s.loadPricing(ctx).ID
	return catalog
}
func (s *Source) ResolvePricing(ctx context.Context, provider, model string) source.PricingResolution {
	result, _ := s.loadPricing(ctx).resolve(provider, model, time.Now().UTC())
	return result
}

func readPricing() pricingSnapshot {
	var p pricingSnapshot
	if err := json.Unmarshal(bundledPricing, &p); err != nil {
		panic(err)
	}
	return p
}
