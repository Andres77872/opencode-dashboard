package tui

import (
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
)

type sessionsViewState struct {
	cursor      int
	loading     bool
	filter      string
	filterDraft string
	filterMode  bool
	sort        stats.SessionSortMode
}

type tableViewState struct {
	cursor      int
	loading     bool
	filter      string
	filterDraft string
	filterMode  bool
	sortLabel   string
}

func renderModels(s styles, width, height int, items []stats.ModelEntry, total int, state tableViewState) string {
	rows := []string{
		s.PanelTitle.Render("Models"),
		s.Muted.Render("Top usage with leader summary, cost share, and avg/msg."),
	}

	// Leader section (top 3 by cost) - reusable helper
	if len(items) >= 2 {
		totalCost := 0.0
		for _, item := range items {
			totalCost += item.Cost
		}
		leaders := make([]LeaderEntry, min(3, len(items)))
		for i := 0; i < len(leaders); i++ {
			leaders[i] = LeaderEntry{Name: items[i].ModelID, Value: items[i].Cost}
		}
		leaderSection := renderLeaderSection(s, width, leaders, totalCost, "#%d", formatMoney)
		if leaderSection != "" {
			rows = append(rows, "", leaderSection)
		}
	}

	rows = append(rows, "")

	// Width thresholds for progressive column drop
	showAvgPerMsg := width >= 95
	showCostShare := width >= 110

	// Calculate dynamic widths
	nameWidth := max(width-42, 16)
	if showAvgPerMsg && !showCostShare {
		nameWidth = max(width-50, 16)
	} else if showAvgPerMsg && showCostShare {
		nameWidth = max(width-74, 16)
	} else if showCostShare {
		nameWidth = max(width-62, 16)
	}

	// Build header based on available columns
	headerParts := []string{padRight("MODEL", nameWidth), padRight("PROVIDER", 12), padLeft("SESS", 6), padLeft("MSG", 6)}
	if showAvgPerMsg {
		headerParts = append(headerParts, padLeft("AVG$", 8))
	}
	headerParts = append(headerParts, padLeft("COST", 10))
	if showCostShare {
		headerParts = append(headerParts, padLeft("SHARE", 12))
	}
	rows = append(rows, s.TableHeader.Render(strings.Join(headerParts, " ")))

	limit := min(len(items), max(height-len(rows)-4, 5))
	if len(items) == 0 {
		message := "No assistant model usage found."
		if state.filter != "" {
			message = "No models match the current filter."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		totalCost := 0.0
		for _, item := range items {
			totalCost += item.Cost
		}

		start, end := tableWindow(len(items), state.cursor, limit)
		for i := start; i < end; i++ {
			item := items[i]
			avgPerMsg := 0.0
			if item.Messages > 0 {
				avgPerMsg = item.Cost / float64(item.Messages)
			}

			lineParts := []string{
				padRight(truncateWithEllipsis(item.ModelID, nameWidth), nameWidth),
				padRight(truncateWithEllipsis(item.ProviderID, 12), 12),
				padLeft(formatInt(item.Sessions), 6),
				padLeft(formatInt(item.Messages), 6),
			}
			if showAvgPerMsg {
				lineParts = append(lineParts, padLeft(formatMoney(avgPerMsg), 8))
			}
			lineParts = append(lineParts, padLeft(formatMoney(item.Cost), 10))
			if showCostShare {
				shareBar := progressBarWithPercent(s, item.Cost, totalCost, 12)
				lineParts = append(lineParts, padLeft(shareBar, 12))
			}

			line := strings.Join(lineParts, " ")
			if i == state.cursor {
				rows = append(rows, s.TableRowActive.Render("> "+line))
				continue
			}
			rows = append(rows, s.TableRow.Render("  "+line))
		}
	}
	rows = appendTableStatus(rows, s, state, len(items), total, "models")
	return strings.TrimRight(joinLines(rows...), "\n")
}

func renderTools(s styles, width, height int, items []stats.ToolEntry, total int, state tableViewState) string {
	rows := []string{
		s.PanelTitle.Render("Tools"),
		s.Muted.Render("Top tool usage with leader summary, success rate, and status."),
	}

	// Leader section (top 2 by invocations) - reusable helper
	if len(items) >= 2 {
		totalInvocations := int64(0)
		for _, item := range items {
			totalInvocations += item.Invocations
		}
		leaders := make([]LeaderEntry, min(2, len(items)))
		for i := 0; i < len(leaders); i++ {
			leaders[i] = LeaderEntry{Name: items[i].Name, Value: float64(items[i].Invocations)}
		}
		leaderSection := renderLeaderSection(s, width, leaders, float64(totalInvocations), "#%d", func(v float64) string { return formatInt(int64(v)) })
		if leaderSection != "" {
			rows = append(rows, "", leaderSection)
		}
	}

	rows = append(rows, "")

	// Width thresholds for progressive column drop
	showSuccessRate := width >= 100
	showShare := width >= 120
	showStatus := width >= 130

	// Calculate dynamic widths
	nameWidth := max(width-34, 16)
	if showSuccessRate && !showShare && !showStatus {
		nameWidth = max(width-42, 16)
	} else if showSuccessRate && showShare && !showStatus {
		nameWidth = max(width-64, 16)
	} else if showSuccessRate && showShare && showStatus {
		nameWidth = max(width-72, 16)
	} else if showShare {
		nameWidth = max(width-52, 16)
	}

	// Build header based on available columns
	headerParts := []string{padRight("TOOL", nameWidth), padLeft("RUNS", 7), padLeft("OK", 7), padLeft("ERR", 7)}
	if showSuccessRate {
		headerParts = append(headerParts, padLeft("RATE", 8))
	}
	headerParts = append(headerParts, padLeft("SESS", 7))
	if showShare {
		headerParts = append(headerParts, padLeft("SHARE", 12))
	}
	if showStatus {
		headerParts = append(headerParts, padLeft("STATUS", 8))
	}
	rows = append(rows, s.TableHeader.Render(strings.Join(headerParts, " ")))

	limit := min(len(items), max(height-len(rows)-4, 5))
	if len(items) == 0 {
		message := "No tool invocation data found."
		if state.filter != "" {
			message = "No tools match the current filter."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		totalInvocations := int64(0)
		for _, item := range items {
			totalInvocations += item.Invocations
		}

		start, end := tableWindow(len(items), state.cursor, limit)
		for i := start; i < end; i++ {
			item := items[i]
			successRate := 0.0
			if item.Invocations > 0 {
				successRate = (float64(item.Successes) / float64(item.Invocations)) * 100
			}

			lineParts := []string{
				padRight(truncateWithEllipsis(item.Name, nameWidth), nameWidth),
				padLeft(formatInt(item.Invocations), 7),
				padLeft(formatInt(item.Successes), 7),
				padLeft(formatInt(item.Failures), 7),
			}
			if showSuccessRate {
				rateText := "--"
				if item.Invocations > 0 {
					rateText = fmt.Sprintf("%.1f%%", successRate)
				}
				lineParts = append(lineParts, padLeft(rateText, 8))
			}
			lineParts = append(lineParts, padLeft(formatInt(item.Sessions), 7))
			if showShare {
				shareBar := progressBarWithPercent(s, float64(item.Invocations), float64(totalInvocations), 12)
				lineParts = append(lineParts, padLeft(shareBar, 12))
			}
			if showStatus {
				// Pass -1 for no-data case (invocations=0) per spec
				badgeRate := successRate
				if item.Invocations == 0 {
					badgeRate = -1
				}
				statusBadge := renderStatusBadge(s, badgeRate)
				lineParts = append(lineParts, padLeft(statusBadge, 8))
			}

			line := strings.Join(lineParts, " ")
			if i == state.cursor {
				rows = append(rows, s.TableRowActive.Render("> "+line))
				continue
			}
			rows = append(rows, s.TableRow.Render("  "+line))
		}
	}
	rows = appendTableStatus(rows, s, state, len(items), total, "tools")
	return strings.TrimRight(joinLines(rows...), "\n")
}

func renderProjects(s styles, width, height int, items []stats.ProjectEntry, total int, state tableViewState) string {
	rows := []string{
		s.PanelTitle.Render("Projects"),
		s.Muted.Render("Project concentration with leader summary, tokens, and share."),
	}

	// Leader section (top 3 by cost) - reusable helper
	if len(items) >= 2 {
		totalCost := 0.0
		for _, item := range items {
			totalCost += item.Cost
		}
		leaders := make([]LeaderEntry, min(3, len(items)))
		for i := 0; i < len(leaders); i++ {
			leaders[i] = LeaderEntry{Name: items[i].ProjectName, Value: items[i].Cost}
		}
		leaderSection := renderLeaderSection(s, width, leaders, totalCost, "#%d", formatMoney)
		if leaderSection != "" {
			rows = append(rows, "", leaderSection)
		}
	}

	rows = append(rows, "")

	// Width thresholds for progressive column drop
	showTokens := width >= 100
	showShare := width >= 120
	showAvgSession := width >= 130

	// Calculate dynamic widths
	nameWidth := max(width-31, 16)
	if showTokens && !showShare && !showAvgSession {
		nameWidth = max(width-42, 16)
	} else if showTokens && showShare && !showAvgSession {
		nameWidth = max(width-64, 16)
	} else if showTokens && showShare && showAvgSession {
		nameWidth = max(width-72, 16)
	} else if showShare {
		nameWidth = max(width-53, 16)
	}

	// Build header based on available columns
	headerParts := []string{padRight("PROJECT", nameWidth), padLeft("SESS", 7), padLeft("MSG", 7)}
	if showTokens {
		headerParts = append(headerParts, padLeft("TOK", 7))
	}
	headerParts = append(headerParts, padLeft("COST", 10))
	if showShare {
		headerParts = append(headerParts, padLeft("SHARE", 12))
	}
	if showAvgSession {
		headerParts = append(headerParts, padLeft("AVG/S", 8))
	}
	rows = append(rows, s.TableHeader.Render(strings.Join(headerParts, " ")))

	limit := min(len(items), max(height-len(rows)-4, 5))
	if len(items) == 0 {
		message := "No project activity found."
		if state.filter != "" {
			message = "No projects match the current filter."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		totalCost := 0.0
		for _, item := range items {
			totalCost += item.Cost
		}

		start, end := tableWindow(len(items), state.cursor, limit)
		for i := start; i < end; i++ {
			item := items[i]
			totalTokens := item.Tokens.Input + item.Tokens.Output + item.Tokens.Reasoning + item.Tokens.Cache.Read + item.Tokens.Cache.Write
			avgPerSession := 0.0
			if item.Sessions > 0 {
				avgPerSession = item.Cost / float64(item.Sessions)
			}

			lineParts := []string{
				padRight(truncateWithEllipsis(item.ProjectName, nameWidth), nameWidth),
				padLeft(formatInt(item.Sessions), 7),
				padLeft(formatInt(item.Messages), 7),
			}
			if showTokens {
				lineParts = append(lineParts, padLeft(formatCompactInt(totalTokens), 7))
			}
			lineParts = append(lineParts, padLeft(formatMoney(item.Cost), 10))
			if showShare {
				shareBar := progressBarWithPercent(s, item.Cost, totalCost, 12)
				lineParts = append(lineParts, padLeft(shareBar, 12))
			}
			if showAvgSession {
				lineParts = append(lineParts, padLeft(formatMoney(avgPerSession), 8))
			}

			line := strings.Join(lineParts, " ")
			if i == state.cursor {
				rows = append(rows, s.TableRowActive.Render("> "+line))
				continue
			}
			rows = append(rows, s.TableRow.Render("  "+line))
		}
	}
	rows = appendTableStatus(rows, s, state, len(items), total, "projects")
	return strings.TrimRight(joinLines(rows...), "\n")
}

func renderSessions(s styles, width, height int, list stats.SessionList, state sessionsViewState) string {
	rows := []string{
		s.PanelTitle.Render("Sessions"),
		s.Muted.Render("Dense browse flow with filter, sort, cost share, and Enter drill-down."),
		"",
	}

	// Width threshold for share column
	showCostShare := width >= 110

	// Calculate dynamic widths
	titleWidth := max(width-45, 16)
	if showCostShare {
		titleWidth = max(width-67, 16)
	}

	// Build header based on available columns
	headerParts := []string{padRight("TITLE", titleWidth), padRight("PROJECT", 12), padRight("UPDATED", 12), padLeft("MSG", 5), padLeft("COST", 10)}
	if showCostShare {
		headerParts = append(headerParts, padLeft("SHARE", 12))
	}
	rows = append(rows, s.TableHeader.Render(strings.Join(headerParts, " ")))

	if len(list.Sessions) == 0 {
		message := "No sessions match the current view."
		if state.filter == "" {
			message = "No sessions on this page."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		limit := min(len(list.Sessions), max(height-6, 5))

		// Calculate page total for share percentage
		pageTotalCost := 0.0
		for _, item := range list.Sessions[:limit] {
			pageTotalCost += item.Cost
		}

		for i, item := range list.Sessions[:limit] {
			lineParts := []string{
				padRight(truncateWithEllipsis(item.Title, titleWidth), titleWidth),
				padRight(truncateWithEllipsis(item.ProjectName, 12), 12),
				padRight(item.TimeUpdated.Format("2006-01-02"), 12),
				padLeft(formatInt(item.MessageCount), 5),
				padLeft(formatMoney(item.Cost), 10),
			}
			if showCostShare {
				shareBar := progressBarWithPercent(s, item.Cost, pageTotalCost, 12)
				lineParts = append(lineParts, padLeft(shareBar, 12))
			}

			line := strings.Join(lineParts, " ")
			if i == state.cursor {
				rows = append(rows, s.TableRowActive.Render("> "+line))
				continue
			}
			rows = append(rows, s.TableRow.Render("  "+line))
		}
	}

	status := fmt.Sprintf("Page %d/%d • showing %d of %d sessions • sort:%s", max(list.Page, 1), totalSessionPages(list), len(list.Sessions), list.Total, renderSessionSortLabel(state.sort))
	if state.filter != "" {
		status += " • filter:" + state.filter
	}
	if state.loading {
		status += " • refreshing"
	}
	if state.filterMode {
		rows = append(rows, "", s.FilterPrompt.Render("/ "+state.filterDraft+"_"))
	}
	rows = append(rows, "", s.Muted.Render(status))
	return strings.TrimRight(joinLines(rows...), "\n")
}

func appendTableStatus(rows []string, s styles, state tableViewState, visible, total int, noun string) []string {
	status := fmt.Sprintf("showing %d of %d %s • sort:%s", visible, total, noun, state.sortLabel)
	if state.filter != "" {
		status += " • filter:" + state.filter
	}
	if state.loading {
		status += " • refreshing"
	}
	if state.filterMode {
		rows = append(rows, "", s.FilterPrompt.Render("/ "+state.filterDraft+"_"))
	}
	rows = append(rows, "", s.Muted.Render(status))
	return rows
}

func tableWindow(total, cursor, limit int) (int, int) {
	if total <= 0 || limit <= 0 || total <= limit {
		return 0, total
	}
	start := clamp(cursor-(limit/2), 0, max(total-limit, 0))
	end := min(start+limit, total)
	return start, end
}

func renderSessionSortLabel(mode stats.SessionSortMode) string {
	switch mode {
	case stats.SessionSortOldest:
		return "oldest"
	case stats.SessionSortCost:
		return "cost"
	case stats.SessionSortMessages:
		return "messages"
	default:
		return "newest"
	}
}

func totalSessionPages(list stats.SessionList) int {
	if list.PageSize <= 0 || list.Total <= 0 {
		return 1
	}
	pages := int((list.Total + int64(list.PageSize) - 1) / int64(list.PageSize))
	if pages < 1 {
		return 1
	}
	return pages
}
