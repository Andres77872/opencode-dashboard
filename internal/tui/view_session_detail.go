package tui

import (
	"database/sql"
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
)

func renderSessionDetailOverlay(s styles, width, height int, state sessionOverlayState) string {
	lines := []string{s.PanelTitle.Render("Session detail")}

	if state.loading {
		lines = append(lines, "", s.Muted.Render("Loading session detail…"))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.err != nil {
		message := state.err.Error()
		if state.err == sql.ErrNoRows {
			message = "Session no longer matches the current list state. Close this overlay and adjust the filter/page."
		}
		lines = append(lines, "", s.Danger.Render(truncateWithEllipsis(message, maxInt(width-8, 20))))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.detail == nil {
		lines = append(lines, "", s.Muted.Render("No detail available for this session."))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	detail := state.detail
	lines = append(lines,
		s.Accent.Render(truncateWithEllipsis(detail.Title, maxInt(width-8, 24))),
		s.Muted.Render(fmt.Sprintf("project %s • messages %s • cost %s", fallbackString(detail.ProjectName, "-"), formatInt(detail.MessageCount), formatMoney(detail.TotalCost))),
		s.Muted.Render(fmt.Sprintf("created %s • updated %s", detail.TimeCreated.Format("2006-01-02 15:04"), detail.TimeUpdated.Format("2006-01-02 15:04"))),
	)
	if detail.Directory != "" {
		lines = append(lines, s.Muted.Render("dir      "+truncateWithEllipsis(detail.Directory, maxInt(width-8, 24))))
	}
	lines = append(lines,
		"",
		s.Text.Render("Totals"),
		s.Muted.Render(fmt.Sprintf("tokens   %s", formatTokens(detail.TotalTokens))),
		s.Muted.Render(fmt.Sprintf("cache    %s read • %s write", formatInt(detail.TotalTokens.Cache.Read), formatInt(detail.TotalTokens.Cache.Write))),
		"",
		s.Text.Render("Recent message flow"),
	)

	messageRows := maxInt(height-len(lines)-3, 3)
	for _, row := range renderSessionMessageRows(s, detail.Messages, width-6, messageRows) {
		lines = append(lines, row)
	}

	lines = append(lines, "", s.Muted.Render("Esc closes overlay • r reloads current snapshot + detail"))
	return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
}

func renderSessionMessageRows(s styles, messages []stats.SessionMessage, width, limit int) []string {
	if len(messages) == 0 {
		return []string{s.Muted.Render("No messages recorded.")}
	}
	start := maxInt(len(messages)-limit, 0)
	rows := make([]string, 0, minInt(len(messages), limit)+1)
	for _, msg := range messages[start:] {
		meta := []string{msg.Role}
		if msg.ModelID != "" {
			meta = append(meta, truncateWithEllipsis(msg.ModelID, 18))
		}
		if msg.Agent != "" {
			meta = append(meta, truncateWithEllipsis(msg.Agent, 12))
		}
		if msg.Cost > 0 {
			meta = append(meta, formatMoney(msg.Cost))
		}
		if msg.Tokens != nil {
			meta = append(meta, fmt.Sprintf("%s tok", formatInt(msg.Tokens.Input+msg.Tokens.Output+msg.Tokens.Reasoning)))
		}
		line := fmt.Sprintf("%s  %s", msg.TimeCreated.Format("01-02 15:04"), strings.Join(meta, " • "))
		rows = append(rows, truncateWithEllipsis(line, width))
	}
	return rows
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
