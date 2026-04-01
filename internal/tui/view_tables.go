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
		s.Muted.Render("Dense browse flow with cursor, sort, and inline filter."),
		"",
		s.TableHeader.Render(fmt.Sprintf("%s %s %s %s %s", padRight("MODEL", max(width-42, 16)), padRight("PROVIDER", 12), padLeft("SESS", 6), padLeft("MSG", 6), padLeft("COST", 10))),
	}
	limit := min(len(items), max(height-6, 5))
	nameWidth := max(width-42, 16)
	if len(items) == 0 {
		message := "No assistant model usage found."
		if state.filter != "" {
			message = "No models match the current filter."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		start, end := tableWindow(len(items), state.cursor, limit)
		for i := start; i < end; i++ {
			item := items[i]
			line := fmt.Sprintf("%s %s %s %s %s", padRight(truncateWithEllipsis(item.ModelID, nameWidth), nameWidth), padRight(truncateWithEllipsis(item.ProviderID, 12), 12), padLeft(formatInt(item.Sessions), 6), padLeft(formatInt(item.Messages), 6), padLeft(formatMoney(item.Cost), 10))
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
		s.Muted.Render("Top tool usage with cursor, sort, and inline filter."),
		"",
		s.TableHeader.Render(fmt.Sprintf("%s %s %s %s %s", padRight("TOOL", max(width-34, 16)), padLeft("RUNS", 7), padLeft("OK", 7), padLeft("ERR", 7), padLeft("SESS", 7))),
	}
	limit := min(len(items), max(height-6, 5))
	nameWidth := max(width-34, 16)
	if len(items) == 0 {
		message := "No tool invocation data found."
		if state.filter != "" {
			message = "No tools match the current filter."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		start, end := tableWindow(len(items), state.cursor, limit)
		for i := start; i < end; i++ {
			item := items[i]
			line := fmt.Sprintf("%s %s %s %s %s", padRight(truncateWithEllipsis(item.Name, nameWidth), nameWidth), padLeft(formatInt(item.Invocations), 7), padLeft(formatInt(item.Successes), 7), padLeft(formatInt(item.Failures), 7), padLeft(formatInt(item.Sessions), 7))
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
		s.Muted.Render("Project concentration with cursor, sort, and inline filter."),
		"",
		s.TableHeader.Render(fmt.Sprintf("%s %s %s %s", padRight("PROJECT", max(width-31, 16)), padLeft("SESS", 7), padLeft("MSG", 7), padLeft("COST", 10))),
	}
	limit := min(len(items), max(height-6, 5))
	nameWidth := max(width-31, 16)
	if len(items) == 0 {
		message := "No project activity found."
		if state.filter != "" {
			message = "No projects match the current filter."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		start, end := tableWindow(len(items), state.cursor, limit)
		for i := start; i < end; i++ {
			item := items[i]
			line := fmt.Sprintf("%s %s %s %s", padRight(truncateWithEllipsis(item.ProjectName, nameWidth), nameWidth), padLeft(formatInt(item.Sessions), 7), padLeft(formatInt(item.Messages), 7), padLeft(formatMoney(item.Cost), 10))
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
		s.Muted.Render("Dense browse flow with filter, sort, and Enter drill-down."),
		"",
		s.TableHeader.Render(fmt.Sprintf("%s %s %s %s %s", padRight("TITLE", max(width-45, 16)), padRight("PROJECT", 12), padRight("UPDATED", 12), padLeft("MSG", 5), padLeft("COST", 10))),
	}

	if len(list.Sessions) == 0 {
		message := "No sessions match the current view."
		if state.filter == "" {
			message = "No sessions on this page."
		}
		rows = append(rows, s.Muted.Render(message))
	} else {
		limit := min(len(list.Sessions), max(height-6, 5))
		titleWidth := max(width-45, 16)
		for i, item := range list.Sessions[:limit] {
			line := fmt.Sprintf("%s %s %s %s %s", padRight(truncateWithEllipsis(item.Title, titleWidth), titleWidth), padRight(truncateWithEllipsis(item.ProjectName, 12), 12), padRight(item.TimeUpdated.Format("2006-01-02"), 12), padLeft(formatInt(item.MessageCount), 5), padLeft(formatMoney(item.Cost), 10))
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
