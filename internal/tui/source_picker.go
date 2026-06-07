package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"opencode-dashboard/internal/source"
)

// sourcePickerOverlayState drives the runtime source switcher overlay.
type sourcePickerOverlayState struct {
	visible bool
	cursor  int
	sources []source.SourceInfo
	err     string
}

func sourceUnavailableReason(info source.SourceInfo) string {
	if info.Diagnostics.Reason != "" {
		return info.Diagnostics.Reason
	}
	return "not configured"
}

func (m *model) openSourcePicker() {
	infos := m.registry.List(context.Background())
	cursor := 0
	for i, info := range infos {
		if info.ID == m.selectedSource {
			cursor = i
			break
		}
	}
	m.sourcePicker = sourcePickerOverlayState{visible: true, sources: infos, cursor: cursor}
}

// switchSource swaps the active source and hard-resets all view/overlay state so
// no stale data from the previous source survives, then refetches everything.
func (m *model) switchSource(id source.SourceID) (tea.Model, tea.Cmd) {
	m.selectedSource = id
	m.resolveSource()

	m.data = dashboardData{}
	m.dailyCursor = 0
	m.models = modelTableState{sort: m.models.sort}
	m.tools = toolTableState{sort: m.tools.sort}
	m.projects = projectTableState{sort: m.projects.sort}
	m.sessions = sessionTableState{page: 1, sort: m.sessions.sort}
	m.sessionDetail = sessionOverlayState{}
	m.projectDetail = projectDetailOverlayState{}
	m.dayMessages = dayMessagesOverlayState{}
	m.messageDetail = messageDetailOverlayState{}
	m.config = configState{section: -1, expanded: make(map[string]bool)}
	m.filterMode = false
	m.loaded = false
	m.loadErr = nil
	m.sourcePicker = sourcePickerOverlayState{}

	return m, m.loadAllForCurrent()
}

func (m *model) updateSourcePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	p := &m.sourcePicker
	count := len(p.sources)

	switch {
	case matches(key, m.keys.Close...) || matches(key, m.keys.Source...):
		m.sourcePicker = sourcePickerOverlayState{}
		return m, nil
	case matches(key, m.keys.Up...):
		p.cursor = clamp(p.cursor-1, 0, max(count-1, 0))
		p.err = ""
		return m, nil
	case matches(key, m.keys.Down...):
		p.cursor = clamp(p.cursor+1, 0, max(count-1, 0))
		p.err = ""
		return m, nil
	case matches(key, m.keys.Toggle...):
		if count == 0 || p.cursor < 0 || p.cursor >= count {
			return m, nil
		}
		info := p.sources[p.cursor]
		if !info.Available {
			p.err = "unavailable: " + sourceUnavailableReason(info)
			return m, nil
		}
		if info.ID == m.selectedSource {
			m.sourcePicker = sourcePickerOverlayState{}
			return m, nil
		}
		return m.switchSource(info.ID)
	}
	return m, nil
}

func (m *model) renderSourcePicker(base string, bodyHeight int) string {
	s := m.styles
	p := m.sourcePicker

	lines := []string{s.PanelTitle.Render("Data source"), ""}
	for i, info := range p.sources {
		marker := s.Success.Render("●")
		label := sourceLabelOrID(info, info.ID)
		suffix := ""
		if !info.Available {
			marker = s.Muted.Render("○")
			suffix = s.Muted.Render("  unavailable")
		} else if info.ID == m.selectedSource {
			suffix = s.Muted.Render("  ← active")
		}
		row := marker + " " + padRight(label, 16) + suffix
		if i == p.cursor {
			lines = append(lines, s.TableRowActive.Render("> "+row))
		} else {
			lines = append(lines, s.TableRow.Render("  "+row))
		}
	}
	if len(p.sources) == 0 {
		lines = append(lines, s.Muted.Render("  No sources registered"))
	}
	if p.err != "" {
		lines = append(lines, "", s.Danger.Render(p.err))
	}
	lines = append(lines, "", s.Muted.Render("Enter select • j/k move • Esc cancel"))

	panelWidth := max(min(m.width-8, 52), 36)
	box := s.OverlayPanel.Width(panelWidth).Render(joinLines(lines...))
	return m.placeOverlay(box, bodyHeight)
}
