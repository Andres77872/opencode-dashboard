package tui

import (
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestCodexDailyCostSurfacesUseProvenance(t *testing.T) {
	missing := &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", MissingCount: 1}
	daily := stats.DailyStats{
		SourceID:       "codex",
		Granularity:    stats.GranularityDay,
		CostStatus:     stats.CostMissing,
		CostProvenance: missing,
		Days: []stats.DayStats{{
			SourceID:       "codex",
			Date:           "2026-07-17",
			Sessions:       1,
			Messages:       1,
			Cost:           0,
			CostStatus:     stats.CostMissing,
			CostProvenance: missing,
		}},
	}

	result := renderDaily(newStyles(), 100, 30, daily, "7d", dailyMetricCost, false, 0)
	for _, want := range []string{"API cost estimate (USD)", "Total Unknown", "Latest 07-17 • Unknown"} {
		if !strings.Contains(result, want) {
			t.Fatalf("Codex daily missing-cost view must contain %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "Total $0.00") || strings.Contains(result, "Latest 07-17 • $0.00") {
		t.Fatalf("Codex daily missing cost must not render as raw zero spend:\n%s", result)
	}
}

func TestCodexMessageAndSessionHeadersUseProvenance(t *testing.T) {
	missing := &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", MissingCount: 1}
	message := stats.MessageEntry{
		SourceID:       "codex",
		Role:           "assistant",
		TimeCreated:    time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		CostStatus:     stats.CostMissing,
		CostProvenance: missing,
	}
	messageRow := renderMessageRow(newStyles(), message, "codex", 120, false)
	if !strings.Contains(messageRow, "Unknown") || strings.Contains(messageRow, "$0.00") {
		t.Fatalf("Codex message row must show missing cost as Unknown:\n%s", messageRow)
	}

	detail := &stats.SessionDetail{
		SourceID:       "codex",
		Title:          "Unsupported pricing",
		MessageCount:   1,
		CostStatus:     stats.CostMissing,
		CostProvenance: missing,
	}
	session := renderSessionDetailOverlay(newStyles(), 100, 30, sessionOverlayState{detail: detail})
	if !strings.Contains(session, "project - • messages 1 • cost Unknown") || strings.Contains(session, "project - • messages 1 • cost $0.00") {
		t.Fatalf("Codex session header must show missing cost as Unknown:\n%s", session)
	}
}

func TestSessionKPIsUseListAndRowCostProvenance(t *testing.T) {
	missing := &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", MissingCount: 1}
	list := stats.SessionList{
		SourceID:       "codex",
		CostStatus:     stats.CostMissing,
		CostProvenance: missing,
		Sessions: []stats.SessionEntry{{
			SourceID:       "codex",
			ID:             "session-1",
			Title:          "Unsupported pricing",
			CostStatus:     stats.CostMissing,
			CostProvenance: missing,
		}},
	}
	result := renderSessions(newStyles(), 100, 30, list, sessionsViewState{})
	if !strings.Contains(result, "Unknown") {
		t.Fatalf("Codex session KPI must preserve missing cost provenance:\n%s", result)
	}
}
