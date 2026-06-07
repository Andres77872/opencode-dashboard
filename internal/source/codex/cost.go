package codex

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"time"

	"opencode-dashboard/internal/stats"
)

//go:embed pricing_snapshot.json
var defaultPricingFS embed.FS

const apiEquivalentNote = "Estimated using OpenAI API rates as an API-equivalent value. Codex subscription plans are flat-fee/credit-based; this is not actual billed spend."

type pricingSnapshot struct {
	ID          string                 `json:"id"`
	RetrievedAt string                 `json:"retrieved_at"`
	Source      string                 `json:"source"`
	Currency    string                 `json:"currency"`
	Models      map[string]pricingRate `json:"models"`
}

type pricingRate struct {
	InputPerMillion             float64 `json:"input_per_million"`
	CachedInputPerMillion       float64 `json:"cached_input_per_million"`
	OutputPerMillion            float64 `json:"output_per_million"`
	CacheWriteInputPerMillion   float64 `json:"cache_write_input_per_million"`
	ReasoningOutputBilledAs     string  `json:"reasoning_output_billed_as"`
	LongContextThresholdTokens  int64   `json:"long_context_threshold_input_tokens"`
	LongContextInputMultiplier  float64 `json:"long_context_input_multiplier"`
	LongContextOutputMultiplier float64 `json:"long_context_output_multiplier"`
	FastModeMultiplier          float64 `json:"fast_mode_multiplier"`
	CacheWriteNote              string  `json:"cache_write_note"`
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

func computeCost(model string, tokens stats.TokenStats, maxInputSnapshot int64, pricing pricingSnapshot) costResult {
	currency := pricing.Currency
	if currency == "" {
		currency = "USD"
	}
	if model == "" || len(pricing.Models) == 0 {
		return missingCost(currency)
	}
	rate, ok := pricing.Models[model]
	if !ok || rate.InputPerMillion == 0 || rate.OutputPerMillion == 0 {
		return missingCost(currency)
	}
	normalInput := tokens.Input - tokens.Cache.Read
	if normalInput < 0 {
		normalInput = 0
	}
	cachedInput := tokens.Cache.Read
	outputBillable := tokens.Output + tokens.Reasoning
	inputMultiplier := 1.0
	outputMultiplier := 1.0
	if rate.LongContextThresholdTokens > 0 && maxInputSnapshot > rate.LongContextThresholdTokens {
		inputMultiplier = nonZero(rate.LongContextInputMultiplier, 1)
		outputMultiplier = nonZero(rate.LongContextOutputMultiplier, 1)
	}
	cost := ((float64(normalInput)*rate.InputPerMillion + float64(cachedInput)*rate.CachedInputPerMillion) * inputMultiplier / 1_000_000) +
		(float64(outputBillable) * rate.OutputPerMillion * outputMultiplier / 1_000_000)
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

func aggregateCostProvenance(messages []*messageRecord) (float64, stats.TokenStats, stats.CostStatus, *stats.CostProvenance) {
	var totalCost float64
	var tokens stats.TokenStats
	prov := &stats.CostProvenance{Currency: "USD"}
	statuses := make(map[stats.CostStatus]bool)
	for _, msg := range messages {
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
