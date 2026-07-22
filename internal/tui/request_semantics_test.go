package tui

import (
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestOverviewRendersRequestsSeparatelyAndCountsRequestOnlyActivity(t *testing.T) {
	data := dashboardData{AllOverview: source.AllSourcesOverview{
		Total: stats.OverviewStats{
			Sessions: 2,
			Messages: 11,
			Requests: 7,
			Days:     1,
		},
		Sources: []source.SourceOverview{
			{
				SourceID: "kimi_code",
				Label:    "Kimi Code",
				Overview: stats.OverviewStats{Messages: 11, Requests: 7},
			},
			{
				SourceID: "codex",
				Label:    "Codex",
			},
		},
	}}

	result := renderOverview(newStyles(), 140, 30, data)
	for _, want := range []string{"Messages", "Requests", "MSGS", "REQS", "1 / 2", "outbound attempts"} {
		if !strings.Contains(result, want) {
			t.Errorf("overview missing %q:\n%s", want, result)
		}
	}
}

func TestDailyRequestsMetricUsesRequestCountWithoutChangingMessages(t *testing.T) {
	day := stats.DayStats{Messages: 91, Requests: 13}

	if got := dailyMetricValue(day, dailyMetricRequests); got != 13 {
		t.Fatalf("requests metric = %.0f, want 13", got)
	}
	if got := dailyMetricValue(day, dailyMetricMessages); got != 91 {
		t.Fatalf("messages metric = %.0f, want transcript count 91", got)
	}
	if got := renderDailyMetricLabel(dailyMetricRequests); got != "requests" {
		t.Fatalf("requests metric label = %q, want requests", got)
	}

	daily := stats.DailyStats{Days: []stats.DayStats{{Date: "2026-07-20", Sessions: 1, Messages: 91, Requests: 13}}}
	result := renderDaily(newStyles(), 100, 24, daily, "7d", dailyMetricRequests, false, 0)
	if !strings.Contains(result, "Daily activity • 7d • requests") {
		t.Fatalf("daily requests lens missing from render:\n%s", result)
	}
}

func TestCombineTrendRetainsRequests(t *testing.T) {
	sources := []source.SourceOverview{
		{Trend: []stats.DayStats{{Date: "2026-07-20", Messages: 4, Requests: 3}}},
		{Trend: []stats.DayStats{{Date: "2026-07-20", Messages: 5, Requests: 7}}},
	}

	got := combineTrend(sources)
	if len(got) != 1 {
		t.Fatalf("combined trend rows = %d, want 1", len(got))
	}
	if got[0].Messages != 9 || got[0].Requests != 10 {
		t.Fatalf("combined trend = messages %d, requests %d; want 9 and 10", got[0].Messages, got[0].Requests)
	}
}

func TestOverviewTrendPlotsRequestsRatherThanMessages(t *testing.T) {
	result := strings.Join(renderOverviewTrend(newStyles(), []stats.DayStats{
		{Date: "2026-07-19", Messages: 900, Requests: 3},
		{Date: "2026-07-20", Messages: 800, Requests: 17},
	}), "\n")

	if !strings.Contains(result, "Activity trend (requests/day)") {
		t.Fatalf("request trend title missing:\n%s", result)
	}
	if strings.Contains(result, "messages/day") {
		t.Fatalf("overview trend still describes transcript messages:\n%s", result)
	}
	for _, want := range []string{"3", "17"} {
		if !strings.Contains(result, want) {
			t.Fatalf("request trend missing request count %s:\n%s", want, result)
		}
	}
}

func TestDailyFullFooterKeepsMessagesAndRequestsDistinct(t *testing.T) {
	daily := stats.DailyStats{Days: []stats.DayStats{{
		Date:     "2026-07-20",
		Sessions: 2,
		Messages: 91,
		Requests: 13,
	}}}

	footer := renderDailyFooter(daily, false, true)
	if !strings.Contains(footer, "91 messages • 13 requests") {
		t.Fatalf("full daily footer does not distinguish messages and requests: %s", footer)
	}
}

func TestOverviewDisclosesIncompleteKimiRequestAccounting(t *testing.T) {
	data := dashboardData{AllOverview: source.AllSourcesOverview{Sources: []source.SourceOverview{
		{
			SourceID: string(source.SourceKimiCode),
			Overview: stats.OverviewStats{RequestAccounting: &stats.RequestAccounting{
				UsageUnavailable: 2,
				TraceCoverage:    stats.TraceCoverageMixed,
			}},
		},
	}}}

	result := renderOverview(newStyles(), 120, 30, data)
	for _, want := range []string{"Kimi accounting", "usage-unavailable requests: 2", "trace coverage: mixed", "tokens/cost are unknown (not zero)"} {
		if !strings.Contains(result, want) {
			t.Fatalf("overview Kimi disclosure missing %q:\n%s", want, result)
		}
	}
}

func TestDailyDisclosesIncompleteKimiRequestAccounting(t *testing.T) {
	daily := stats.DailyStats{
		Days: []stats.DayStats{{Date: "2026-07-20", Requests: 4}},
		RequestAccounting: &stats.RequestAccounting{
			UsageUnavailable: 1,
			TraceCoverage:    stats.TraceCoverageSuccessfulOnly,
		},
	}

	result := renderDaily(newStyles(), 120, 30, daily, "7d", dailyMetricRequests, false, 0)
	for _, want := range []string{"usage-unavailable requests: 1", "trace coverage: successful_only", "tokens/cost are unknown (not zero)"} {
		if !strings.Contains(result, want) {
			t.Fatalf("daily Kimi disclosure missing %q:\n%s", want, result)
		}
	}
}

func TestKimiAccountingDisclosureIsHiddenOnlyForCompleteAvailableUsage(t *testing.T) {
	complete := &stats.RequestAccounting{UsageRecorded: 3, TraceCoverage: stats.TraceCoverageComplete}
	if got := renderKimiAccountingDisclosure(newStyles(), complete); got != "" {
		t.Fatalf("complete accounting produced disclosure: %q", got)
	}

	incomplete := &stats.RequestAccounting{TraceCoverage: stats.TraceCoverageUnknown}
	got := renderKimiAccountingDisclosure(newStyles(), incomplete)
	if !strings.Contains(got, "usage-unavailable requests: 0") || !strings.Contains(got, "trace coverage: unknown") {
		t.Fatalf("coverage-only disclosure missing zero unavailable count or coverage: %q", got)
	}
}
