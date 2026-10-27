package cache

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

func costEntry(status stats.CostStatus, note string) stats.MessageEntry {
	return stats.MessageEntry{
		Role:           "assistant",
		CostStatus:     status,
		CostProvenance: &stats.CostProvenance{Status: status, Note: note},
	}
}

func TestCostAggKeepsAgreeingRequestNote(t *testing.T) {
	var agg costAgg
	for i := 0; i < 3; i++ {
		agg.add(costEntry(stats.CostEstimatedAPIEquivalent, "priority"))
	}
	if _, prov := agg.result(); prov == nil || prov.Note != "priority" {
		t.Fatalf("note = %#v, want the shared request note", prov)
	}
}

func TestCostAggDropsConflictingRequestNotes(t *testing.T) {
	// Alternating request notes used to concatenate into an unbounded
	// "A; B; A; B; ..." aggregate note.
	var agg costAgg
	for i := 0; i < 50; i++ {
		agg.add(costEntry(stats.CostEstimatedAPIEquivalent, "priority"))
		agg.add(costEntry(stats.CostEstimatedAPIEquivalent, "standard"))
		agg.add(costEntry(stats.CostMissing, "unknown"))
	}
	status, prov := agg.result()
	if status != stats.CostMixed || prov == nil {
		t.Fatalf("status/prov = %q/%#v, want mixed provenance", status, prov)
	}
	if prov.Note != "" {
		t.Fatalf("note = %q, want empty once request notes disagree", prov.Note)
	}
	if prov.ComputedCount != 100 || prov.MissingCount != 50 {
		t.Fatalf("counts = %d computed/%d missing, want 100/50", prov.ComputedCount, prov.MissingCount)
	}
}

func TestMergeCostKeepsFirstNote(t *testing.T) {
	cache := &stats.CostProvenance{Status: stats.CostMixed, Note: "source policy"}
	gap := &stats.CostProvenance{Status: stats.CostEstimatedAPIEquivalent, Note: "priority"}
	_, prov := mergeCost(stats.CostMixed, cache, stats.CostEstimatedAPIEquivalent, gap)
	if prov.Note != "source policy" {
		t.Fatalf("note = %q, want the cached source-level note", prov.Note)
	}
	_, prov = mergeCost(stats.CostMixed, &stats.CostProvenance{Status: stats.CostMixed}, stats.CostEstimatedAPIEquivalent, gap)
	if prov.Note != "priority" {
		t.Fatalf("note = %q, want the gap note when the cache has none", prov.Note)
	}
}
