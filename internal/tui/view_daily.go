package tui

import (
	"fmt"
	"math"
	"strings"

	"opencode-dashboard/internal/stats"
)

func renderDaily(s styles, width, height int, daily stats.DailyStats, period string, metric dailyMetric, loading bool) string {
	if loading {
		return s.EmptyState.Render("Loading period data...")
	}
	if len(daily.Days) == 0 {
		return s.EmptyState.Render("No daily activity is available yet.")
	}

	isHourly := daily.Granularity == stats.GranularityHour

	maxValue := 0.0
	for _, day := range daily.Days {
		value := dailyMetricValue(day, metric)
		if value > maxValue {
			maxValue = value
		}
	}

	barWidth := max(width-32, 8)
	var periodLabel string
	if isHourly {
		periodLabel = "hourly distribution"
	} else {
		periodLabel = period
	}
	lines := []string{
		s.PanelTitle.Render(fmt.Sprintf("Daily activity • %s • %s", periodLabel, renderDailyMetricLabel(metric))),
		s.Muted.Render(renderDailySummary(daily, metric, isHourly)),
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
		dateLabel := renderDateLabel(day.Date, isHourly)
		lines = append(lines, fmt.Sprintf("%s %s %s %s %s", dateLabel, trend, padRight(s.Accent.Render(bar), barWidth), primary, secondary))
	}

	lines = append(lines, "", s.Muted.Render(renderDailyFooter(daily, isHourly)))
	return joinLines(lines...)
}

func renderDateLabel(date string, isHourly bool) string {
	if isHourly {
		if len(date) >= 16 {
			return date[11:13] + "h"
		}
		return date
	}
	if len(date) >= 10 {
		return date[5:10]
	}
	return date
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

func renderDailySummary(daily stats.DailyStats, metric dailyMetric, isHourly bool) string {
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

	avgLabel := "Avg/day"
	peakLabel := "Peak"
	if isHourly {
		avgLabel = "Avg/hour"
		peakLabel = "Peak hour"
	}

	parts := []string{
		fmt.Sprintf("Total %s", renderDailyMetricValue(metric, total, true)),
		fmt.Sprintf("%s %s", avgLabel, renderDailyMetricValue(metric, total/float64(len(daily.Days)), true)),
	}

	if len(daily.Days) > 1 {
		latest := daily.Days[len(daily.Days)-1]
		previous := daily.Days[len(daily.Days)-2]
		latestLabel := renderDateLabel(latest.Date, isHourly)
		previousLabel := renderDateLabel(previous.Date, isHourly)
		parts = append(parts, fmt.Sprintf("Latest %s %s vs %s", latestLabel, renderDailyDelta(latest, previous, metric), previousLabel))
	}

	parts = append(parts, fmt.Sprintf("%s %s %s", peakLabel, renderDateLabel(peak.Date, isHourly), renderDailyMetricValue(metric, peakValue, true)))
	return strings.Join(parts, " • ")
}

func renderDailyFooter(daily stats.DailyStats, isHourly bool) string {
	latest := daily.Days[len(daily.Days)-1]
	latestLabel := renderDateLabel(latest.Date, isHourly)
	return fmt.Sprintf(
		"Latest %s • %s • %s sessions • %s messages • %s tokens",
		latestLabel,
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
