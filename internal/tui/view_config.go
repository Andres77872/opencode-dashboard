package tui

import (
	"fmt"
	"sort"
	"strings"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

// configSections returns the sorted top-level keys from the config content.
func (m *model) configSections() []string {
	if m.data.Config.Content == nil {
		return nil
	}
	keys := make([]string, 0, len(m.data.Config.Content))
	for k := range m.data.Config.Content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// configSectionKeys returns the sorted dot-prefixed keys for the current section.
func (m *model) configSectionKeys() []string {
	sections := m.configSections()
	if m.config.section < 0 || m.config.section >= len(sections) {
		return nil
	}
	section := sections[m.config.section]
	value := m.data.Config.Content[section]
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, section+"."+k)
		}
		sort.Strings(keys)
		return keys
	default:
		return []string{section}
	}
}

// configValueForKey returns the value for a dot-prefixed key path within the current section.
func (m *model) configValueForKey(path string) any {
	sections := m.configSections()
	if m.config.section < 0 || m.config.section >= len(sections) {
		return nil
	}
	section := sections[m.config.section]
	value := m.data.Config.Content[section]

	// If the key is just "section.keyname", extract from the map
	if strings.HasPrefix(path, section+".") {
		key := path[len(section)+1:]
		switch v := value.(type) {
		case map[string]any:
			return v[key]
		}
	}
	return value
}

// isRedactedValue checks if the rendered value contains a redaction marker.
func isRedactedValue(val any) bool {
	str := fmt.Sprintf("%v", val)
	return strings.Contains(str, "[REDACTED]") || strings.Contains(str, "****")
}

// formatConfigValue renders a config value for display.
// Returns the display string and whether it's a nested object.
func formatConfigValue(val any) (display string, isNested bool) {
	if val == nil {
		return "null", false
	}
	switch v := val.(type) {
	case string:
		return v, false
	case bool:
		return fmt.Sprintf("%t", v), false
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v), false
		}
		return fmt.Sprintf("%g", v), false
	case map[string]any:
		return fmt.Sprintf("{%d keys}", len(v)), true
	case []any:
		return fmt.Sprintf("[%d items]", len(v)), len(v) > 0
	default:
		return fmt.Sprintf("%v", v), false
	}
}

func renderConfig(s styles, width, height int, cfg stats.ConfigView, info source.SourceInfo, cs *configState) string {
	kind := info.Kind
	if kind == "" {
		kind = "—"
	}
	lines := []string{
		s.PanelTitle.Render("Runtime and config"),
		fmt.Sprintf("Source          %s", sourceLabelOrID(info, info.ID)),
		fmt.Sprintf("Kind            %s", kind),
		fmt.Sprintf("Path            %s", info.Path),
		fmt.Sprintf("Available       %t", info.Available),
		fmt.Sprintf("Config path     %s", cfg.Path),
		fmt.Sprintf("Config exists   %t", cfg.Exists),
	}

	if !cfg.Exists {
		lines = append(lines, "", s.Muted.Render("No config file found at the detected XDG path."))
		return joinLines(lines...)
	}

	if cfg.Content == nil || len(cfg.Content) == 0 {
		lines = append(lines, "", s.Muted.Render("Config file exists but has no content."))
		return joinLines(lines...)
	}

	sections := make([]string, 0, len(cfg.Content))
	for k := range cfg.Content {
		sections = append(sections, k)
	}
	sort.Strings(sections)

	// Summary stat line
	redactedCount := 0
	for _, k := range sections {
		if isRedactedValue(cfg.Content[k]) {
			redactedCount++
		}
	}
	lines = append(lines, "",
		s.Muted.Render(fmt.Sprintf("%d sections • %d top-level keys", len(sections), len(cfg.Content))),
		"",
	)

	if cs.section == -1 {
		// Section list view
		lines = append(lines, s.Text.Render("Sections (Enter to drill down, j/k to navigate)"))
		for i, section := range sections {
			val := cfg.Content[section]
			desc := ""
			switch v := val.(type) {
			case map[string]any:
				desc = fmt.Sprintf("(%d keys)", len(v))
			case []any:
				desc = fmt.Sprintf("[%d items]", len(v))
			default:
				disp, _ := formatConfigValue(val)
				desc = truncateWithEllipsis(disp, max(width/3, 16))
			}
			redacted := ""
			if isRedactedValue(val) {
				redacted = " ⚡"
			}
			entry := fmt.Sprintf("  %s  (%s)%s", section, desc, redacted)
			if i == cs.cursor {
				lines = append(lines, s.TableRowActive.Render("> "+entry))
			} else {
				lines = append(lines, s.TableRow.Render(entry))
			}
		}
	} else {
		// Section detail view
		if cs.section >= 0 && cs.section < len(sections) {
			section := sections[cs.section]
			lines = append(lines, s.Muted.Render("◄ [/] or Esc to go back • "+section+" section"))

			value := cfg.Content[section]
			switch v := value.(type) {
			case map[string]any:
				keys := make([]string, 0, len(v))
				for k := range v {
					keys = append(keys, k)
				}
				sort.Strings(keys)

				if len(keys) == 0 {
					lines = append(lines, s.Muted.Render("  (empty section)"))
				} else {
					for i, key := range keys {
						val := v[key]
						path := section + "." + key
						display, isNested := formatConfigValue(val)

						// Check if nested and expanded
						if isNested && cs.expanded[path] {
							// Render expanded nested content
							expandedDisplay := renderExpandedValue(val, "  ")
							if i == cs.cursor {
								lines = append(lines, s.TableRowActive.Render("> "+key+":"))
							} else {
								lines = append(lines, s.TableRow.Render("  "+key+":"))
							}
							for _, expLine := range strings.Split(expandedDisplay, "\n") {
								lines = append(lines, "    "+s.Muted.Render(expLine))
							}
						} else {
							// Check redaction
							suffix := ""
							if isRedactedValue(val) {
								suffix = s.Info.Render(" [REDACTED]")
							}

							if isNested {
								display = s.Text.Render(display)
							} else {
								display = truncateWithEllipsis(display, max(width-20, 10))
							}

							entry := fmt.Sprintf("  %s: %s%s", key, display, suffix)
							if i == cs.cursor {
								lines = append(lines, s.TableRowActive.Render("> "+entry))
							} else {
								lines = append(lines, s.TableRow.Render(entry))
							}
						}
					}
				}
			case []any:
				for i, item := range v {
					display := fmt.Sprintf("%v", item)
					entry := fmt.Sprintf("  [%d] %s", i, truncateWithEllipsis(display, max(width-16, 10)))
					if i == cs.cursor {
						lines = append(lines, s.TableRowActive.Render("> "+entry))
					} else {
						lines = append(lines, s.TableRow.Render(entry))
					}
				}
			default:
				display, _ := formatConfigValue(value)
				lines = append(lines, "  "+truncateWithEllipsis(display, max(width-8, 10)))
			}
		}
	}

	return joinLines(lines...)
}

// renderExpandedValue renders a nested map/array value inline with indentation.
func renderExpandedValue(val any, indent string) string {
	var parts []string
	switch v := val.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := v[k]
			disp, isNested := formatConfigValue(val)
			if isNested {
				parts = append(parts, fmt.Sprintf("%s%s: %s", indent, k, disp))
			} else {
				parts = append(parts, fmt.Sprintf("%s%s: %s", indent, k, disp))
			}
		}
	case []any:
		for i, item := range v {
			parts = append(parts, fmt.Sprintf("%s[%d] %v", indent, i, item))
		}
	}
	return strings.Join(parts, "\n")
}
