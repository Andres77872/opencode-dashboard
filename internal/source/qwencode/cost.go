package qwencode

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"strings"
	"time"

	"opencode-dashboard/internal/stats"
)

//go:embed pricing_snapshot.json
var defaultPricingFS embed.FS

const apiEquivalentNote = "Estimated from Alibaba Cloud Model Studio list prices as an API-equivalent value. Qwen Code OAuth coding plans and Token Plan bundles are not billed per transcript token, so this is not actual subscription spend."

type pricingSnapshot struct {
	ID          string                 `json:"id"`
	RetrievedAt string                 `json:"retrieved_at"`
	Source      string                 `json:"source"`
	Currency    string                 `json:"currency"`
	Models      map[string]pricingRate `json:"models"`
	Aliases     map[string]string      `json:"aliases"`
}

type pricingRate struct {
	DisplayName        string  `json:"display_name"`
	ContextTokens      int64   `json:"context_tokens"`
	InputPerMillion    float64 `json:"input_per_million"`
	CacheHitPerMillion float64 `json:"cache_hit_input_per_million"`
	OutputPerMillion   float64 `json:"output_per_million"`
	AvailabilityNote   string  `json:"availability_note,omitempty"`
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

func (p pricingSnapshot) lookup(model string) (pricingRate, bool) {
	model = strings.TrimSpace(model)
	if rate, ok := p.Models[model]; ok {
		return rate, true
	}
	canonical, ok := p.Aliases[model]
	if !ok {
		return pricingRate{}, false
	}
	rate, ok := p.Models[canonical]
	return rate, ok
}

func computeCost(model string, tokens stats.TokenStats, pricing pricingSnapshot) costResult {
	currency := defaultCurrency(pricing)
	rate, ok := pricing.lookup(model)
	if !ok || rate.InputPerMillion <= 0 || rate.OutputPerMillion <= 0 {
		return missingCost(currency)
	}
	// Qwen has no separate cache-write tier: cache writes (always zero in
	// recorded data) bill at the normal input rate, cache hits at the
	// discounted cached-input rate, and reasoning at the output rate.
	inputMiss := tokens.Input + tokens.Cache.Write
	cost := (float64(inputMiss)*rate.InputPerMillion +
		float64(tokens.Cache.Read)*rate.CacheHitPerMillion +
		float64(tokens.Output+tokens.Reasoning)*rate.OutputPerMillion) / 1_000_000
	return costResult{
		Cost:   cost,
		Status: stats.CostEstimatedAPIEquivalent,
		Provenance: &stats.CostProvenance{
			Status:            stats.CostEstimatedAPIEquivalent,
			Currency:          currency,
			PricingSnapshotID: pricing.ID,
			PricingSource:     pricing.Source,
			ComputedCount:     1,
			Note:              apiEquivalentNote,
		},
	}
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
