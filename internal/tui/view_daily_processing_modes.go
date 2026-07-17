package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"opencode-dashboard/internal/stats"
)

type processingModeTotal struct {
	Mode           stats.ProcessingMode
	Messages       int64
	Tokens         int64
	Cost           float64
	CostStatus     stats.CostStatus
	CostProvenance *stats.CostProvenance
}

var processingModeDisplayOrder = []stats.ProcessingMode{
	stats.ProcessingModeFast,
	stats.ProcessingModeStandard,
	stats.ProcessingModeFlex,
	stats.ProcessingModeUnknown,
}

func aggregateProcessingModeTotals(rows []stats.DimensionDayStats) []processingModeTotal {
	byMode := make(map[stats.ProcessingMode]*processingModeTotal, len(processingModeDisplayOrder))
	for _, mode := range processingModeDisplayOrder {
		byMode[mode] = &processingModeTotal{Mode: mode}
	}
	for _, row := range rows {
		mode := resolveRequestedProcessingMode(stats.ProcessingMode(row.Dimension), "")
		total := byMode[mode]
		total.Messages += row.Messages
		total.Tokens += totalTokenStats(row.Tokens)
		total.Cost += row.Cost
		total.CostStatus, total.CostProvenance = mergeProcessingModeCost(
			total.CostStatus,
			total.CostProvenance,
			row.CostStatus,
			row.CostProvenance,
		)
	}

	totals := make([]processingModeTotal, 0, len(processingModeDisplayOrder))
	for _, mode := range processingModeDisplayOrder {
		totals = append(totals, *byMode[mode])
	}
	return totals
}

func mergeProcessingModeCost(aStatus stats.CostStatus, aProv *stats.CostProvenance, bStatus stats.CostStatus, bProv *stats.CostProvenance) (stats.CostStatus, *stats.CostProvenance) {
	if bStatus == "" && bProv != nil {
		bStatus = bProv.Status
	}
	if aStatus == "" && aProv != nil {
		aStatus = aProv.Status
	}
	if bStatus == "" {
		return aStatus, aProv
	}
	if aStatus == "" {
		if bProv == nil {
			return bStatus, &stats.CostProvenance{Status: bStatus}
		}
		clone := *bProv
		clone.Status = bStatus
		return bStatus, &clone
	}
	status := aStatus
	if aStatus != bStatus {
		status = stats.CostMixed
	}
	provenance := &stats.CostProvenance{Status: status, Currency: "USD"}
	for _, p := range []*stats.CostProvenance{aProv, bProv} {
		if p == nil {
			continue
		}
		provenance.MissingCount += p.MissingCount
		provenance.ComputedCount += p.ComputedCount
		provenance.ReportedCount += p.ReportedCount
	}
	return status, provenance
}

func processingModeMetricValue(total processingModeTotal, metric dailyMetric) float64 {
	switch metric {
	case dailyMetricCost:
		return total.Cost
	case dailyMetricTokens:
		return float64(total.Tokens)
	default:
		return float64(total.Messages)
	}
}

func processingModeMetricLabel(metric dailyMetric) string {
	switch metric {
	case dailyMetricCost:
		return "API cost estimate (USD)"
	case dailyMetricTokens:
		return "tokens"
	default:
		return "assistant requests"
	}
}

func renderProcessingModeMetricValue(metric dailyMetric, value float64, status stats.CostStatus, provenance *stats.CostProvenance) string {
	if metric == dailyMetricCost {
		return plainCostProv(value, status, provenance)
	}
	return formatCompactInt(int64(math.Round(value)))
}

func renderProcessingModeLabel(s styles, mode stats.ProcessingMode) string {
	label := requestedProcessingModeLabel(mode, "")
	switch mode {
	case stats.ProcessingModeFast:
		return s.Accent.Render(label)
	case stats.ProcessingModeStandard:
		return s.Success.Render(label)
	case stats.ProcessingModeFlex:
		return s.Info.Render(label)
	default:
		return s.Muted.Render(label)
	}
}

