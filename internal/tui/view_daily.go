package tui

import (
	"fmt"
	"math"
	"strings"

	"opencode-dashboard/internal/stats"
)

func renderDaily(s styles, width, height int, daily stats.DailyStats, period string, metric dailyMetric) string {
	if len(daily.Days) == 0 {
		return s.EmptyState.Render("No daily activity is available yet.")
	}

	maxValue := 0.0
	for _, day := range daily.Days {
		value := dailyMetricValue(day, metric)
		if value > maxValue {
			maxValue = value
		}
	}

	barWidth := max(width-32, 8)
	lines := []string{
		s.PanelTitle.Render(fmt.Sprintf("Daily activity • %s • %s", period, renderDailyMetricLabel(metric))),
		s.Muted.Render(renderDailySummary(daily, metric)),
		"",
	}

	maxRows := max(height-5, 5)
	start := max(len(daily.Days)-maxRows, 0)
	visible := daily.Days[start:]
	for i, day := range visible {
		value := dailyMetricValue(day, metric)
		bar := asciiBar(value, maxValue, barWidth)
		if bar == "" {
			bar = strings.Repeat("·", min(3, barWidth))
		}

		trend := renderDailyTrendGlyph(s, visible, i, metric)
		primary := padLeft(renderDailyMetricValue(metric, value, true), 8)
		secondary := s.Muted.Render(renderDailySecondary(day, metric))
		lines = append(lines, fmt.Sprintf("%s %s %s %s %s", day.Date[5:], trend, padRight(s.Accent.Render(bar), barWidth), primary, secondary))
	}

	lines = append(lines, "", s.Muted.Render(renderDailyFooter(daily)))
	return joinLines(lines...)
}

func renderDailyMetricLabel(metric dailyMetric) string {
	switch metric {
	case dailyMetricSessions:
		return "sessions"
	case dailyMetricMessages:
		return "messages"
	case dailyMetricTokens:
		return "tokens"
	default:
		return "cost"
	}
}

func dailyMetricValue(day stats.DayStats, metric dailyMetric) float64 {
	switch metric {
	case dailyMetricSessions:
		return float64(day.Sessions)
	case dailyMetricMessages:
		return float64(day.Messages)
	case dailyMetricTokens:
		return float64(totalDayTokens(day.Tokens))
	default:
		return day.Cost
	}
}

func renderDailyMetricValue(metric dailyMetric, value float64, compact bool) string {
	if metric == dailyMetricCost {
		return formatMoney(value)
	}

	rounded := int64(math.Round(value))
	if compact {
		return formatCompactInt(rounded)
	}
	return formatInt(rounded)
}

func renderDailySummary(daily stats.DailyStats, metric dailyMetric) string {
	if len(daily.Days) == 0 {
		return ""
	}

	total := 0.0
	peak := daily.Days[0]
	peakValue := dailyMetricValue(peak, metric)
	for _, day := range daily.Days {
		value := dailyMetricValue(day, metric)
		total += value
		if value > peakValue {
			peak = day
			peakValue = value
		}
	}

	parts := []string{
		fmt.Sprintf("Total %s", renderDailyMetricValue(metric, total, true)),
		fmt.Sprintf("Avg/day %s", renderDailyMetricValue(metric, total/float64(len(daily.Days)), true)),
	}

	if len(daily.Days) > 1 {
		latest := daily.Days[len(daily.Days)-1]
		previous := daily.Days[len(daily.Days)-2]
		parts = append(parts, fmt.Sprintf("Latest %s %s vs %s", latest.Date[5:], renderDailyDelta(latest, previous, metric), previous.Date[5:]))
	}

	parts = append(parts, fmt.Sprintf("Peak %s %s", peak.Date[5:], renderDailyMetricValue(metric, peakValue, true)))
	return strings.Join(parts, " • ")
}

func renderDailyFooter(daily stats.DailyStats) string {
	latest := daily.Days[len(daily.Days)-1]
	return fmt.Sprintf(
		"Latest %s • %s • %s sessions • %s messages • %s tokens",
		latest.Date,
		formatMoney(latest.Cost),
		formatInt(latest.Sessions),
		formatInt(latest.Messages),
		formatCompactInt(totalDayTokens(latest.Tokens)),
	)
}

func renderDailySecondary(day stats.DayStats, metric dailyMetric) string {
	switch metric {
	case dailyMetricSessions:
		return formatMoney(day.Cost)
	case dailyMetricMessages, dailyMetricTokens:
		return padLeft(formatCompactInt(day.Sessions), 4) + " sess"
	default:
		return padLeft(formatCompactInt(day.Sessions), 4) + " sess"
	}
}

func renderDailyTrendGlyph(s styles, visible []stats.DayStats, index int, metric dailyMetric) string {
	if index == 0 {
		return s.Muted.Render("·")
	}

	current := dailyMetricValue(visible[index], metric)
	previous := dailyMetricValue(visible[index-1], metric)
	switch {
	case current > previous:
		return s.Success.Render("↑")
	case current < previous:
		return s.Danger.Render("↓")
	default:
		return s.Info.Render("→")
	}
}

func renderDailyDelta(current, previous stats.DayStats, metric dailyMetric) string {
	delta := dailyMetricValue(current, metric) - dailyMetricValue(previous, metric)
	switch {
	case delta > 0:
		return "↑ " + renderDailyMetricValue(metric, delta, true)
	case delta < 0:
		return "↓ " + renderDailyMetricValue(metric, math.Abs(delta), true)
	default:
		return "→ flat"
	}
}

func totalDayTokens(tokens stats.TokenStats) int64 {
	return tokens.Input + tokens.Output + tokens.Reasoning
}
