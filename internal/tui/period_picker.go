package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/stats"
)

type periodPickerMode int

const (
	periodPickerPresets periodPickerMode = iota
	periodPickerCustom
)

// periodPresets is the global set of quick ranges, ordered shortest → longest.
var periodPresets = []string{"1h", "6h", "12h", "24h", "72h", "1d", "7d", "14d", "30d", "1y", "all"}

var periodPresetLabels = map[string]string{
	"1h":  "Last hour",
	"6h":  "Last 6 hours",
	"12h": "Last 12 hours",
	"24h": "Last 24 hours",
	"72h": "Last 72 hours",
	"1d":  "Today",
	"7d":  "Last 7 days",
	"14d": "Last 14 days",
	"30d": "Last 30 days",
	"1y":  "Last year",
	"all": "All time",
}

// periodPickerOverlayState drives the global time-range picker overlay.
type periodPickerOverlayState struct {
	visible     bool
	mode        periodPickerMode
	cursor      int // 0..len(periodPresets)-1 selects a preset; len selects "Custom range…"
	fromDraft   string
	toDraft     string
	customField int // 0 = from, 1 = to
	err         string
}

func indexOfPreset(period string) int {
	for i, p := range periodPresets {
		if p == period {
			return i
		}
	}
	return -1
}

// periodLabel renders the active period for the status bar / chrome.
func periodLabel(pq stats.PeriodQuery) string {
	if pq.From != "" {
		if pq.To == "" {
			return pq.From + " → now"
		}
		return pq.From + " → " + pq.To
	}
	if pq.Period == "" {
		return "7d"
	}
	return pq.Period
}

func (m *model) openPeriodPicker() {
	p := periodPickerOverlayState{visible: true}
	if m.period.From != "" {
		p.mode = periodPickerCustom
		p.fromDraft = m.period.From
		p.toDraft = m.period.To
		p.cursor = len(periodPresets)
	} else if idx := indexOfPreset(m.period.Period); idx >= 0 {
		p.cursor = idx
	} else {
		p.cursor = indexOfPreset("7d")
	}
	m.periodPicker = p
}

func (m *model) resetForPeriodChange() {
	m.dailyCursor = 0
	m.models.cursor = 0
	m.tools.cursor = 0
	m.projects.cursor = 0
	m.sessions.page = 1
	m.sessions.cursor = 0
}

func (m *model) applyPeriodPreset(preset string) (tea.Model, tea.Cmd) {
	m.period = stats.PeriodQuery{Period: preset}
	m.periodPicker = periodPickerOverlayState{}
	m.resetForPeriodChange()
	return m, m.loadAllForCurrent()
}

// validateCustomDates mirrors the web handler's range validation so the TUI gives
// fast local feedback; the authoritative window is still computed server-side.
func validateCustomDates(from, to string) (stats.PeriodQuery, string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" {
		return stats.PeriodQuery{}, "from date is required (YYYY-MM-DD)"
	}
	ft, err := time.Parse("2006-01-02", from)
	if err != nil {
		return stats.PeriodQuery{}, "invalid from date: expected YYYY-MM-DD"
	}
	now := time.Now().UTC()
	if ft.After(now) {
		return stats.PeriodQuery{}, "from date cannot be in the future"
	}
	if to != "" {
		tt, err := time.Parse("2006-01-02", to)
		if err != nil {
			return stats.PeriodQuery{}, "invalid to date: expected YYYY-MM-DD"
		}
		if tt.After(now) {
			return stats.PeriodQuery{}, "to date cannot be in the future"
		}
		if ft.After(tt) {
			return stats.PeriodQuery{}, "from must be before or equal to to"
		}
	}
	return stats.PeriodQuery{From: from, To: to}, ""
}

func (m *model) applyPeriodCustom() (tea.Model, tea.Cmd) {
	pq, errMsg := validateCustomDates(m.periodPicker.fromDraft, m.periodPicker.toDraft)
	if errMsg != "" {
		m.periodPicker.err = errMsg
		return m, nil
	}
	m.period = pq
	m.periodPicker = periodPickerOverlayState{}
	m.resetForPeriodChange()
	return m, m.loadAllForCurrent()
}

