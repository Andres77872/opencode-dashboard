package tui

import (
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
)

func renderConfig(s styles, width, height int, cfg stats.ConfigView, opts Options, schema store.SchemaInfo) string {
	lines := []string{
		s.PanelTitle.Render("Runtime and config"),
		fmt.Sprintf("Database path   %s", opts.DBPath),
		fmt.Sprintf("Database source %s", opts.DBSource),
		fmt.Sprintf("Schema valid    %t", schema.IsValid),
		fmt.Sprintf("Config path     %s", cfg.Path),
		fmt.Sprintf("Config exists   %t", cfg.Exists),
		"",
		s.PanelTitle.Render("Redacted config preview"),
	}

	if !cfg.Exists {
		lines = append(lines, s.Muted.Render("No config file found at the detected XDG path."))
		return joinLines(lines...)
	}

	previewLines := strings.Split(cfg.Content, "\n")
	maxPreview := max(height-10, 6)
	for i, line := range previewLines {
		if i >= maxPreview {
			lines = append(lines, s.Muted.Render("…truncated in TUI core; full grouped config view lands in Phase 6."))
			break
		}
		lines = append(lines, truncateWithEllipsis(line, max(width-2, 20)))
	}

	return joinLines(lines...)
}
