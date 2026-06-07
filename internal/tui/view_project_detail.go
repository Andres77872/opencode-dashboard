package tui

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// errProjectStale is a sentinel error used when the project detail overlay's
// project no longer appears in the filtered/sorted project list after a refresh.
var errProjectStale = errors.New("project data may be stale")

func renderProjectDetailOverlay(s styles, width, height int, state projectDetailOverlayState) string {
	lines := []string{s.PanelTitle.Render("Project detail")}

	if state.loading {
		lines = append(lines, "", s.Muted.Render("Loading project detail…"))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.err != nil {
		message := state.err.Error()
		if errors.Is(state.err, errProjectStale) {
			message = "Project data may be stale. The project no longer appears in the current project list."
		} else if state.err == sql.ErrNoRows {
			message = "Project not found. It may have been deleted or the period may not contain data."
		}
		lines = append(lines, "", s.Danger.Render(truncateWithEllipsis(message, max(width-8, 20))))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.detail == nil {
		lines = append(lines, "", s.Muted.Render("No detail available for this project."))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	detail := state.detail

	// Header
	lines = append(lines,
		s.Accent.Render(truncateWithEllipsis(detail.ProjectName, max(width-8, 24))),
		s.Muted.Render(fmt.Sprintf("sessions %s • messages %s • cost %s",
			formatInt(detail.Sessions), formatInt(detail.Messages), plainCostProv(detail.Cost, detail.CostStatus, detail.CostProvenance))),
	)

	// Worktree path
	if detail.Worktree != "" {
		lines = append(lines, s.Muted.Render("path     "+truncateWithEllipsis(detail.Worktree, max(width-8, 24))))
	}

	// KPI cards row
	lines = append(lines, "")
	cardWidth := max((width-8)/4, 18)
	projectKPI := lipgloss.JoinHorizontal(lipgloss.Top,
		compactMetricCard(s, "Sessions", formatInt(detail.Sessions), "", cardWidth),
		compactMetricCard(s, "Messages", formatInt(detail.Messages), "", cardWidth),
		compactMetricCard(s, "Cost", formatMoneyProv(s, detail.Cost, detail.CostStatus, detail.CostProvenance, false), "", cardWidth),
		compactMetricCard(s, "Avg $/sess", formatMoneyProv(s, detail.Cost/float64(max(detail.Sessions, 1)), detail.CostStatus, detail.CostProvenance, false), "", cardWidth),
	)
	lines = append(lines, projectKPI)

	// Token breakdown
	totalTok := detail.Tokens.Input + detail.Tokens.Output + detail.Tokens.Reasoning + detail.Tokens.Cache.Read + detail.Tokens.Cache.Write
	if totalTok > 0 {
		lines = append(lines, "", s.Text.Render("Token breakdown"))
		barW := max(12, min(width/4, 20))
		lines = append(lines,
			fmt.Sprintf("Input       %s %s", progressBarWithPercent(s, float64(detail.Tokens.Input), float64(totalTok), barW), formatInt(detail.Tokens.Input)),
			fmt.Sprintf("Output      %s %s", progressBarWithPercent(s, float64(detail.Tokens.Output), float64(totalTok), barW), formatInt(detail.Tokens.Output)),
			fmt.Sprintf("Reasoning   %s %s", progressBarWithPercent(s, float64(detail.Tokens.Reasoning), float64(totalTok), barW), formatInt(detail.Tokens.Reasoning)),
			fmt.Sprintf("Cache Read  %s %s", progressBarWithPercent(s, float64(detail.Tokens.Cache.Read), float64(totalTok), barW), formatInt(detail.Tokens.Cache.Read)),
			fmt.Sprintf("Cache Write %s %s", progressBarWithPercent(s, float64(detail.Tokens.Cache.Write), float64(totalTok), barW), formatInt(detail.Tokens.Cache.Write)),
		)
	}

	// Recent sessions table
	lines = append(lines, "", s.Text.Render("Recent sessions"))
	if len(detail.RecentSessions) == 0 {
		lines = append(lines, s.Muted.Render("No sessions found for this project."))
	} else {
		titleW := max(width/3, 12)
		headerParts := []string{padRight("TITLE", titleW), padRight("DATE", 12), padLeft("MSG", 5), padLeft("COST", 10)}
		lines = append(lines, s.TableHeader.Render(strings.Join(headerParts, " ")))

		for i, session := range detail.RecentSessions {
			lineParts := []string{
				padRight(truncateWithEllipsis(session.Title, titleW), titleW),
				padRight(session.TimeUpdated.Format("2006-01-02"), 12),
				padLeft(formatInt(session.MessageCount), 5),
				padLeft(formatMoneyProv(s, session.Cost, session.CostStatus, session.CostProvenance, true), 10),
			}
			line := strings.Join(lineParts, " ")
			if i == state.cursor {
				lines = append(lines, s.TableRowActive.Render("> "+line))
			} else {
				lines = append(lines, s.TableRow.Render("  "+line))
			}
		}
	}

	// Pagination status
	totalPages := max(int((detail.TotalSessions+int64(defaultSessionsPageSize)-1)/int64(defaultSessionsPageSize)), 1)
	pageInfo := fmt.Sprintf("Page %d/%d • n next • p prev", state.page, totalPages)
	lines = append(lines, "", s.Muted.Render(pageInfo))

	// Footer
	lines = append(lines, s.Muted.Render("Esc closes overlay • r reloads • Enter opens session"))
	return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
}
