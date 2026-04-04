package tui

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"opencode-dashboard/internal/stats"
)

func renderDayMessagesOverlayContent(s styles, width, height int, state dayMessagesOverlayState) string {
	lines := []string{s.PanelTitle.Render(fmt.Sprintf("Messages for %s", state.date))}

	if state.loading {
		lines = append(lines, "", s.Muted.Render("Loading messages..."))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.err != nil {
		message := state.err.Error()
		if state.err == sql.ErrNoRows {
			message = "No messages found for this day."
		}
		lines = append(lines, "", s.Danger.Render(truncateWithEllipsis(message, max(width-8, 20))))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if len(state.messages.Messages) == 0 {
		lines = append(lines, "", s.Muted.Render("No messages recorded for this day."), "")
		lines = append(lines, s.Muted.Render("Esc closes overlay"))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	total := state.messages.Total
	page := state.messages.Page
	pageSize := state.messages.PageSize
	from := (page-1)*pageSize + 1
	to := min(page*pageSize, int(total))
	pageInfo := fmt.Sprintf("Showing %d-%d of %d", from, to, total)

	lines = append(lines, s.Muted.Render(pageInfo), "")

	start, end := tableWindow(len(state.messages.Messages), state.cursor, height-10)
	for i := start; i < end; i++ {
		msg := state.messages.Messages[i]
		row := renderMessageRow(s, msg, width-4, i == state.cursor)
		lines = append(lines, row)
	}

	hasNext := hasNextMessagePage(state.messages)
	navParts := []string{"j/k move", "Enter detail"}
	if page > 1 {
		navParts = append(navParts, "p prev page")
	}
	if hasNext {
		navParts = append(navParts, "n next page")
	}
	navParts = append(navParts, "Esc close")

	lines = append(lines, "", s.Muted.Render(strings.Join(navParts, " • ")))
	return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
}

func renderMessageRow(s styles, msg stats.MessageEntry, width int, isActive bool) string {
	timeStr := msg.TimeCreated.Format("15:04")
	roleStyle := s.Text
	if msg.Role == "user" {
		roleStyle = s.Info
	} else if msg.Role == "assistant" {
		roleStyle = s.Accent
	} else if msg.Role == "system" {
		roleStyle = s.Muted
	}

	role := roleStyle.Render(msg.Role)

	sessionPart := ""
	if msg.SessionTitle != "" {
		sessionPart = truncateWithEllipsis(msg.SessionTitle, 20)
	} else {
		sessionPart = truncateWithEllipsis(msg.SessionID, 12)
	}

	costStr := formatMoney(msg.Cost)
	tokensStr := ""
	if msg.Tokens != nil {
		totalTok := msg.Tokens.Input + msg.Tokens.Output + msg.Tokens.Reasoning + msg.Tokens.Cache.Read + msg.Tokens.Cache.Write
		tokensStr = formatCompactInt(totalTok) + " tok"
	}

	modelStr := ""
	if msg.ModelID != "" {
		modelStr = truncateWithEllipsis(msg.ModelID, 14)
	}

	parts := []string{timeStr, role, sessionPart}
	if modelStr != "" {
		parts = append(parts, modelStr)
	}
	if msg.Role == "assistant" {
		parts = append(parts, costStr)
		if tokensStr != "" {
			parts = append(parts, tokensStr)
		}
	}

	line := strings.Join(parts, " • ")
	line = truncateWithEllipsis(line, width)

	if isActive {
		return s.TableRowActive.Render("> " + line)
	}
	return s.TableRow.Render("  " + line)
}

func renderMessageDetailOverlayContent(s styles, width, height int, state messageDetailOverlayState) string {
	lines := []string{s.PanelTitle.Render("Message detail")}

	if state.loading {
		lines = append(lines, "", s.Muted.Render("Loading message detail..."))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.err != nil {
		message := state.err.Error()
		if state.err == sql.ErrNoRows {
			message = "Message no longer available."
		}
		lines = append(lines, "", s.Danger.Render(truncateWithEllipsis(message, max(width-8, 20))))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.detail == nil {
		lines = append(lines, "", s.Muted.Render("No detail available for this message."), "")
		lines = append(lines, s.Muted.Render("Esc closes overlay"))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	detail := state.detail

	roleStyle := s.Text
	if detail.Role == "user" {
		roleStyle = s.Info
	} else if detail.Role == "assistant" {
		roleStyle = s.Accent
	} else if detail.Role == "system" {
		roleStyle = s.Muted
	}

	sessionPart := ""
	if detail.SessionTitle != "" {
		sessionPart = truncateWithEllipsis(detail.SessionTitle, 30)
	} else {
		sessionPart = truncateWithEllipsis(detail.SessionID, 20)
	}

	lines = append(lines,
		roleStyle.Render(detail.Role),
		s.Muted.Render(fmt.Sprintf("Session %s", sessionPart)),
		s.Muted.Render(fmt.Sprintf("Time %s", detail.TimeCreated.Format("2006-01-02 15:04:05"))),
	)

	if detail.Role == "assistant" {
		lines = append(lines, "")
		lines = append(lines, s.Text.Render("Metadata"))
		metaParts := []string{fmt.Sprintf("Cost %s", formatMoney(detail.Cost))}
		if detail.ModelID != "" {
			metaParts = append(metaParts, fmt.Sprintf("Model %s", truncateWithEllipsis(detail.ModelID, 18)))
		}
		if detail.ProviderID != "" {
			metaParts = append(metaParts, fmt.Sprintf("Provider %s", truncateWithEllipsis(detail.ProviderID, 12)))
		}
		lines = append(lines, s.Muted.Render(strings.Join(metaParts, " • ")))

		if detail.Tokens != nil {
			tokParts := []string{
				fmt.Sprintf("Input %s", formatInt(detail.Tokens.Input)),
				fmt.Sprintf("Output %s", formatInt(detail.Tokens.Output)),
				fmt.Sprintf("Reasoning %s", formatInt(detail.Tokens.Reasoning)),
			}
			lines = append(lines, s.Muted.Render(fmt.Sprintf("Tokens %s", strings.Join(tokParts, " • "))))
		}
	}

	textParts := detail.Content.TextParts
	reasoningParts := detail.Content.ReasoningParts
	toolParts := detail.Content.ToolParts

	maxContentLines := height - len(lines) - 4
	if len(textParts) > 0 {
		lines = append(lines, "", s.Text.Render("Text content"))
		textCount := 0
		for _, part := range textParts {
			if textCount >= maxContentLines/2 {
				break
			}
			truncated := truncateContent(part.Text, width-6)
			wrapped := wrapText(truncated, width-4)
			for _, wrappedLine := range wrapped {
				if textCount >= maxContentLines/2 {
					break
				}
				lines = append(lines, s.Text.Render(wrappedLine))
				textCount++
			}
		}
		if len(textParts) > 1 || textCount >= maxContentLines/2 {
			lines = append(lines, s.Muted.Render(fmt.Sprintf("(%d text parts)", len(textParts))))
		}
	}

	if len(reasoningParts) > 0 {
		lines = append(lines, "", s.Info.Render("Reasoning content"))
		reasonCount := 0
		for _, part := range reasoningParts {
			if reasonCount >= maxContentLines/2 {
				break
			}
			truncated := truncateContent(part.Text, width-6)
			wrapped := wrapText(truncated, width-4)
			for _, wrappedLine := range wrapped {
				if reasonCount >= maxContentLines/2 {
					break
				}
				lines = append(lines, s.Info.Render(wrappedLine))
				reasonCount++
			}
		}
		if len(reasoningParts) > 1 || reasonCount >= maxContentLines/2 {
			lines = append(lines, s.Muted.Render(fmt.Sprintf("(%d reasoning parts)", len(reasoningParts))))
		}
	}

	if len(toolParts) > 0 {
		lines = append(lines, "", s.Accent.Render("Tool activity"))
		toolCount := 0
		toolBudget := max(maxContentLines/2, 4)
		for _, part := range toolParts {
			if toolCount >= toolBudget {
				break
			}

			for _, row := range renderToolPartRows(s, part, width-4) {
				if toolCount >= toolBudget {
					break
				}
				lines = append(lines, row)
				toolCount++
			}
		}
		if len(toolParts) > 1 || toolCount >= toolBudget {
			lines = append(lines, s.Muted.Render(fmt.Sprintf("(%d tool parts)", len(toolParts))))
		}
	}

	lines = append(lines, "", s.Muted.Render("Esc closes overlay • r refreshes"))
	return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
}

func renderToolPartRows(s styles, part stats.ToolPart, width int) []string {
	if width <= 0 {
		return nil
	}

	statusLabel := part.State.Status
	if statusLabel == "" {
		statusLabel = "unknown"
	}

	header := fmt.Sprintf("%s [%s]", toolDisplayName(part), strings.ToUpper(statusLabel))
	if part.State.Title != "" {
		header += " • " + part.State.Title
	}

	rows := []string{toolStatusStyle(s, statusLabel).Render(truncateContent(header, max(width-2, 12)))}

	inputSummary := summarizeToolInput(part.State.Input)
	if inputSummary != "" {
		rows = append(rows, s.Muted.Render(truncateContent("in: "+inputSummary, max(width-2, 12))))
	}

	if summary := toolPrimarySummary(part.State); summary != "" {
		style := s.Text
		label := "out: "
		if statusLabel == "error" {
			style = s.Danger
			label = "err: "
		}

		for _, wrapped := range wrapText(truncateContent(label+summary, max(width-2, 12)), max(width, 12)) {
			rows = append(rows, style.Render(wrapped))
		}
	}

	return rows
}

func toolDisplayName(part stats.ToolPart) string {
	if part.Tool != "" {
		return part.Tool
	}
	return "unknown-tool"
}

func toolStatusStyle(s styles, status string) interface{ Render(...string) string } {
	switch status {
	case "completed":
		return s.Success
	case "error":
		return s.Danger
	case "running":
		return s.Accent
	case "pending":
		return s.Info
	default:
		return s.Text
	}
}

func summarizeToolInput(input map[string]interface{}) string {
	if len(input) == 0 {
		return ""
	}

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, min(len(keys), 4))
	for i, key := range keys {
		if i >= 4 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, truncateContent(stringifyToolValue(input[key]), 40)))
	}

	if len(keys) > 4 {
		parts = append(parts, fmt.Sprintf("+%d more", len(keys)-4))
	}

	return strings.Join(parts, " • ")
}

func toolPrimarySummary(state stats.ToolState) string {
	if state.Status == "error" && state.Error != "" {
		return state.Error
	}
	if state.Status == "completed" && state.Output != "" {
		return state.Output
	}
	if state.Title != "" {
		return state.Title
	}
	return ""
}

func stringifyToolValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "..."
}

func wrapText(text string, maxWidth int) []string {
	if len(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	for len(text) > maxWidth {
		lines = append(lines, text[:maxWidth])
		text = text[maxWidth:]
	}
	if len(text) > 0 {
		lines = append(lines, text)
	}
	return lines
}
