package tui

import (
	"fmt"

	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/stats"
)

func renderOverview(s styles, width, _ int, data dashboardData) string {
	// Per spec: always render zero-valued metrics and empty bars, never generic empty state
	// Efficiency metrics for overview
	tokensPerSession := int64(0)
	if data.Overview.Sessions > 0 {
		tokensPerSession = totalTokens(data.Overview) / data.Overview.Sessions
	}
	costPerMessage := 0.0
	if data.Overview.Messages > 0 {
		costPerMessage = data.Overview.Cost / float64(data.Overview.Messages)
	}

	ov := data.Overview
	costStatus := resolveCostStatus(ov.CostStatus, ov.CostProvenance)
	cardWidth := max((width-6)/4, 18)
	cards := []string{
		metricCard(s, "Sessions", formatInt(ov.Sessions), fmt.Sprintf("%d active days", ov.Days), cardWidth),
		metricCard(s, "Messages", formatInt(ov.Messages), fmt.Sprintf("%s / sess • %s/msg", formatInt(avgPerSession(ov.Messages, ov.Sessions)), formatMoneyProv(s, costPerMessage, ov.CostStatus, ov.CostProvenance, true)), cardWidth),
		metricCard(s, "Cost", formatMoneyProv(s, ov.Cost, ov.CostStatus, ov.CostProvenance, false), fmt.Sprintf("%s / day • %s tok/sess", formatMoneyProv(s, ov.CostPerDay, ov.CostStatus, ov.CostProvenance, true), formatCompactInt(tokensPerSession)), cardWidth),
		metricCard(s, "Tokens", formatInt(totalTokens(ov)), formatTokens(ov.Tokens), cardWidth),
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	if width < 96 {
		row = lipgloss.JoinVertical(lipgloss.Left, cards...)
	}

	topModel := "No assistant model data"
	if len(data.Models.Models) > 0 {
		mdl := data.Models.Models[0]
		topModel = fmt.Sprintf("%s (%s)", mdl.ModelID, formatMoneyProv(s, mdl.Cost, mdl.CostStatus, mdl.CostProvenance, false))
	}
	topProject := "No project data"
	if len(data.Projects.Projects) > 0 {
		prj := data.Projects.Projects[0]
		topProject = fmt.Sprintf("%s (%s)", prj.ProjectName, formatMoneyProv(s, prj.Cost, prj.CostStatus, prj.CostProvenance, false))
	}
	topTool := "No tool data"
	if len(data.Tools.Tools) > 0 {
		topTool = fmt.Sprintf("%s (%s runs)", data.Tools.Tools[0].Name, formatInt(data.Tools.Tools[0].Invocations))
	}

	// Token breakdown with progress bars - per spec: ALL categories including cache read/write
	totalTok := totalTokens(data.Overview)
	secondary := []string{
		s.PanelTitle.Render("Top signals"),
		fmt.Sprintf("%s %s", padRight("Top model", 12), topModel),
		fmt.Sprintf("%s %s", padRight("Top project", 12), topProject),
		fmt.Sprintf("%s %s", padRight("Top tool", 12), topTool),
		"",
		s.PanelTitle.Render("Token breakdown"),
	}

	barWidth := 16
	// Per spec: each token category must show count + percentage + progress bar
	secondary = append(secondary,
		fmt.Sprintf("Input      %s %s", progressBarWithPercent(s, float64(data.Overview.Tokens.Input), float64(max(totalTok, 1)), barWidth), formatInt(data.Overview.Tokens.Input)),
		fmt.Sprintf("Output     %s %s", progressBarWithPercent(s, float64(data.Overview.Tokens.Output), float64(max(totalTok, 1)), barWidth), formatInt(data.Overview.Tokens.Output)),
		fmt.Sprintf("Reasoning  %s %s", progressBarWithPercent(s, float64(data.Overview.Tokens.Reasoning), float64(max(totalTok, 1)), barWidth), formatInt(data.Overview.Tokens.Reasoning)),
		fmt.Sprintf("Cache Read %s %s", progressBarWithPercent(s, float64(data.Overview.Tokens.Cache.Read), float64(max(totalTok, 1)), barWidth), formatInt(data.Overview.Tokens.Cache.Read)),
		fmt.Sprintf("Cache Write %s %s", progressBarWithPercent(s, float64(data.Overview.Tokens.Cache.Write), float64(max(totalTok, 1)), barWidth), formatInt(data.Overview.Tokens.Cache.Write)),
		s.Muted.Render(fmt.Sprintf("Total       %s", formatInt(totalTok))),
	)
	if legend := renderProvenanceLegend(s, width, costStatus); legend != "" {
		secondary = append(secondary, "", legend)
	}

	recent := []string{s.PanelTitle.Render("Recent sessions")}
	for i, session := range data.Sessions.Sessions {
		if i >= 5 {
			break
		}
		recent = append(recent, fmt.Sprintf("%s  %s  %s", session.TimeCreated.Format("Jan 02 15:04"), truncateWithEllipsis(session.Title, max(width-34, 18)), formatMoneyProv(s, session.Cost, session.CostStatus, session.CostProvenance, true)))
	}
	if len(data.Sessions.Sessions) == 0 {
		recent = append(recent, s.Muted.Render("No sessions on current page"))
	}

	return joinLines(
		row,
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			s.Panel.Width(max(width/2-2, 30)).Render(joinLines(secondary...)),
			s.Panel.Width(max(width/2-2, 30)).Render(joinLines(recent...)),
		),
	)
}

func totalTokens(overviewData stats.OverviewStats) int64 {
	return overviewData.Tokens.Input + overviewData.Tokens.Output + overviewData.Tokens.Reasoning + overviewData.Tokens.Cache.Read + overviewData.Tokens.Cache.Write
}

func avgPerSession(messages, sessions int64) int64 {
	if sessions <= 0 {
		return 0
	}
	return messages / sessions
}
