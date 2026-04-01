package tui

import (
	"fmt"

	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/stats"
)

func renderOverview(s styles, width, _ int, data dashboardData) string {
	if data.Overview.Sessions == 0 && data.Overview.Messages == 0 {
		return s.EmptyState.Render("No OpenCode activity found in the selected database yet.")
	}

	cardWidth := max((width-6)/4, 18)
	cards := []string{
		metricCard(s, "Sessions", formatInt(data.Overview.Sessions), fmt.Sprintf("%d active days", data.Overview.Days), cardWidth),
		metricCard(s, "Messages", formatInt(data.Overview.Messages), fmt.Sprintf("%s per session", formatInt(avgPerSession(data.Overview.Messages, data.Overview.Sessions))), cardWidth),
		metricCard(s, "Cost", formatMoney(data.Overview.Cost), fmt.Sprintf("%s / day", formatMoney(data.Overview.CostPerDay)), cardWidth),
		metricCard(s, "Tokens", formatInt(totalTokens(data.Overview)), formatTokens(data.Overview.Tokens), cardWidth),
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	if width < 96 {
		row = lipgloss.JoinVertical(lipgloss.Left, cards...)
	}

	topModel := "No assistant model data"
	if len(data.Models.Models) > 0 {
		topModel = fmt.Sprintf("%s (%s)", data.Models.Models[0].ModelID, formatMoney(data.Models.Models[0].Cost))
	}
	topProject := "No project data"
	if len(data.Projects.Projects) > 0 {
		topProject = fmt.Sprintf("%s (%s)", data.Projects.Projects[0].ProjectName, formatMoney(data.Projects.Projects[0].Cost))
	}
	topTool := "No tool data"
	if len(data.Tools.Tools) > 0 {
		topTool = fmt.Sprintf("%s (%s runs)", data.Tools.Tools[0].Name, formatInt(data.Tools.Tools[0].Invocations))
	}

	secondary := []string{
		s.PanelTitle.Render("Top signals"),
		fmt.Sprintf("Top model   %s", topModel),
		fmt.Sprintf("Top project %s", topProject),
		fmt.Sprintf("Top tool    %s", topTool),
		"",
		s.PanelTitle.Render("Token mix"),
		fmt.Sprintf("Input       %s", formatInt(data.Overview.Tokens.Input)),
		fmt.Sprintf("Output      %s", formatInt(data.Overview.Tokens.Output)),
		fmt.Sprintf("Reasoning   %s", formatInt(data.Overview.Tokens.Reasoning)),
		fmt.Sprintf("Cache R/W   %s / %s", formatInt(data.Overview.Tokens.Cache.Read), formatInt(data.Overview.Tokens.Cache.Write)),
	}

	recent := []string{s.PanelTitle.Render("Recent sessions")}
	for i, session := range data.Sessions.Sessions {
		if i >= 5 {
			break
		}
		recent = append(recent, fmt.Sprintf("%s  %s  %s", session.TimeCreated.Format("Jan 02 15:04"), truncateWithEllipsis(session.Title, max(width-34, 18)), formatMoney(session.Cost)))
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
