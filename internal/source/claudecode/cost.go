package claudecode

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"sort"
	"strings"

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

type costResult struct {
	Cost       float64
	Status     stats.CostStatus
	Provenance *stats.CostProvenance
}

func (s *Source) loadPricing(ctx context.Context) pricingSnapshot {
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

func computeCost(model string, usage tokenUsage, hasUsage bool, reported *float64, pricing pricingSnapshot) costResult {
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
	if !hasUsage || model == "" || len(pricing.Models) == 0 {
		return missingCost(currency)
	}
	rate, exact, ok := pricing.lookup(model)
	if !ok {
		return missingCost(currency)
	}
	cacheCreate5m, cacheCreate1h := usage.cacheCreateForPricing()
	cost := (float64(usage.Input)*rate.InputPerMillion +
		float64(usage.Output)*rate.OutputPerMillion +
		float64(usage.CacheRead)*rate.CacheReadPerMillion +
		float64(cacheCreate5m)*rate.CacheCreatePerMillion +
		float64(cacheCreate1h)*rate.cacheCreate1hPerMillion()) / 1_000_000
	status := stats.CostComputed
	note := "cost computed from transcript token usage and bundled pricing snapshot"
	if !exact || rate.Approximate {
		status = stats.CostApproximate
		note = "cost approximated from transcript token usage and family-level bundled pricing"
		if rate.Note != "" {
			note = rate.Note
		}
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

func (p pricingSnapshot) lookup(model string) (pricingRate, bool, bool) {
	if rate, ok := p.Models[model]; ok {
		return rate, true, true
	}
	keys := make([]string, 0, len(p.Models))
	for key := range p.Models {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if modelPricingKeyMatches(model, key) {
			rate := p.Models[key]
			rate.Approximate = true
			return rate, false, true
		}
	}
	return pricingRate{}, false, false
}

func modelPricingKeyMatches(model string, key string) bool {
	if !strings.HasPrefix(model, key) {
		return false
	}
	return len(model) == len(key) || model[len(key)] == '-'
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
