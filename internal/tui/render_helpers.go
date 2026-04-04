package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/stats"
)

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}

func formatInt(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	parts := make([]string, 0, 4)
	for {
		if v < 1000 {
			parts = append(parts, fmt.Sprintf("%d", v))
			break
		}
		parts = append(parts, fmt.Sprintf("%03d", v%1000))
		v /= 1000
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	result := strings.Join(parts, ",")
	if neg {
		return "-" + result
	}
	return result
}

func formatCompactInt(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}

	var out string
	switch {
	case v >= 1_000_000_000:
		out = trimCompactFloat(fmt.Sprintf("%.1fb", float64(v)/1_000_000_000))
	case v >= 1_000_000:
		out = trimCompactFloat(fmt.Sprintf("%.1fm", float64(v)/1_000_000))
	case v >= 1_000:
		out = trimCompactFloat(fmt.Sprintf("%.1fk", float64(v)/1_000))
	default:
		out = formatInt(v)
	}

	if neg {
		return "-" + out
	}
	return out
}

func formatMoney(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func formatTokens(tokens stats.TokenStats) string {
	return fmt.Sprintf("%s in • %s out • %s reason", formatInt(tokens.Input), formatInt(tokens.Output), formatInt(tokens.Reasoning))
}

func asciiBar(value, maxValue float64, width int) string {
	if width < 1 || value <= 0 || maxValue <= 0 {
		return ""
	}
	filled := int(math.Round((value / maxValue) * float64(width)))
	filled = clamp(filled, 1, width)
	return strings.Repeat("█", filled)
}

func truncateWithEllipsis(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	current := lipgloss.Width(s)
	if current >= width {
		return truncateWithEllipsis(s, width)
	}
	return s + strings.Repeat(" ", width-current)
}

func padLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	current := lipgloss.Width(s)
	if current >= width {
		return truncateWithEllipsis(s, width)
	}
	return strings.Repeat(" ", width-current) + s
}

func metricCard(s styles, title, value, detail string, width int) string {
	content := []string{
		s.MetricLabel.Render(title),
		s.MetricValue.Render(value),
		s.Muted.Render(detail),
	}
	return s.MetricCard.Width(width).Render(joinLines(content...))
}

func loadedLabel(t time.Time, loaded bool) string {
	if !loaded || t.IsZero() {
		return "never"
	}
	d := time.Since(t).Round(time.Second)
	if d < time.Second {
		return "just now"
	}
	return d.String() + " ago"
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func trimCompactFloat(s string) string {
	return strings.Replace(s, ".0", "", 1)
}

// progressBar renders a solid-colored progress bar using █ for filled (Accent color) and ░ for empty (Muted color).
// Returns empty muted dots if value <= 0 or maxValue <= 0 or width < 1.
func progressBar(s styles, value, maxValue float64, width int) string {
	if width < 1 || value <= 0 || maxValue <= 0 {
		return s.Muted.Render(strings.Repeat("░", width))
	}
	percentage := value / maxValue
	if percentage > 1.0 {
		percentage = 1.0
	}
	filled := int(math.Round(percentage * float64(width)))
	filled = clamp(filled, 0, width)
	empty := width - filled

	bar := s.Accent.Render(strings.Repeat("█", filled))
	if empty > 0 {
		bar += s.Muted.Render(strings.Repeat("░", empty))
	}
	return bar
}

// progressBarWithPercent renders a progress bar with percentage text appended.
// Subtracts 6 chars from width for percent text (e.g., " 50%").
func progressBarWithPercent(s styles, value, maxValue float64, width int) string {
	if width < 8 {
		// Not enough space for bar + percent
		percent := 0
		if maxValue > 0 && value > 0 {
			percent = int(math.Round((value / maxValue) * 100))
		}
		return s.Muted.Render(fmt.Sprintf("%3d%%", percent))
	}

	bar := progressBar(s, value, maxValue, width-6)
	percent := int(math.Round((value / maxValue) * 100))
	if percent > 100 {
		percent = 100
	}
	return bar + " " + s.Muted.Render(fmt.Sprintf("%3d%%", percent))
}

// calculateMessageMix counts messages by role and returns percentages (0-100).
// Returns 0, 0, 0 for empty list.
func calculateMessageMix(messages []stats.SessionMessage) (userPct, assistantPct, systemPct float64) {
	if len(messages) == 0 {
		return 0, 0, 0
	}

	var userCount, assistantCount, systemCount int
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		case "system":
			systemCount++
		}
	}

	total := float64(len(messages))
	userPct = (float64(userCount) / total) * 100
	assistantPct = (float64(assistantCount) / total) * 100
	systemPct = (float64(systemCount) / total) * 100

	return userPct, assistantPct, systemPct
}

