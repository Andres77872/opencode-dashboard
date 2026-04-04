package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

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
		lines = append(lines, "", s.Danger.Render(truncateWithEllipsis(message, max(width-8, 20))))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	if state.detail == nil {
		lines = append(lines, "", s.Muted.Render("No detail available for this session."))
		return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
	}

	detail := state.detail
	lines = append(lines,
		s.Accent.Render(truncateWithEllipsis(detail.Title, max(width-8, 24))),
		s.Muted.Render(fmt.Sprintf("project %s • messages %s • cost %s", fallbackString(detail.ProjectName, "-"), formatInt(detail.MessageCount), formatMoney(detail.TotalCost))),
		s.Muted.Render(fmt.Sprintf("created %s • updated %s", detail.TimeCreated.Format("2006-01-02 15:04"), detail.TimeUpdated.Format("2006-01-02 15:04"))),
	)

	// Facts summary: Duration, Primary model, Total cost, Total messages
	duration := ""
	if !detail.TimeCreated.IsZero() && !detail.TimeUpdated.IsZero() {
		d := detail.TimeUpdated.Sub(detail.TimeCreated).Round(time.Second)
		if d < time.Minute {
			duration = fmt.Sprintf("%ds", int(d.Seconds()))
		} else if d < time.Hour {
			duration = fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
		} else {
			hours := int(d.Hours())
			mins := int(d.Minutes()) % 60
			duration = fmt.Sprintf("%dh %dm", hours, mins)
		}
	} else {
		duration = "--"
	}

	// Primary model (most messages or tokens)
	primaryModel := "N/A"
	modelCounts := make(map[string]int)
	for _, msg := range detail.Messages {
		if msg.ModelID != "" {
			modelCounts[msg.ModelID]++
		}
	}
	maxCount := 0
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			primaryModel = model
		}
	}

	if duration != "--" || primaryModel != "N/A" {
		factsLine := fmt.Sprintf("Duration %s • Primary model %s", duration, truncateWithEllipsis(primaryModel, 18))
		lines = append(lines, s.Muted.Render(factsLine))
	}

	if detail.Directory != "" {
		lines = append(lines, s.Muted.Render("dir      "+truncateWithEllipsis(detail.Directory, max(width-8, 24))))
	}
	lines = append(lines,
		"",
		s.Text.Render("Totals"),
		s.Muted.Render(fmt.Sprintf("tokens   %s", formatTokens(detail.TotalTokens))),
	)

	// Cache tokens with percentages (per spec: show both count and percentage)
	totalTok := detail.TotalTokens.Input + detail.TotalTokens.Output + detail.TotalTokens.Reasoning + detail.TotalTokens.Cache.Read + detail.TotalTokens.Cache.Write
	if totalTok > 0 {
		cacheReadPct := (float64(detail.TotalTokens.Cache.Read) / float64(totalTok)) * 100
		cacheWritePct := (float64(detail.TotalTokens.Cache.Write) / float64(totalTok)) * 100
		lines = append(lines,
			s.Muted.Render(fmt.Sprintf("cache    %s read (%.1f%%) • %s write (%.1f%%)",
				formatInt(detail.TotalTokens.Cache.Read), cacheReadPct,
				formatInt(detail.TotalTokens.Cache.Write), cacheWritePct)),
		)
	} else {
		lines = append(lines,
			s.Muted.Render(fmt.Sprintf("cache    %s read • %s write",
				formatInt(detail.TotalTokens.Cache.Read), formatInt(detail.TotalTokens.Cache.Write))),
		)
	}

	// Message mix with percentages and count summary
	if len(detail.Messages) > 0 {
		userPct, assistantPct, systemPct := calculateMessageMix(detail.Messages)
		var userCount, assistantCount, systemCount int
		for _, msg := range detail.Messages {
			switch msg.Role {
			case "user":
				userCount++
			case "assistant":
				assistantCount++
			case "system":
				systemCount++
			}
		}
		lines = append(lines,
			"",
			s.Text.Render("Message mix"),
			// Count summary per spec: "U: x | A: y | S: z"
			s.Muted.Render(fmt.Sprintf("U: %d | A: %d | S: %d", userCount, assistantCount, systemCount)),
			fmt.Sprintf("User      %s", progressBarWithPercent(s, userPct, 100, 20)),
			fmt.Sprintf("Assistant %s", progressBarWithPercent(s, assistantPct, 100, 20)),
			fmt.Sprintf("System    %s", progressBarWithPercent(s, systemPct, 100, 20)),
		)
	}

	// Peak row identification
	if len(detail.Messages) > 0 {
		peakIdx, peakTokens := findPeakRow(detail.Messages)
		if peakIdx >= 0 {
			peakCost := 0.0
			if peakIdx < len(detail.Messages) {
				peakCost = detail.Messages[peakIdx].Cost
			}
			lines = append(lines,
				"",
				s.Text.Render("Peak message"),
				s.Muted.Render(fmt.Sprintf("Row %d • %s tokens • %s", peakIdx+1, formatCompactInt(peakTokens), formatMoney(peakCost))),
			)
		}
	}

	// Token breakdown with percentages (reuse totalTok from cache section)
	totalTok = detail.TotalTokens.Input + detail.TotalTokens.Output + detail.TotalTokens.Reasoning + detail.TotalTokens.Cache.Read + detail.TotalTokens.Cache.Write
	if totalTok > 0 {
		lines = append(lines,
			"",
			s.Text.Render("Token breakdown"),
			fmt.Sprintf("Input     %s %s", progressBarWithPercent(s, float64(detail.TotalTokens.Input), float64(totalTok), 16), formatInt(detail.TotalTokens.Input)),
			fmt.Sprintf("Output    %s %s", progressBarWithPercent(s, float64(detail.TotalTokens.Output), float64(totalTok), 16), formatInt(detail.TotalTokens.Output)),
			fmt.Sprintf("Reasoning %s %s", progressBarWithPercent(s, float64(detail.TotalTokens.Reasoning), float64(totalTok), 16), formatInt(detail.TotalTokens.Reasoning)),
		)
	}

	lines = append(lines,
		"",
		s.Text.Render("Recent message flow"),
	)

	messageRows := calculateMessageRows(height, len(lines))
	for _, row := range renderSessionMessageRows(s, detail.Messages, width-4, messageRows) {
		lines = append(lines, row)
	}

	lines = append(lines, "", s.Muted.Render("Esc closes overlay • r reloads current snapshot + detail"))
	return s.OverlayPanel.Width(width).Height(height).Render(joinLines(lines...))
}

func renderSessionMessageRows(s styles, messages []stats.SessionMessage, width, limit int) []string {
	if len(messages) == 0 {
		return []string{s.Muted.Render("No messages recorded.")}
	}
	start := max(len(messages)-limit, 0)
	rows := make([]string, 0, min(len(messages), limit)+1)
	for _, msg := range messages[start:] {
		meta := []string{msg.Role}
		if msg.ModelID != "" {
			meta = append(meta, truncateWithEllipsis(msg.ModelID, 18))
		}
		if msg.ProviderID != "" {
			meta = append(meta, truncateWithEllipsis(msg.ProviderID, 14))
		}
		if msg.Agent != "" {
			meta = append(meta, truncateWithEllipsis(msg.Agent, 12))
		}
		meta = append(meta, formatMoney(msg.Cost))
		if msg.Tokens != nil {
			meta = append(meta, fmt.Sprintf("%s tok", formatInt(msg.Tokens.Input+msg.Tokens.Output+msg.Tokens.Reasoning+msg.Tokens.Cache.Read+msg.Tokens.Cache.Write)))
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

func calculateMessageRows(height int, lineCount int) int {
	available := height - lineCount - 3
	minRows := max(3, height/6)
	return max(available, minRows)
}
