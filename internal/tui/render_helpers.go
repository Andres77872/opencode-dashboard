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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func trimCompactFloat(s string) string {
	return strings.Replace(s, ".0", "", 1)
}
