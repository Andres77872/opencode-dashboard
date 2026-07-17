package tui

import (
	"math"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func processingModeDimensionFixture() stats.DailyDimensionStats {
	return stats.DailyDimensionStats{
		SourceID:   "codex",
		Dimension:  "processing_mode",
		Period:     "7d",
		CostStatus: stats.CostEstimatedAPIEquivalent,
		Days: []stats.DimensionDayStats{
			{
				Date:           "2026-07-16",
				Dimension:      "fast",
				Messages:       2,
				Cost:           0.35,
				CostStatus:     stats.CostEstimatedAPIEquivalent,
				CostProvenance: &stats.CostProvenance{Status: stats.CostEstimatedAPIEquivalent, Currency: "USD", ComputedCount: 2},
				Tokens: stats.TokenStats{
					Input: 100, Output: 20, Reasoning: 5,
					Cache: stats.CacheStats{Read: 40, Write: 10},
				},
			},
			{Date: "2026-07-16", Dimension: "standard", Messages: 1, Cost: 0.1, Tokens: stats.TokenStats{Input: 25}, CostStatus: stats.CostEstimatedAPIEquivalent, CostProvenance: &stats.CostProvenance{Status: stats.CostEstimatedAPIEquivalent, Currency: "USD", ComputedCount: 1}},
			{Date: "2026-07-17", Dimension: "unknown", Messages: 3, Cost: 0.05, Tokens: stats.TokenStats{Input: 50}, CostStatus: stats.CostEstimatedAPIEquivalent, CostProvenance: &stats.CostProvenance{Status: stats.CostEstimatedAPIEquivalent, Currency: "USD", ComputedCount: 3}},
		},
	}
}

func TestAggregateProcessingModeTotalsUsesExactTokenBuckets(t *testing.T) {
	totals := aggregateProcessingModeTotals(processingModeDimensionFixture().Days)
	if len(totals) != 4 {
		t.Fatalf("got %d mode totals, want 4", len(totals))
	}
	if got, want := totals[0].Mode, stats.ProcessingModeFast; got != want {
		t.Fatalf("first mode = %q, want %q", got, want)
	}
	if got, want := totals[0].Tokens, int64(175); got != want {
		t.Fatalf("fast tokens = %d, want %d (including cache read/write)", got, want)
	}
	if got, want := totals[0].Messages, int64(2); got != want {
		t.Fatalf("fast assistant requests = %d, want %d", got, want)
	}
	if got, want := totals[0].Cost, 0.35; math.Abs(got-want) > 1e-12 {
		t.Fatalf("fast API cost estimate = %f, want %f", got, want)
	}
	if totals[0].CostStatus != stats.CostEstimatedAPIEquivalent || totals[0].CostProvenance == nil || totals[0].CostProvenance.ComputedCount != 2 {
		t.Fatalf("fast cost metadata = %q/%#v, want estimated provenance for two requests", totals[0].CostStatus, totals[0].CostProvenance)
	}
}

func TestRenderDailyProcessingModesShowsHonestLabelsAndTable(t *testing.T) {
	result := renderDailyProcessingModes(newStyles(), 100, 30, processingModeDimensionFixture(), nil, "7d", dailyMetricTokens, false)
	for _, want := range []string{
		"Fast requested",
		"Standard requested",
		"Flex requested",
		"Tier unknown",
		"served tier is not recorded or server-confirmed",
		"Fast uses Priority API rates",
		"Flex uses Flex API rates",
		"Tier unknown stays unknown and falls back to Standard API rates",
		"not actual billed spend",
		"API cost estimate ~$0.50",
		"Daily table",
		"175",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("requested-mode Daily view missing %q:\n%s", want, result)
		}
	}
}

func TestRenderDailyProcessingModesSupportsUSDCostMetric(t *testing.T) {
	result := renderDailyProcessingModes(newStyles(), 100, 30, processingModeDimensionFixture(), nil, "7d", dailyMetricCost, false)
	for _, want := range []string{
		"API cost estimate (USD) by requested mode",
		"~$0.35",
		"~$0.10",
		"t switches cost/messages/tokens",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("requested-mode cost view missing %q:\n%s", want, result)
		}
	}
}

func TestRenderDailyProcessingModesShowsUnsupportedModeCostAsUnknown(t *testing.T) {
	fixture := processingModeDimensionFixture()
	fixture.Days = append(fixture.Days, stats.DimensionDayStats{
		Date: "2026-07-17", Dimension: "flex", Messages: 1, Cost: 0,
		Tokens:         stats.TokenStats{Input: 25},
		CostStatus:     stats.CostMissing,
		CostProvenance: &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", MissingCount: 1},
	})
	result := renderDailyProcessingModes(newStyles(), 100, 30, fixture, nil, "7d", dailyMetricCost, false)
	if !strings.Contains(result, "Flex requested") || !strings.Contains(result, "Unknown") {
		t.Fatalf("unsupported Flex pricing must render Unknown, not $0.00:\n%s", result)
	}
}

func TestRenderDailyProcessingModesDistinguishesFailureFromEmpty(t *testing.T) {
	result := renderDailyProcessingModes(
		newStyles(),
		100,
		30,
		stats.DailyDimensionStats{},
		assertiveError("dimension failed"),
		"7d",
		dailyMetricMessages,
		false,
	)
	if !strings.Contains(result, "Requested-mode breakdown unavailable") || !strings.Contains(result, "dimension failed") {
		t.Fatalf("dimension failure must be distinct from empty telemetry:\n%s", result)
	}
}

type assertiveError string

func (e assertiveError) Error() string { return string(e) }

func TestDailyRequestedModeLensIsCodexOnly(t *testing.T) {
	newTestModel := func(id source.SourceID) *model {
		return &model{
			selectedSource: id,
			styles:         newStyles(),
			keys:           defaultKeyMap(),
			dailyMetric:    dailyMetricCost,
			dailyBreakdown: dailyBreakdownOverall,
		}
	}

	codex := newTestModel(source.SourceCodex)
	_, _ = codex.updateDailyKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if codex.dailyBreakdown != dailyBreakdownProcessingMode || codex.dailyMetric != dailyMetricCost {
		t.Fatalf("Codex d key = lens %q metric %q, want requested mode/cost", codex.dailyBreakdown, codex.dailyMetric)
	}
	_, _ = codex.updateDailyKey(tea.KeyPressMsg{Code: 't', Text: "t"})
	if codex.dailyMetric != dailyMetricMessages {
		t.Fatalf("requested-mode first t key = %q, want messages", codex.dailyMetric)
	}
	_, _ = codex.updateDailyKey(tea.KeyPressMsg{Code: 't', Text: "t"})
	if codex.dailyMetric != dailyMetricTokens {
		t.Fatalf("requested-mode second t key = %q, want tokens", codex.dailyMetric)
	}
	_, _ = codex.updateDailyKey(tea.KeyPressMsg{Code: 't', Text: "t"})
	if codex.dailyMetric != dailyMetricCost {
		t.Fatalf("requested-mode third t key = %q, want cost", codex.dailyMetric)
	}

	opencode := newTestModel(source.SourceOpenCode)
	_, _ = opencode.updateDailyKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if opencode.dailyBreakdown != dailyBreakdownOverall {
		t.Fatalf("non-Codex d key changed lens to %q", opencode.dailyBreakdown)
	}
}