func (m *model) updatePeriodPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	p := &m.periodPicker

	if p.mode == periodPickerPresets {
		switch {
		case matches(key, m.keys.Close...) || matches(key, m.keys.Period...):
			m.periodPicker = periodPickerOverlayState{}
			return m, nil
		case matches(key, m.keys.Up...):
			p.cursor = clamp(p.cursor-1, 0, len(periodPresets))
			return m, nil
		case matches(key, m.keys.Down...):
			p.cursor = clamp(p.cursor+1, 0, len(periodPresets))
			return m, nil
		case matches(key, m.keys.Top...):
			p.cursor = 0
			return m, nil
		case matches(key, m.keys.Bottom...):
			p.cursor = len(periodPresets)
			return m, nil
		case matches(key, m.keys.Toggle...):
			if p.cursor >= len(periodPresets) {
				p.mode = periodPickerCustom
				p.err = ""
				return m, nil
			}
			return m.applyPeriodPreset(periodPresets[p.cursor])
		}
		return m, nil
	}

	// Custom-range mode.
	switch {
	case matches(key, m.keys.Close...):
		p.mode = periodPickerPresets
		p.err = ""
		return m, nil
	case key == "tab" || key == "shift+tab":
		p.customField = (p.customField + 1) % 2
		return m, nil
	case key == "enter":
		return m.applyPeriodCustom()
	case key == "backspace" || key == "ctrl+h":
		if p.customField == 0 {
			p.fromDraft = trimTrailingRune(p.fromDraft)
		} else {
			p.toDraft = trimTrailingRune(p.toDraft)
		}
		return m, nil
	default:
		if len(msg.Text) > 0 && msg.Text != " " {
			if p.customField == 0 {
				p.fromDraft += msg.Text
			} else {
				p.toDraft += msg.Text
			}
		}
		return m, nil
	}
}

func (m *model) renderPeriodPicker(base string, bodyHeight int) string {
	s := m.styles
	p := m.periodPicker

	lines := []string{s.PanelTitle.Render("Time range"), ""}

	for i, preset := range periodPresets {
		label := periodPresetLabels[preset]
		row := padRight(label, 16) + s.Muted.Render("("+preset+")")
		if p.mode == periodPickerPresets && i == p.cursor {
			lines = append(lines, s.TableRowActive.Render("> "+row))
		} else {
			lines = append(lines, s.TableRow.Render("  "+row))
		}
	}

	// Custom range entry row.
	customRow := "Custom range…"
	if p.mode == periodPickerPresets && p.cursor == len(periodPresets) {
		lines = append(lines, s.TableRowActive.Render("> "+customRow))
	} else {
		lines = append(lines, s.TableRow.Render("  "+customRow))
	}

	if p.mode == periodPickerCustom {
		lines = append(lines, "", s.Muted.Render("Enter dates as YYYY-MM-DD (Tab switches field):"))
		fromLine := "  from " + fieldValue(p.fromDraft)
		toLine := "  to   " + fieldValue(p.toDraft)
		if p.customField == 0 {
			fromLine = s.Accent.Render("> from ") + fieldValue(p.fromDraft)
		} else {
			toLine = s.Accent.Render("> to   ") + fieldValue(p.toDraft)
		}
		lines = append(lines, s.Text.Render(fromLine), s.Text.Render(toLine))
		if p.err != "" {
			lines = append(lines, "", s.Danger.Render(p.err))
		}
		lines = append(lines, "", s.Muted.Render("Enter apply • Tab switch field • Esc back"))
	} else {
		lines = append(lines, "", s.Muted.Render("Enter apply • j/k move • Esc cancel"))
	}

	panelWidth := max(min(m.width-8, 52), 36)
	box := s.OverlayPanel.Width(panelWidth).Render(joinLines(lines...))
	return m.placeOverlay(box, bodyHeight)
}

func fieldValue(v string) string {
	if v == "" {
		return lipgloss.NewStyle().Faint(true).Render("—")
	}
	return v
}
