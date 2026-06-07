package tui

import (
	"fmt"
	"sort"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

// renderOverview renders the high-density, ALL-SOURCES dashboard. Unlike every
// other tab (which is scoped to the selected source), the Overview merges data
// across all available sources. Cost is shown only per source — never as a single
// combined number, since sources mix real spend with estimated API-equivalents.
func renderOverview(s styles, width, height int, data dashboardData) string {
	ao := data.AllOverview
	ov := ao.Total

	// Per-source cost statuses drive the provenance legend (no combined status).
	var statuses []stats.CostStatus
	activeSources := 0
	for _, src := range ao.Sources {
		statuses = append(statuses, resolveCostStatus(src.Overview.CostStatus, src.Overview.CostProvenance))
		if src.Overview.Sessions > 0 || src.Overview.Messages > 0 {
			activeSources++
		}
	}

	// --- Block 1: combined KPI cards (no combined cost) ---
	cardWidth := max((width-6)/4, 18)
	cards := []string{
		metricCard(s, "Sessions", formatInt(ov.Sessions), fmt.Sprintf("%d sources • %d days", len(ao.Sources), ov.Days), cardWidth),
		metricCard(s, "Messages", formatInt(ov.Messages), fmt.Sprintf("%.1f / session", ao.MessagesPerSession), cardWidth),
		metricCard(s, "Tokens", formatInt(totalTokens(ov)), formatTokens(ov.Tokens), cardWidth),
		metricCard(s, "Sources active", fmt.Sprintf("%d / %d", activeSources, len(ao.Sources)), "with activity in range", cardWidth),
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	if width < 96 {
		row = lipgloss.JoinVertical(lipgloss.Left, cards...)
	}

	// --- Block 2: per-source failure notice ---
	var errLine string
	if len(ao.Errors) > 0 {
		ids := make([]string, 0, len(ao.Errors))
		for _, e := range ao.Errors {
			ids = append(ids, e.SourceID)
		}
		errLine = s.Danger.Render("⚠ sources unavailable: " + strings.Join(ids, ", "))
	}

	// --- Block 3: usage by source ---
	tableSection := s.Panel.Width(max(width-2, 30)).Render(joinLines(renderSourceUsage(s, width, ao)...))

	// --- Block 4 + 5: token distribution + efficiency (left panel) ---
	leftLines := append(renderTokenDistribution(s, ao.TokenDistribution), "")
	leftLines = append(leftLines, renderEfficiency(s, ao)...)

	// --- Block 6: top signals across sources (right panel) ---
	rightLines := renderTopSignals(s, width, ao)

	var midSection string
	if width >= 96 {
		panelW := max(width/2-2, 30)
		midSection = lipgloss.JoinHorizontal(lipgloss.Top,
			s.Panel.Width(panelW).Render(joinLines(leftLines...)),
			s.Panel.Width(panelW).Render(joinLines(rightLines...)),
		)
	} else {
		midSection = joinLines(
			s.Panel.Width(max(width-2, 30)).Render(joinLines(leftLines...)),
			s.Panel.Width(max(width-2, 30)).Render(joinLines(rightLines...)),
		)
	}

	// --- Block 7: combined activity trend (optional) ---
	var trendSection string
	if width >= 96 && height >= 24 {
		if days := combineTrend(ao.Sources); len(days) > 0 {
			trendSection = s.Panel.Width(max(width-2, 30)).Render(joinLines(renderOverviewTrend(s, days)...))
		}
	}

	sections := []string{row, ""}
	if errLine != "" {
		sections = append(sections, errLine, "")
	}
	sections = append(sections, tableSection, "", midSection)
	if trendSection != "" {
		sections = append(sections, "", trendSection)
	}
	if legend := renderProvenanceLegend(s, width, statuses...); legend != "" {
		sections = append(sections, "", legend)
	}
	return joinLines(sections...)
}

// renderSourceUsage builds the "Usage by source" table: per-source sessions,
// messages, tokens, that source's own cost, and its token share with a bar.
func renderSourceUsage(s styles, width int, ao source.AllSourcesOverview) []string {
	lines := []string{s.PanelTitle.Render("Usage by source")}
	if len(ao.Sources) == 0 {
		return append(lines, s.Muted.Render("No sources available"))
	}

	showExtra := width >= 90
	const barW = 14
	buildRow := func(label, sess, msgs, toks, cost, share, bar string) string {
		cols := []string{padRight(label, 14)}
		if showExtra {
			cols = append(cols, padLeft(sess, 6))
		}
		cols = append(cols, padLeft(msgs, 8))
		if showExtra {
			cols = append(cols, padLeft(toks, 9))
		}
		cols = append(cols, padLeft(cost, 11), padLeft(share, 6))
		if showExtra {
			cols = append(cols, bar)
		}
		return strings.Join(cols, " ")
	}

	lines = append(lines, s.TableHeader.Render(buildRow("SOURCE", "SESS", "MSGS", "TOKENS", "COST", "SHARE", padRight("TOKEN SHARE", barW))))
	for _, src := range ao.Sources {
		rowText := buildRow(
			truncateWithEllipsis(src.Label, 14),
			formatCompactInt(src.Overview.Sessions),
			formatCompactInt(src.Overview.Messages),
			formatCompactInt(totalTokens(src.Overview)),
			plainCostProv(src.Overview.Cost, src.Overview.CostStatus, src.Overview.CostProvenance),
			fmt.Sprintf("%.0f%%", src.TokenShare*100),
			padRight(s.Accent.Render(asciiBar(src.TokenShare, 1.0, barW)), barW),
		)
		lines = append(lines, s.TableRow.Render(rowText))
	}
	return lines
}

// renderTokenDistribution renders the combined token mix as labelled progress bars.
func renderTokenDistribution(s styles, td stats.TokenStats) []string {
	tot := td.Input + td.Output + td.Reasoning + td.Cache.Read + td.Cache.Write
	maxv := float64(max(tot, 1))
	const barWidth = 16
	return []string{
		s.PanelTitle.Render("Token distribution"),
		fmt.Sprintf("Input       %s %s", progressBarWithPercent(s, float64(td.Input), maxv, barWidth), formatInt(td.Input)),
		fmt.Sprintf("Output      %s %s", progressBarWithPercent(s, float64(td.Output), maxv, barWidth), formatInt(td.Output)),
		fmt.Sprintf("Reasoning   %s %s", progressBarWithPercent(s, float64(td.Reasoning), maxv, barWidth), formatInt(td.Reasoning)),
		fmt.Sprintf("Cache Read  %s %s", progressBarWithPercent(s, float64(td.Cache.Read), maxv, barWidth), formatInt(td.Cache.Read)),
		fmt.Sprintf("Cache Write %s %s", progressBarWithPercent(s, float64(td.Cache.Write), maxv, barWidth), formatInt(td.Cache.Write)),
		s.Muted.Render(fmt.Sprintf("Total       %s", formatInt(tot))),
	}
}

// renderEfficiency renders cost-neutral combined throughput ratios.
func renderEfficiency(s styles, ao source.AllSourcesOverview) []string {
	tpm := ao.TokensPerMessage
	return []string{
		s.PanelTitle.Render("Efficiency (combined)"),
		fmt.Sprintf("%s %.1f", padRight("Msgs / session", 18), ao.MessagesPerSession),
		fmt.Sprintf("%s %s", padRight("Tokens / message", 18), formatCompactInt(int64(avgTokenTotal(&tpm)))),
		s.Muted.Render(fmt.Sprintf("in %.0f / out %.0f / reason %.0f per msg", tpm.Input, tpm.Output, tpm.Reasoning)),
	}
}

// renderTopSignals renders the top models / projects / tools merged across
// sources, each row prefixed with its source.
func renderTopSignals(s styles, width int, ao source.AllSourcesOverview) []string {
	lines := []string{s.PanelTitle.Render("Top signals across sources")}
	if len(ao.TopModels) == 0 && len(ao.TopProjects) == 0 && len(ao.TopTools) == 0 {
		return append(lines, s.Muted.Render("No model/project/tool data"))
	}
	nameW := max(width/5, 12)

	if len(ao.TopModels) > 0 {
		lines = append(lines, s.Muted.Render("Models"))
		for i, m := range ao.TopModels {
			if i >= 3 {
				break
			}
			lines = append(lines, fmt.Sprintf("%s %s %s",
				padRight(m.SourceID, 11),
				padRight(truncateWithEllipsis(m.ModelID, nameW), nameW),
				plainCostProv(m.Cost, m.CostStatus, m.CostProvenance)))
		}
	}
	if len(ao.TopProjects) > 0 {
		lines = append(lines, s.Muted.Render("Projects"))
		for i, p := range ao.TopProjects {
			if i >= 3 {
				break
			}
			lines = append(lines, fmt.Sprintf("%s %s %s",
				padRight(p.SourceID, 11),
				padRight(truncateWithEllipsis(p.ProjectName, nameW), nameW),
				plainCostProv(p.Cost, p.CostStatus, p.CostProvenance)))
		}
	}
	if len(ao.TopTools) > 0 {
		lines = append(lines, s.Muted.Render("Tools"))
		for i, t := range ao.TopTools {
			if i >= 3 {
				break
			}
			lines = append(lines, fmt.Sprintf("%s %s %s runs",
				padRight(t.SourceID, 11),
				padRight(truncateWithEllipsis(t.Name, nameW), nameW),
				formatInt(t.Invocations)))
		}
	}
	return lines
}

// renderOverviewTrend renders a combined per-day message-activity sparkline. It
// uses messages (an additive, cost-neutral metric) so no cross-source cost is mixed.
func renderOverviewTrend(s styles, days []stats.DayStats) []string {
	lines := []string{s.PanelTitle.Render("Activity trend (messages/day)")}
	var maxMsgs int64 = 1
	for _, d := range days {
		if d.Messages > maxMsgs {
			maxMsgs = d.Messages
		}
	}
	// Show the most recent slice if there are many days.
	start := max(len(days)-14, 0)
	for _, d := range days[start:] {
		bar := s.Accent.Render(asciiBar(float64(d.Messages), float64(maxMsgs), 24))
		lines = append(lines, fmt.Sprintf("%s %s %s", padRight(renderDateLabel(d.Date, false), 8), padRight(bar, 24), padLeft(formatCompactInt(d.Messages), 7)))
	}
	return lines
}

// combineTrend sums per-source daily trends by date into a single ascending series.
func combineTrend(sources []source.SourceOverview) []stats.DayStats {
	byDate := make(map[string]stats.DayStats)
	for _, src := range sources {
		for _, d := range src.Trend {
			agg := byDate[d.Date]
			agg.Date = d.Date
			agg.Sessions += d.Sessions
			agg.Messages += d.Messages
			agg.Cost += d.Cost
			agg.Tokens.Input += d.Tokens.Input
			agg.Tokens.Output += d.Tokens.Output
			agg.Tokens.Reasoning += d.Tokens.Reasoning
			agg.Tokens.Cache.Read += d.Tokens.Cache.Read
			agg.Tokens.Cache.Write += d.Tokens.Cache.Write
			byDate[d.Date] = agg
		}
	}
	dates := make([]string, 0, len(byDate))
	for dt := range byDate {
		dates = append(dates, dt)
	}
	sort.Strings(dates)
	out := make([]stats.DayStats, 0, len(dates))
	for _, dt := range dates {
		out = append(out, byDate[dt])
	}
	return out
}

func totalTokens(overviewData stats.OverviewStats) int64 {
	return overviewData.Tokens.Input + overviewData.Tokens.Output + overviewData.Tokens.Reasoning + overviewData.Tokens.Cache.Read + overviewData.Tokens.Cache.Write
}
