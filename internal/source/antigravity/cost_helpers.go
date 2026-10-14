package antigravity

import "opencode-dashboard/internal/stats"

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
		prov.Note = "aggregate mixes estimated API-equivalent and missing Antigravity costs"
	case stats.CostMissing:
		prov.Note = "aggregate Antigravity cost is unknown because supported model pricing or request usage is unavailable"
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
