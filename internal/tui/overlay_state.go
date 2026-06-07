package tui

import (
	"context"
	"database/sql"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

var errSourceUnavailable = errors.New("selected source is unavailable")

// sessionOverlayState tracks the session detail overlay lifecycle.
type sessionOverlayState struct {
	visible bool
	id      string
	detail  *stats.SessionDetail
	loading bool
	err     error
}

// projectDetailOverlayState tracks the project drilldown overlay lifecycle.
type projectDetailOverlayState struct {
	visible bool
	id      string
	detail  *stats.ProjectDetail
	loading bool
	err     error
	page    int
	pq      stats.PeriodQuery
	cursor  int
}

// dayMessagesOverlayState tracks the day messages overlay lifecycle.
type dayMessagesOverlayState struct {
	visible  bool
	date     string
	messages stats.MessageList
	cursor   int
	page     int
	loading  bool
	err      error
}

// messageDetailOverlayState tracks the message detail overlay lifecycle.
type messageDetailOverlayState struct {
	visible bool
	id      string
	detail  *stats.MessageDetail
	loading bool
	err     error
}

// configState tracks the config structured browser navigation state.
type configState struct {
	section  int             // -1 = section list view, 0..N = viewing section
	cursor   int             // cursor within current view
	expanded map[string]bool // key path → expanded flag for deep values
}

// Message types for overlay state transitions.

type sessionDetailLoadedMsg struct {
	id     string
	detail *stats.SessionDetail
	err    error
}

type projectDetailLoadedMsg struct {
	id     string
	detail *stats.ProjectDetail
	err    error
}

type dayMessagesLoadedMsg struct {
	date string
	list stats.MessageList
	err  error
}

type messageDetailLoadedMsg struct {
	id     string
	detail *stats.MessageDetail
	err    error
}

// Load commands for overlay data.

func loadSessionDetailCmd(src source.Source, id string) tea.Cmd {
	return func() tea.Msg {
		if src == nil {
			return sessionDetailLoadedMsg{id: id, err: errSourceUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		detail, err := src.SessionByID(ctx, id)
		return sessionDetailLoadedMsg{id: id, detail: detail, err: err}
	}
}

func loadProjectDetailCmd(src source.Source, id string, pq stats.PeriodQuery, page int) tea.Cmd {
	return func() tea.Msg {
		if src == nil {
			return projectDetailLoadedMsg{id: id, err: errSourceUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		detail, err := src.ProjectByID(ctx, id, pq, page, defaultSessionsPageSize)
		return projectDetailLoadedMsg{id: id, detail: detail, err: err}
	}
}

func loadDayMessagesCmd(src source.Source, date string, page int) tea.Cmd {
	return func() tea.Msg {
		if src == nil {
			return dayMessagesLoadedMsg{date: date, err: errSourceUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pq := stats.PeriodQuery{Period: date}
		if len(date) == 10 {
			pq = stats.PeriodQuery{Period: "1d"}
		}
		list, err := src.Messages(ctx, pq, page, 20, stats.DefaultMessageSort())
		if err == nil && len(date) == 10 {
			var filtered []stats.MessageEntry
			for _, msg := range list.Messages {
				if msg.TimeCreated.Format("2006-01-02") == date {
					filtered = append(filtered, msg)
				}
			}
			list.Messages = filtered
			if filtered == nil {
				list.Messages = []stats.MessageEntry{}
			}
		}
		return dayMessagesLoadedMsg{date: date, list: list, err: err}
	}
}

func loadMessageDetailCmd(src source.Source, id string) tea.Cmd {
	return func() tea.Msg {
		if src == nil {
			return messageDetailLoadedMsg{id: id, err: errSourceUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		detail, err := src.MessageByID(ctx, id)
		return messageDetailLoadedMsg{id: id, detail: detail, err: err}
	}
}

// Overlay helpers.

func hasNextMessagePage(list stats.MessageList) bool {
	if list.PageSize <= 0 {
		return false
	}
	return int64(list.Page*list.PageSize) < list.Total
}

func calculateOverlayWidth(termWidth int) int {
	minWidth := 48
	maxWidth := 140
	proportionalWidth := int(float64(termWidth) * 0.85)
	return max(min(proportionalWidth, maxWidth), minWidth)
}

func calculateOverlayHeight(bodyHeight int) int {
	minHeight := 12
	maxHeight := 40
	proportionalHeight := int(float64(bodyHeight) * 0.85)
	return max(min(proportionalHeight, maxHeight), minHeight)
}

func calculateMessageRows(height int, lineCount int) int {
	available := height - lineCount - 3
	minRows := max(3, height/6)
	return max(available, minRows)
}

func (m *model) reconcileSessionOverlay() {
	if !m.sessionDetail.visible || m.sessionDetail.id == "" {
		return
	}
	if _, ok := m.sessionEntryByID(m.sessionDetail.id); !ok && !m.sessionDetail.loading {
		m.sessionDetail.err = sql.ErrNoRows
	}
}

func (m *model) reconcileProjectOverlay() {
	if !m.projectDetail.visible || m.projectDetail.id == "" {
		return
	}
	found := false
	for _, entry := range m.visibleProjectEntries() {
		if entry.ProjectID == m.projectDetail.id {
			found = true
			break
		}
	}
	if !found && !m.projectDetail.loading {
		m.projectDetail.err = errProjectStale
	}
}

// Render overlay helpers.

func (m *model) renderSessionOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderSessionDetailOverlay(m.styles, panelWidth, panelHeight, m.sessionDetail)
	return m.placeOverlay(overlay, bodyHeight)
}

func (m *model) renderProjectOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderProjectDetailOverlay(m.styles, panelWidth, panelHeight, m.projectDetail)
	return m.placeOverlay(overlay, bodyHeight)
}

func (m *model) renderDayMessagesOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderDayMessagesOverlayContent(m.styles, panelWidth, panelHeight, m.dayMessages)
	return m.placeOverlay(overlay, bodyHeight)
}

func (m *model) renderMessageDetailOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderMessageDetailOverlayContent(m.styles, panelWidth, panelHeight, m.messageDetail)
	return m.placeOverlay(overlay, bodyHeight)
}

func (m *model) placeOverlay(overlay string, bodyHeight int) string {
	return lipgloss.Place(max(m.width-4, 40), bodyHeight, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#171A1F"))))
}

// Overlay key handlers.

func (m *model) updateSessionOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if matches(key, m.keys.Close...) {
		m.sessionDetail.visible = false
		m.sessionDetail.loading = false
		m.sessionDetail.err = nil
		m.sessionDetail.detail = nil
		m.sessionDetail.id = ""
		return m, nil
	}
	if matches(key, m.keys.Refresh...) {
		m.sessionDetail.loading = true
		return m, tea.Batch(
			m.loadAllForCurrent(),
			loadSessionDetailCmd(m.src, m.sessionDetail.id),
		)
	}
	return m, nil
}

func (m *model) updateDayMessagesOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	count := len(m.dayMessages.messages.Messages)

	if matches(key, m.keys.Close...) {
		m.dayMessages.visible = false
		m.dayMessages.loading = false
		m.dayMessages.err = nil
		m.dayMessages.messages = stats.MessageList{}
		m.dayMessages.date = ""
		m.dayMessages.cursor = 0
		m.dayMessages.page = 1
		return m, nil
	}
	if matches(key, m.keys.Refresh...) {
		m.dayMessages.loading = true
		m.dayMessages.err = nil
		return m, loadDayMessagesCmd(m.src, m.dayMessages.date, m.dayMessages.page)
	}
	if count > 0 {
		if matches(key, m.keys.Down...) {
			m.dayMessages.cursor = clamp(m.dayMessages.cursor+1, 0, max(count-1, 0))
			return m, nil
		}
		if matches(key, m.keys.Up...) {
			m.dayMessages.cursor = clamp(m.dayMessages.cursor-1, 0, max(count-1, 0))
			return m, nil
		}
		if matches(key, m.keys.Top...) {
			m.dayMessages.cursor = 0
			return m, nil
		}
		if matches(key, m.keys.Bottom...) {
			m.dayMessages.cursor = max(count-1, 0)
			return m, nil
		}
		if matches(key, m.keys.NextPage...) && hasNextMessagePage(m.dayMessages.messages) {
			m.dayMessages.page++
			m.dayMessages.cursor = 0
			m.dayMessages.loading = true
			return m, loadDayMessagesCmd(m.src, m.dayMessages.date, m.dayMessages.page)
		}
		if matches(key, m.keys.PrevPage...) && m.dayMessages.page > 1 {
			m.dayMessages.page--
			m.dayMessages.cursor = 0
			m.dayMessages.loading = true
			return m, loadDayMessagesCmd(m.src, m.dayMessages.date, m.dayMessages.page)
		}
		if matches(key, m.keys.Toggle...) {
			if m.dayMessages.cursor >= 0 && m.dayMessages.cursor < count {
				entry := m.dayMessages.messages.Messages[m.dayMessages.cursor]
				m.messageDetail = messageDetailOverlayState{
					visible: true,
					id:      entry.ID,
					loading: true,
				}
				return m, loadMessageDetailCmd(m.src, entry.ID)
			}
		}
	}
	return m, nil
}

func (m *model) updateMessageDetailOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if matches(key, m.keys.Close...) {
		m.messageDetail.visible = false
		m.messageDetail.loading = false
		m.messageDetail.err = nil
		m.messageDetail.detail = nil
		m.messageDetail.id = ""
		return m, nil
	}
	if matches(key, m.keys.Refresh...) {
		m.messageDetail.loading = true
		m.messageDetail.err = nil
		return m, loadMessageDetailCmd(m.src, m.messageDetail.id)
	}
	return m, nil
}

func (m *model) updateProjectDetailOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if matches(key, m.keys.Close...) {
		m.projectDetail = projectDetailOverlayState{}
		return m, nil
	}

	if matches(key, m.keys.Refresh...) && m.projectDetail.id != "" {
		m.projectDetail.loading = true
		return m, tea.Batch(
			m.loadAllForCurrent(),
			loadProjectDetailCmd(m.src, m.projectDetail.id, m.projectDetail.pq, m.projectDetail.page),
		)
	}

	count := 0
	if m.projectDetail.detail != nil {
		count = len(m.projectDetail.detail.RecentSessions)
	}

	if matches(key, m.keys.Down...) {
		m.projectDetail.cursor = clamp(m.projectDetail.cursor+1, 0, max(count-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Up...) {
		m.projectDetail.cursor = clamp(m.projectDetail.cursor-1, 0, max(count-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Top...) && count > 0 {
		m.projectDetail.cursor = 0
		return m, nil
	}
	if matches(key, m.keys.Bottom...) && count > 0 {
		m.projectDetail.cursor = max(count-1, 0)
		return m, nil
	}

	if matches(key, m.keys.NextPage...) && m.projectDetail.detail != nil {
		totalSessions := m.projectDetail.detail.TotalSessions
		if int64(m.projectDetail.page*defaultSessionsPageSize) < totalSessions {
			m.projectDetail.page++
			m.projectDetail.cursor = 0
			m.projectDetail.loading = true
			return m, loadProjectDetailCmd(m.src, m.projectDetail.id, m.projectDetail.pq, m.projectDetail.page)
		}
	}

	if matches(key, m.keys.PrevPage...) && m.projectDetail.page > 1 {
		m.projectDetail.page--
		m.projectDetail.cursor = 0
		m.projectDetail.loading = true
		return m, loadProjectDetailCmd(m.src, m.projectDetail.id, m.projectDetail.pq, m.projectDetail.page)
	}

	if matches(key, m.keys.Toggle...) && count > 0 && m.projectDetail.cursor >= 0 && m.projectDetail.cursor < count {
		entry := m.projectDetail.detail.RecentSessions[m.projectDetail.cursor]
		m.projectDetail = projectDetailOverlayState{}
		m.sessionDetail = sessionOverlayState{visible: true, id: entry.ID, loading: true}
		return m, loadSessionDetailCmd(m.src, entry.ID)
	}

	return m, nil
}

func (m *model) updateConfigKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if matches(key, m.keys.Close...) {
		if m.config.section >= 0 {
			m.config.section = -1
			return m, nil
		}
		return m, nil
	}

	sectionCount := len(m.configSections())

	if m.config.section == -1 {
		// Section list navigation
		if matches(key, m.keys.Down...) {
			m.config.cursor = clamp(m.config.cursor+1, 0, max(sectionCount-1, 0))
			return m, nil
		}
		if matches(key, m.keys.Up...) {
			m.config.cursor = clamp(m.config.cursor-1, 0, max(sectionCount-1, 0))
			return m, nil
		}
		if matches(key, m.keys.Toggle...) && sectionCount > 0 {
			m.config.section = m.config.cursor
			m.config.cursor = 0
			return m, nil
		}
		return m, nil
	}

	// Section detail navigation
	keys := m.configSectionKeys()
	keyCount := len(keys)

	if matches(key, m.keys.Down...) {
		m.config.cursor = clamp(m.config.cursor+1, 0, max(keyCount-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Up...) {
		m.config.cursor = clamp(m.config.cursor-1, 0, max(keyCount-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Top...) && keyCount > 0 {
		m.config.cursor = 0
		return m, nil
	}
	if matches(key, m.keys.Bottom...) && keyCount > 0 {
		m.config.cursor = max(keyCount-1, 0)
		return m, nil
	}
	if matches(key, m.keys.Toggle...) && keyCount > 0 && m.config.cursor >= 0 && m.config.cursor < keyCount {
		path := keys[m.config.cursor]
		if m.config.expanded[path] {
			delete(m.config.expanded, path)
		} else {
			m.config.expanded[path] = true
		}
		return m, nil
	}
	if matches(key, m.keys.PrevTab...) || matches(key, m.keys.NextTab...) || matches(key, m.keys.Close...) {
		m.config.section = -1
		return m, nil
	}
	return m, nil
}