// findPeakRow scans for message with highest token count and returns index and token sum.
// Returns -1, 0 for empty list or messages with no token data.
func findPeakRow(messages []stats.SessionMessage) (peakIdx int, peakTokens int64) {
	if len(messages) == 0 {
		return -1, 0
	}

	peakIdx = -1
	peakTokens = 0

	for i, msg := range messages {
		if msg.Tokens == nil {
			continue
		}
		totalTokens := msg.Tokens.Input + msg.Tokens.Output + msg.Tokens.Reasoning + msg.Tokens.Cache.Read + msg.Tokens.Cache.Write
		if totalTokens > peakTokens {
			peakIdx = i
			peakTokens = totalTokens
		}
	}

	return peakIdx, peakTokens
}

// renderStatusBadge returns a styled status badge based on success rate.
// Thresholds per spec:
//   - >95%: "OK" (Success style)
//   - ≥80% and ≤95%: "--" (Muted style, neutral)
//   - <80%: "WARN" (Info style, warning)
//   - no data (successRate < 0 or invocations=0): "--" (Muted style, neutral)
func renderStatusBadge(s styles, successRate float64) string {
	// No data case (passed as -1 from caller when invocations=0)
	if successRate < 0 {
		return s.Muted.Render("--")
	}
	// Success: >95%
	if successRate > 95 {
		return s.Success.Bold(true).Render("OK")
	}
	// Neutral: ≥80% and ≤95%
	if successRate >= 80 {
		return s.Muted.Render("--")
	}
	// Warning: <80%
	return s.Info.Bold(true).Render("WARN")
}

// LeaderEntry represents an item in a leader summary section.
type LeaderEntry struct {
	Name  string
	Value float64 // Cost or invocations
}

// renderLeaderSection renders a reusable leader summary section for top N items.
// Used by Models (top 3 by cost), Tools (top 2 by invocations), Projects (top 3 by cost).
// Returns formatted leader cards joined horizontally (or vertically on narrow width).
func renderLeaderSection(s styles, width int, leaders []LeaderEntry, total float64, cardTitleFormat string, valueFormatter func(float64) string) string {
	if len(leaders) < 2 {
		return "" // No leader section for single item per spec
	}

	leaderCount := min(3, len(leaders))
	cardWidth := max((width-4)/leaderCount, 22)
	if width < 70 {
		cardWidth = max(width-4, 20) // Single card width for stacked layout
	}

	leaderCards := make([]string, leaderCount)
	for i := 0; i < leaderCount; i++ {
		item := leaders[i]
		name := truncateWithEllipsis(item.Name, max(cardWidth-6, 12))
		leaderCards[i] = metricCard(s, fmt.Sprintf(cardTitleFormat, i+1), valueFormatter(item.Value),
			fmt.Sprintf("%s\n%s", progressBarWithPercent(s, item.Value, max(total, 1), max(cardWidth-8, 10)), name), cardWidth)
	}

	if width >= 70 {
		return lipgloss.JoinHorizontal(lipgloss.Top, leaderCards...)
	}

	// Stack vertically on narrow terminals
	result := ""
	for i, card := range leaderCards {
		if i > 0 {
			result += "\n"
		}
		result += card
	}
	return result
}