func renderDailyProcessingModes(
	s styles,
	width, height int,
	dimension stats.DailyDimensionStats,
	dimensionErr error,
	period string,
	metric dailyMetric,
	loading bool,
) string {
	if loading {
		return s.EmptyState.Render("Loading requested-mode data...")
	}
	if dimensionErr != nil {
		return joinLines(
			s.PanelTitle.Render(fmt.Sprintf("Daily activity • %s • requested mode", period)),
			"",
			s.Danger.Render("Requested-mode breakdown unavailable"),
			s.Muted.Render(truncateWithEllipsis(dimensionErr.Error(), max(width-4, 20))),
			"",
			s.Muted.Render("d returns to overall daily activity"),
		)
	}
	if len(dimension.Days) == 0 {
		return joinLines(
			s.PanelTitle.Render(fmt.Sprintf("Daily activity • %s • requested mode", period)),
			"",
			s.EmptyState.Render("No requested-tier telemetry is available for this Codex window."),
			"",
			s.Muted.Render("d returns to overall daily activity"),
		)
	}
	if metric != dailyMetricCost && metric != dailyMetricTokens {
		metric = dailyMetricMessages
	}

	totals := aggregateProcessingModeTotals(dimension.Days)
	maxValue := 0.0
	allMessages := int64(0)
	allTokens := int64(0)
	allCost := 0.0
	var allCostStatus stats.CostStatus
	var allCostProvenance *stats.CostProvenance
	for _, total := range totals {
		value := processingModeMetricValue(total, metric)
		if value > maxValue {
			maxValue = value
		}
		allMessages += total.Messages
		allTokens += total.Tokens
		allCost += total.Cost
		allCostStatus, allCostProvenance = mergeProcessingModeCost(allCostStatus, allCostProvenance, total.CostStatus, total.CostProvenance)
	}

	lines := []string{
		s.PanelTitle.Render(fmt.Sprintf("Daily activity • %s • %s by requested mode", period, processingModeMetricLabel(metric))),
		s.Muted.Render("Requested locally; served tier is not recorded or server-confirmed"),
		s.Muted.Render("Fast uses Priority API rates • Flex uses Flex API rates • Standard uses Standard API rates"),
		s.Muted.Render("Tier unknown stays unknown and falls back to Standard API rates • not actual billed spend"),
		s.Muted.Render(fmt.Sprintf("API cost estimate %s • Assistant requests %s • Tokens %s", plainCostProv(allCost, allCostStatus, allCostProvenance), formatInt(allMessages), formatCompactInt(allTokens))),
		"",
		s.Text.Render("Requested processing mode totals"),
	}

	barWidth := max(min(width-39, 28), 8)
	for _, total := range totals {
		value := processingModeMetricValue(total, metric)
		bar := asciiBar(value, maxValue, barWidth)
		if bar == "" {
			bar = strings.Repeat("·", min(3, barWidth))
		}
		lines = append(lines, fmt.Sprintf(
			"  %s %s %s",
			padRight(renderProcessingModeLabel(s, total.Mode), 20),
			padRight(s.Accent.Render(bar), barWidth),
			padLeft(renderProcessingModeMetricValue(metric, value, total.CostStatus, total.CostProvenance), 12),
		))
	}

	if width >= 68 && height >= 18 {
		lines = append(lines, "", s.Text.Render("Daily table"))
		lines = append(lines, s.TableHeader.Render(fmt.Sprintf(
			"  %-10s %12s %12s %12s %12s",
			"DATE", "FAST", "STANDARD", "FLEX", "UNKNOWN",
		)))

		type dailyModeCell struct {
			value      float64
			costStatus stats.CostStatus
			costProv   *stats.CostProvenance
		}
		byDate := make(map[string]map[stats.ProcessingMode]dailyModeCell)
		for _, row := range dimension.Days {
			mode := resolveRequestedProcessingMode(stats.ProcessingMode(row.Dimension), "")
			values := byDate[row.Date]
			if values == nil {
				values = make(map[stats.ProcessingMode]dailyModeCell, len(processingModeDisplayOrder))
				byDate[row.Date] = values
			}
			cell := values[mode]
			switch metric {
			case dailyMetricCost:
				cell.value += row.Cost
				cell.costStatus, cell.costProv = mergeProcessingModeCost(cell.costStatus, cell.costProv, row.CostStatus, row.CostProvenance)
			case dailyMetricTokens:
				cell.value += float64(totalTokenStats(row.Tokens))
			default:
				cell.value += float64(row.Messages)
			}
			values[mode] = cell
		}
		dates := make([]string, 0, len(byDate))
		for date := range byDate {
			dates = append(dates, date)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dates)))
		maxRows := max(height-len(lines)-2, 1)
		if len(dates) > maxRows {
			dates = dates[:maxRows]
		}
		for _, date := range dates {
			values := byDate[date]
			fast := values[stats.ProcessingModeFast]
			standard := values[stats.ProcessingModeStandard]
			flex := values[stats.ProcessingModeFlex]
			unknown := values[stats.ProcessingModeUnknown]
			lines = append(lines, s.TableRow.Render(fmt.Sprintf(
				"  %-10s %12s %12s %12s %12s",
				renderDateLabel(date, false),
				renderProcessingModeMetricValue(metric, fast.value, fast.costStatus, fast.costProv),
				renderProcessingModeMetricValue(metric, standard.value, standard.costStatus, standard.costProv),
				renderProcessingModeMetricValue(metric, flex.value, flex.costStatus, flex.costProv),
				renderProcessingModeMetricValue(metric, unknown.value, unknown.costStatus, unknown.costProv),
			)))
		}
	}

	lines = append(lines, "", s.Muted.Render("t switches cost/messages/tokens • d returns to overall"))
	return joinLines(lines...)
}
