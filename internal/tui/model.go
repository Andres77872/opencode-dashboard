package tui

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
)

const defaultSessionsPageSize = 12

type modelSortMode string

const (
	modelSortCost     modelSortMode = "cost"
	modelSortMessages modelSortMode = "messages"
	modelSortSessions modelSortMode = "sessions"
	modelSortName     modelSortMode = "model"
)

type toolSortMode string

const (
	toolSortRuns     toolSortMode = "runs"
	toolSortErrors   toolSortMode = "errors"
	toolSortSuccess  toolSortMode = "ok"
	toolSortSessions toolSortMode = "sessions"
	toolSortName     toolSortMode = "tool"
)

type projectSortMode string

const (
	projectSortCost     projectSortMode = "cost"
	projectSortMessages projectSortMode = "messages"
	projectSortSessions projectSortMode = "sessions"
	projectSortName     projectSortMode = "project"
)

type dailyMetric string

const (
	dailyMetricCost     dailyMetric = "cost"
	dailyMetricSessions dailyMetric = "sessions"
	dailyMetricMessages dailyMetric = "messages"
	dailyMetricTokens   dailyMetric = "tokens"
)

type dashboardData struct {
	Overview      stats.OverviewStats
	DailyByPeriod map[string]stats.DailyStats
	Models        stats.ModelStats
	Tools         stats.ToolStats
	Projects      stats.ProjectStats
	Sessions      stats.SessionList
	Config        stats.ConfigView
}

type filterState struct {
	cursor      int
	filter      string
	filterDraft string
}

type sessionTableState struct {
	page int
	filterState
	sort stats.SessionSortMode
}

type modelTableState struct {
	filterState
	sort modelSortMode
}

type toolTableState struct {
	filterState
	sort toolSortMode
}

type projectTableState struct {
	filterState
	sort projectSortMode
}

type sessionOverlayState struct {
	visible bool
	id      string
	detail  *stats.SessionDetail
	loading bool
	err     error
}

type dayMessagesOverlayState struct {
	visible  bool
	date     string
	messages stats.MessageList
	cursor   int
	page     int
	loading  bool
	err      error
}

type messageDetailOverlayState struct {
	visible bool
	id      string
	detail  *stats.MessageDetail
	loading bool
	err     error
}

type snapshotLoadedMsg struct {
	data     dashboardData
	loadedAt time.Time
	err      error
}

type sessionsLoadedMsg struct {
	list stats.SessionList
	err  error
}

type sessionDetailLoadedMsg struct {
	id     string
	detail *stats.SessionDetail
	err    error
}

type dailyPeriodLoadedMsg struct {
	period string
	data   stats.DailyStats
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

type model struct {
	store *store.Store
	opts  Options

	styles styles
	keys   keyMap

	width  int
	height int

	activeTab     tabID
	helpVisible   bool
	dailyPeriod   string
	dailyLoading  bool
	dailyMetric   dailyMetric
	dailyCursor   int
	dayMessages   dayMessagesOverlayState
	messageDetail messageDetailOverlayState
	filterMode    bool
	loading       bool
	loaded        bool
	loadErr       error
	lastLoaded    time.Time
	data          dashboardData
	models        modelTableState
	tools         toolTableState
	projects      projectTableState
	sessions      sessionTableState
	sessionDetail sessionOverlayState
}

func newModel(st *store.Store, opts Options) *model {
	return &model{
		store:       st,
		opts:        opts,
		styles:      newStyles(),
		keys:        defaultKeyMap(),
		width:       120,
		height:      36,
		activeTab:   tabOverview,
		dailyPeriod: "7d",
		dailyMetric: dailyMetricCost,
		loading:     true,
		models:      modelTableState{sort: modelSortCost},
		tools:       toolTableState{sort: toolSortRuns},
		projects: projectTableState{
			sort: projectSortCost,
		},
		sessions: sessionTableState{
			page:        1,
			filterState: filterState{cursor: 0},
			sort:        stats.SessionSortNewest,
		},
	}
}

func (m *model) Init() tea.Cmd {
	return loadSnapshotCmd(m.store, m.currentSessionQuery())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 60)
		m.height = max(msg.Height, 18)
		return m, nil

	case snapshotLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.loaded = true
		m.lastLoaded = msg.loadedAt
		m.data = msg.data
		m.reconcileTableCursors()
		m.applyLoadedSessions(msg.data.Sessions)
		m.reconcileSessionOverlay()
		return m, nil

	case sessionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.loaded = true
		m.lastLoaded = time.Now()
		m.data.Sessions = msg.list
		m.applyLoadedSessions(msg.list)
		m.reconcileSessionOverlay()
		return m, nil

	case sessionDetailLoadedMsg:
		if msg.id != m.sessionDetail.id {
			return m, nil
		}
		m.sessionDetail.loading = false
		m.sessionDetail.err = msg.err
		m.sessionDetail.detail = msg.detail
		return m, nil

	case dailyPeriodLoadedMsg:
		m.dailyLoading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		if m.data.DailyByPeriod == nil {
			m.data.DailyByPeriod = make(map[string]stats.DailyStats)
		}
		m.data.DailyByPeriod[msg.period] = msg.data
		return m, nil

	case dayMessagesLoadedMsg:
		if msg.date != m.dayMessages.date {
			return m, nil
		}
		m.dayMessages.loading = false
		m.dayMessages.err = msg.err
		m.dayMessages.messages = msg.list
		if msg.list.Page > 0 {
			m.dayMessages.page = msg.list.Page
		}
		m.dayMessages.cursor = clamp(m.dayMessages.cursor, 0, max(len(msg.list.Messages)-1, 0))
		return m, nil

	case messageDetailLoadedMsg:
		if msg.id != m.messageDetail.id {
			return m, nil
		}
		m.messageDetail.loading = false
		m.messageDetail.err = msg.err
		m.messageDetail.detail = msg.detail
		return m, nil

	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}

	return m, nil
}

func (m *model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if matches(key, m.keys.Quit...) {
		return m, tea.Quit
	}

	if m.messageDetail.visible {
		return m.updateMessageDetailOverlayKey(msg)
	}

	if m.dayMessages.visible {
		return m.updateDayMessagesOverlayKey(msg)
	}

	if m.sessionDetail.visible {
		return m.updateSessionOverlayKey(msg)
	}

	if m.filterMode {
		return m.updateFilterKey(msg)
	}

	if m.helpVisible {
		if matches(key, m.keys.Close...) || matches(key, m.keys.Help...) {
			m.helpVisible = false
		}
		return m, nil
	}

	if matches(key, m.keys.Help...) {
		m.helpVisible = true
		return m, nil
	}

	if matches(key, m.keys.Refresh...) {
		m.loading = true
		m.sessionDetail.err = nil
		m.dayMessages.err = nil
		m.messageDetail.err = nil
		return m, loadSnapshotCmd(m.store, m.currentSessionQuery())
	}

	if tab, ok := tabFromKey(key); ok {
		m.activeTab = tab
		return m, nil
	}

	if matches(key, m.keys.PrevTab...) {
		m.activeTab = previousTab(m.activeTab)
		return m, nil
	}

	if matches(key, m.keys.NextTab...) {
		m.activeTab = nextTab(m.activeTab)
		return m, nil
	}

	if matches(key, m.keys.Close...) {
		m.helpVisible = false
		return m, nil
	}

	switch m.activeTab {
	case tabDaily:
		return m.updateDailyKey(msg)
	case tabModels:
		return m.updateModelsKey(msg)
	case tabTools:
		return m.updateToolsKey(msg)
	case tabProjects:
		return m.updateProjectsKey(msg)
	case tabSessions:
		return m.updateSessionsKey(msg)
	default:
		return m, nil
	}
}

func (m *model) updateModelsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	count := len(m.visibleModelEntries())

	if matches(key, m.keys.Filter...) {
		m.filterMode = true
		m.models.filterDraft = m.models.filter
		return m, nil
	}

	if matches(key, m.keys.Sort...) {
		m.models.sort = nextModelSort(m.models.sort)
		m.models.cursor = 0
		return m, nil
	}

	return m.updateStaticTableCursorKey(key, count, &m.models.cursor)
}

func (m *model) updateToolsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	count := len(m.visibleToolEntries())

	if matches(key, m.keys.Filter...) {
		m.filterMode = true
		m.tools.filterDraft = m.tools.filter
		return m, nil
	}

	if matches(key, m.keys.Sort...) {
		m.tools.sort = nextToolSort(m.tools.sort)
		m.tools.cursor = 0
		return m, nil
	}

	return m.updateStaticTableCursorKey(key, count, &m.tools.cursor)
}

func (m *model) updateProjectsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	count := len(m.visibleProjectEntries())

	if matches(key, m.keys.Filter...) {
		m.filterMode = true
		m.projects.filterDraft = m.projects.filter
		return m, nil
	}

	if matches(key, m.keys.Sort...) {
		m.projects.sort = nextProjectSort(m.projects.sort)
		m.projects.cursor = 0
		return m, nil
	}

	return m.updateStaticTableCursorKey(key, count, &m.projects.cursor)
}

func (m *model) updateStaticTableCursorKey(key string, count int, cursor *int) (tea.Model, tea.Cmd) {
	if matches(key, m.keys.Down...) {
		*cursor = clamp(*cursor+1, 0, max(count-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Up...) {
		*cursor = clamp(*cursor-1, 0, max(count-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Top...) {
		*cursor = 0
		return m, nil
	}
	if matches(key, m.keys.Bottom...) {
		*cursor = max(count-1, 0)
		return m, nil
	}
	return m, nil
}

func (m *model) updateSessionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	count := len(m.data.Sessions.Sessions)

	if matches(key, m.keys.Filter...) {
		m.filterMode = true
		m.sessions.filterDraft = m.sessions.filter
		return m, nil
	}

	if matches(key, m.keys.Sort...) {
		m.sessions.sort = nextSessionSort(m.sessions.sort)
		m.sessions.page = 1
		m.sessions.cursor = 0
		m.loading = true
		return m, loadSessionsCmd(m.store, m.currentSessionQuery())
	}

	if matches(key, m.keys.Down...) {
		m.sessions.cursor = clamp(m.sessions.cursor+1, 0, max(count-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Up...) {
		m.sessions.cursor = clamp(m.sessions.cursor-1, 0, max(count-1, 0))
		return m, nil
	}
	if matches(key, m.keys.Top...) {
		m.sessions.cursor = 0
		return m, nil
	}
	if matches(key, m.keys.Bottom...) {
		m.sessions.cursor = max(count-1, 0)
		return m, nil
	}
	if matches(key, m.keys.NextPage...) && hasNextSessionPage(m.data.Sessions) {
		m.sessions.page++
		m.sessions.cursor = 0
		m.loading = true
		return m, loadSessionsCmd(m.store, m.currentSessionQuery())
	}
	if matches(key, m.keys.PrevPage...) && m.sessions.page > 1 {
		m.sessions.page--
		m.sessions.cursor = 0
		m.loading = true
		return m, loadSessionsCmd(m.store, m.currentSessionQuery())
	}
	if matches(key, m.keys.Toggle...) {
		entry, ok := m.currentSessionEntry()
		if !ok {
			return m, nil
		}
		m.sessionDetail = sessionOverlayState{visible: true, id: entry.ID, loading: true}
		return m, loadSessionDetailCmd(m.store, entry.ID)
	}

	return m, nil
}

func (m *model) updateFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if matches(key, m.keys.Close...) {
		m.filterMode = false
		m.clearActiveFilterDraft()
		return m, nil
	}
	if key == "enter" {
		return m.applyActiveFilter()
	}
	if key == "backspace" || key == "ctrl+h" {
		m.backspaceActiveFilterDraft()
		return m, nil
	}
	if len(msg.Text) > 0 {
		m.appendActiveFilterDraft(msg.Text)
	}
	return m, nil
}

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
		m.loading = true
		m.sessionDetail.loading = true
		return m, tea.Batch(
			loadSnapshotCmd(m.store, m.currentSessionQuery()),
			loadSessionDetailCmd(m.store, m.sessionDetail.id),
		)
	}
	return m, nil
}

func (m *model) updateDailyKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	daily := m.currentDaily()
	dayCount := len(daily.Days)

	if matches(key, m.keys.PrevPage...) {
		nextPeriod := nextDailyPeriod(m.dailyPeriod)
		m.dailyPeriod = nextPeriod
		if m.data.DailyByPeriod == nil || m.data.DailyByPeriod[nextPeriod].Days == nil {
			m.dailyLoading = true
			m.dailyCursor = 0
			return m, loadDailyPeriodCmd(m.store, nextPeriod)
		}
		m.dailyCursor = 0
		return m, nil
	}
	if matches(key, m.keys.Metric...) {
		m.dailyMetric = nextDailyMetric(m.dailyMetric)
		return m, nil
	}
	if dayCount > 0 {
		if matches(key, m.keys.Down...) {
			m.dailyCursor = clamp(m.dailyCursor+1, 0, dayCount-1)
			return m, nil
		}
		if matches(key, m.keys.Up...) {
			m.dailyCursor = clamp(m.dailyCursor-1, 0, dayCount-1)
			return m, nil
		}
		if matches(key, m.keys.Top...) {
			m.dailyCursor = 0
			return m, nil
		}
		if matches(key, m.keys.Bottom...) {
			m.dailyCursor = max(dayCount-1, 0)
			return m, nil
		}
		if matches(key, m.keys.Toggle...) {
			selectedDay := daily.Days[m.dailyCursor]
			m.dayMessages = dayMessagesOverlayState{
				visible: true,
				date:    selectedDay.Date,
				page:    1,
				cursor:  0,
				loading: true,
			}
			return m, loadDayMessagesCmd(m.store, selectedDay.Date, 1)
		}
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
		return m, loadDayMessagesCmd(m.store, m.dayMessages.date, m.dayMessages.page)
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
			return m, loadDayMessagesCmd(m.store, m.dayMessages.date, m.dayMessages.page)
		}
		if matches(key, m.keys.PrevPage...) && m.dayMessages.page > 1 {
			m.dayMessages.page--
			m.dayMessages.cursor = 0
			m.dayMessages.loading = true
			return m, loadDayMessagesCmd(m.store, m.dayMessages.date, m.dayMessages.page)
		}
		if matches(key, m.keys.Toggle...) {
			if m.dayMessages.cursor >= 0 && m.dayMessages.cursor < count {
				entry := m.dayMessages.messages.Messages[m.dayMessages.cursor]
				m.messageDetail = messageDetailOverlayState{
					visible: true,
					id:      entry.ID,
					loading: true,
				}
				return m, loadMessageDetailCmd(m.store, entry.ID)
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
		return m, loadMessageDetailCmd(m.store, m.messageDetail.id)
	}
	return m, nil
}

func (m *model) View() tea.View {
	bodyHeight := max(m.height-6, 10)
	content := m.renderContent(bodyHeight)
	if m.helpVisible {
		content = m.renderHelp(bodyHeight)
	}
	if m.dayMessages.visible {
		content = m.renderDayMessagesOverlay(content, bodyHeight)
	}
	if m.messageDetail.visible {
		content = m.renderMessageDetailOverlay(content, bodyHeight)
	}
	if m.sessionDetail.visible {
		content = m.renderSessionOverlay(content, bodyHeight)
	}

	parts := []string{m.renderStatusBar(), m.renderTabs(), content, m.renderFooter()}
	if m.loading && m.loaded {
		parts = append([]string{m.styles.BannerWarn.Render("WARN stale data visible while refresh is in flight")}, parts...)
	}
	if m.loadErr != nil && m.loaded {
		parts = append([]string{m.styles.BannerError.Render("ERROR " + truncateWithEllipsis(m.loadErr.Error(), m.width-10))}, parts...)
	}

	out := lipgloss.JoinVertical(lipgloss.Left, parts...)
	v := tea.NewView(m.styles.App.Width(m.width).Height(m.height).Render(out))
	v.AltScreen = true
	v.WindowTitle = "opencode-dashboard"
	return v
}

func (m *model) renderStatusBar() string {
	schemaLabel := "schema OK"
	if !m.store.IsValidSchema() {
		schemaLabel = "schema WARN"
	}
	status := lipgloss.JoinHorizontal(
		lipgloss.Left,
		m.styles.StatusTitle.Render("opencode-dashboard"),
		"  ",
		m.styles.StatusMeta.Render(m.opts.Version),
		"  ",
		m.styles.StatusAccent.Render(schemaLabel),
		"  ",
		m.styles.StatusMeta.Render("db: "+truncateWithEllipsis(m.opts.DBPath, max(m.width/3, 24))),
		"  ",
		m.styles.StatusMeta.Render("source: "+truncateWithEllipsis(m.opts.DBSource, max(m.width/5, 16))),
		"  ",
		m.styles.StatusMeta.Render("loaded: "+loadedLabel(m.lastLoaded, m.loaded)),
	)
	return m.styles.StatusBar.Width(m.width).Render(status)
}

func (m *model) renderTabs() string {
	items := make([]string, 0, len(allTabs))
	for _, tab := range allTabs {
		label := fmt.Sprintf("%d %s", tab.Index, tabLabel(tab, m.width))
		if tab.ID == m.activeTab {
			items = append(items, m.styles.TabActive.Render(label))
			continue
		}
		items = append(items, m.styles.TabInactive.Render(label))
	}
	return m.styles.TabRow.Width(m.width).Render(lipgloss.JoinHorizontal(lipgloss.Left, items...))
}

func (m *model) renderContent(bodyHeight int) string {
	if m.loading && !m.loaded {
		return m.styles.EmptyState.Width(max(m.width-4, 40)).Height(bodyHeight).Render("Loading dashboard snapshot…")
	}
	if m.loadErr != nil && !m.loaded {
		return m.styles.EmptyState.Width(max(m.width-4, 40)).Height(bodyHeight).Render("Failed to load dashboard data\n\n" + m.loadErr.Error() + "\n\nPress r to retry.")
	}

	panelWidth := max(m.width-4, 40)
	content := m.renderActiveTab(panelWidth-4, bodyHeight-2)
	panel := m.styles.Panel.Width(panelWidth).Height(bodyHeight).Render(content)
	return m.styles.ContentArea.Width(m.width).Height(bodyHeight).Render(panel)
}

func (m *model) renderActiveTab(width, height int) string {
	switch m.activeTab {
	case tabOverview:
		return renderOverview(m.styles, width, height, m.data)
	case tabDaily:
		return renderDaily(m.styles, width, height, m.currentDaily(), m.dailyPeriod, m.dailyMetric, m.dailyLoading, m.dailyCursor)
	case tabModels:
		return renderModels(m.styles, width, height, m.visibleModelEntries(), len(m.data.Models.Models), tableViewState{
			cursor:      m.models.cursor,
			loading:     m.loading,
			filter:      m.models.filter,
			filterDraft: m.models.filterDraft,
			filterMode:  m.filterMode,
			sortLabel:   renderModelSortLabel(m.models.sort),
		})
	case tabTools:
		return renderTools(m.styles, width, height, m.visibleToolEntries(), len(m.data.Tools.Tools), tableViewState{
			cursor:      m.tools.cursor,
			loading:     m.loading,
			filter:      m.tools.filter,
			filterDraft: m.tools.filterDraft,
			filterMode:  m.filterMode,
			sortLabel:   renderToolSortLabel(m.tools.sort),
		})
	case tabProjects:
		return renderProjects(m.styles, width, height, m.visibleProjectEntries(), len(m.data.Projects.Projects), tableViewState{
			cursor:      m.projects.cursor,
			loading:     m.loading,
			filter:      m.projects.filter,
			filterDraft: m.projects.filterDraft,
			filterMode:  m.filterMode,
			sortLabel:   renderProjectSortLabel(m.projects.sort),
		})
	case tabSessions:
		return renderSessions(m.styles, width, height, m.data.Sessions, sessionsViewState{
			cursor:      m.sessions.cursor,
			loading:     m.loading,
			filter:      m.sessions.filter,
			filterDraft: m.sessions.filterDraft,
			filterMode:  m.filterMode,
			sort:        m.sessions.sort,
		})
	case tabConfig:
		return renderConfig(m.styles, width, height, m.data.Config, m.opts, m.store.Schema())
	default:
		return "unknown tab"
	}
}

func (m *model) renderHelp(bodyHeight int) string {
	help := []string{
		m.styles.PanelTitle.Render("Keyboard help"),
		"",
		m.styles.Text.Render("Global"),
		"  1-7       jump to tab",
		"  h / ←     previous tab",
		"  l / →     next tab",
		"  r         refresh data",
		"  ? / Esc   toggle help",
		"  q / Ctrl+C quit",
		"",
		m.styles.Text.Render("Sessions"),
		"  j/k       move",
		"  g/G       jump top/bottom",
		"  n/p       paginate",
		"  /         filter by title/project",
		"  s         cycle newest/oldest/cost/messages",
		"  Enter     open detail overlay",
		"  Esc       close overlay/filter/help (top-most first)",
		"",
		m.styles.Text.Render("Models / Tools / Projects"),
		"  j/k       move cursor",
		"  g/G       jump top/bottom",
		"  /         filter current table",
		"  s         cycle table sort",
		"",
		m.styles.Text.Render("Daily"),
		"  j/k       move cursor on bars",
		"  g/G       jump top/bottom",
		"  p         cycles 1d/7d/30d/1y/all",
		"  t         cycles cost/sessions/messages/tokens",
		"  Enter     open day messages overlay",
		"",
		m.styles.Text.Render("Day Messages Overlay"),
		"  j/k       move cursor",
		"  n/p       paginate",
		"  Enter     open message detail",
		"  Esc       close overlay",
		"",
		m.styles.Text.Render("Message Detail Overlay"),
		"  Esc       close overlay (returns to day messages)",
		"  r         refresh message content",
	}
	box := m.styles.HelpPanel.Width(max(min(m.width-8, 84), 40)).Render(joinLines(help...))
	return lipgloss.Place(max(m.width-4, 40), bodyHeight, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#171A1F"))))
}

func (m *model) renderFooter() string {
	contextKeys := "1-7 tabs • h/l switch • r refresh • ? help • q quit"
	if m.activeTab == tabDaily {
		periodLabel := m.dailyPeriod
		if m.dailyLoading {
			periodLabel += " (loading)"
		}
		contextKeys += fmt.Sprintf(" • p period:%s • t metric:%s", periodLabel, renderDailyMetricLabel(m.dailyMetric))
	}
	if m.activeTab == tabModels {
		contextKeys += fmt.Sprintf(" • j/k move • / filter • s sort:%s", renderModelSortLabel(m.models.sort))
		if m.models.filter != "" {
			contextKeys += " • filter:" + truncateWithEllipsis(m.models.filter, 18)
		}
	}
	if m.activeTab == tabTools {
		contextKeys += fmt.Sprintf(" • j/k move • / filter • s sort:%s", renderToolSortLabel(m.tools.sort))
		if m.tools.filter != "" {
			contextKeys += " • filter:" + truncateWithEllipsis(m.tools.filter, 18)
		}
	}
	if m.activeTab == tabProjects {
		contextKeys += fmt.Sprintf(" • j/k move • / filter • s sort:%s", renderProjectSortLabel(m.projects.sort))
		if m.projects.filter != "" {
			contextKeys += " • filter:" + truncateWithEllipsis(m.projects.filter, 18)
		}
	}
	if m.activeTab == tabSessions {
		contextKeys += fmt.Sprintf(" • j/k move • n/p pages • / filter • s sort:%s", renderSessionSortLabel(m.sessions.sort))
		if m.sessions.filter != "" {
			contextKeys += " • filter:" + truncateWithEllipsis(m.sessions.filter, 18)
		}
	}
	if m.filterMode {
		contextKeys = "FILTER • type to search current table • Enter apply • Esc cancel"
	}
	if m.messageDetail.visible {
		contextKeys = "MESSAGE DETAIL • Esc close • r refresh"
	}
	if m.dayMessages.visible && !m.messageDetail.visible {
		pageInfo := fmt.Sprintf("page %d", m.dayMessages.page)
		if m.dayMessages.messages.Total > 0 {
			pageInfo = fmt.Sprintf("page %d/%d", m.dayMessages.page, (m.dayMessages.messages.Total/int64(m.dayMessages.messages.PageSize))+1)
		}
		contextKeys = fmt.Sprintf("DAY MESSAGES • %s • j/k move • n/p pages • Enter detail • Esc close", pageInfo)
	}
	if m.sessionDetail.visible && !m.dayMessages.visible && !m.messageDetail.visible {
		contextKeys = "SESSION DETAIL • Esc close • r refresh detail"
	}
	return m.styles.Footer.Width(m.width).Render(contextKeys)
}

func (m *model) renderSessionOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderSessionDetailOverlay(m.styles, panelWidth, panelHeight, m.sessionDetail)
	return lipgloss.Place(max(m.width-4, 40), bodyHeight, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#171A1F"))))
}

func (m *model) renderDayMessagesOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderDayMessagesOverlayContent(m.styles, panelWidth, panelHeight, m.dayMessages)
	return lipgloss.Place(max(m.width-4, 40), bodyHeight, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#171A1F"))))
}

func (m *model) renderMessageDetailOverlay(base string, bodyHeight int) string {
	panelWidth := calculateOverlayWidth(m.width)
	panelHeight := calculateOverlayHeight(bodyHeight)
	overlay := renderMessageDetailOverlayContent(m.styles, panelWidth, panelHeight, m.messageDetail)
	return lipgloss.Place(max(m.width-4, 40), bodyHeight, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#171A1F"))))
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

func (m *model) currentDaily() stats.DailyStats {
	if m.data.DailyByPeriod == nil {
		return stats.DailyStats{}
	}
	if daily, ok := m.data.DailyByPeriod[m.dailyPeriod]; ok {
		return daily
	}
	return stats.DailyStats{}
}

func (m *model) currentSessionQuery() stats.SessionQuery {
	return stats.SessionQuery{
		Page:     m.sessions.page,
		PageSize: defaultSessionsPageSize,
		Filter:   m.sessions.filter,
		Sort:     m.sessions.sort,
	}
}

func (m *model) applyLoadedSessions(list stats.SessionList) {
	m.data.Sessions = list
	if list.Page > 0 {
		m.sessions.page = list.Page
	}
	m.sessions.cursor = clamp(m.sessions.cursor, 0, max(len(list.Sessions)-1, 0))
	if len(list.Sessions) == 0 {
		m.sessions.cursor = 0
	}
}

func (m *model) reconcileTableCursors() {
	m.models.cursor = clamp(m.models.cursor, 0, max(len(m.visibleModelEntries())-1, 0))
	m.tools.cursor = clamp(m.tools.cursor, 0, max(len(m.visibleToolEntries())-1, 0))
	m.projects.cursor = clamp(m.projects.cursor, 0, max(len(m.visibleProjectEntries())-1, 0))
}

func (m *model) reconcileSessionOverlay() {
	if !m.sessionDetail.visible || m.sessionDetail.id == "" {
		return
	}
	if _, ok := m.sessionEntryByID(m.sessionDetail.id); !ok && !m.sessionDetail.loading {
		m.sessionDetail.err = sql.ErrNoRows
	}
}

func (m *model) currentSessionEntry() (stats.SessionEntry, bool) {
	if m.sessions.cursor < 0 || m.sessions.cursor >= len(m.data.Sessions.Sessions) {
		return stats.SessionEntry{}, false
	}
	return m.data.Sessions.Sessions[m.sessions.cursor], true
}

func (m *model) sessionEntryByID(id string) (stats.SessionEntry, bool) {
	for _, session := range m.data.Sessions.Sessions {
		if session.ID == id {
			return session, true
		}
	}
	return stats.SessionEntry{}, false
}

func loadSnapshotCmd(st *store.Store, query stats.SessionQuery) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var data dashboardData
		var err error

		if data.Overview, err = stats.Overview(ctx, st); err != nil {
			return snapshotLoadedMsg{err: err}
		}
		data.DailyByPeriod = make(map[string]stats.DailyStats)
		if daily7, err := stats.Daily(ctx, st, "7d"); err != nil {
			return snapshotLoadedMsg{err: err}
		} else {
			data.DailyByPeriod["7d"] = daily7
		}
		if data.Models, err = stats.Models(ctx, st); err != nil {
			return snapshotLoadedMsg{err: err}
		}
		if data.Tools, err = stats.Tools(ctx, st); err != nil {
			return snapshotLoadedMsg{err: err}
		}
		if data.Projects, err = stats.Projects(ctx, st); err != nil {
			return snapshotLoadedMsg{err: err}
		}
		if data.Sessions, err = stats.SessionsWithQuery(ctx, st, query); err != nil {
			return snapshotLoadedMsg{err: err}
		}
		if data.Config, err = stats.Config(ctx, st); err != nil {
			return snapshotLoadedMsg{err: err}
		}

		return snapshotLoadedMsg{data: data, loadedAt: time.Now()}
	}
}

func loadSessionsCmd(st *store.Store, query stats.SessionQuery) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		list, err := stats.SessionsWithQuery(ctx, st, query)
		return sessionsLoadedMsg{list: list, err: err}
	}
}

func loadSessionDetailCmd(st *store.Store, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		detail, err := stats.SessionByID(ctx, st, id)
		return sessionDetailLoadedMsg{id: id, detail: detail, err: err}
	}
}

func loadDailyPeriodCmd(st *store.Store, period string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		data, err := stats.Daily(ctx, st, period)
		return dailyPeriodLoadedMsg{period: period, data: data, err: err}
	}
}

func loadDayMessagesCmd(st *store.Store, date string, page int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		period := date
		if len(date) == 10 {
			period = "1d"
		}
		list, err := stats.MessagesByPeriod(ctx, st, period, page, 20, stats.DefaultMessageSort())
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

func loadMessageDetailCmd(st *store.Store, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		detail, err := stats.MessageByID(ctx, st, id)
		return messageDetailLoadedMsg{id: id, detail: detail, err: err}
	}
}

func hasNextMessagePage(list stats.MessageList) bool {
	if list.PageSize <= 0 {
		return false
	}
	return int64(list.Page*list.PageSize) < list.Total
}

func nextDailyPeriod(current string) string {
	periods := []string{"1d", "7d", "30d", "1y", "all"}
	for i, p := range periods {
		if p == current {
			nextIdx := (i + 1) % len(periods)
			return periods[nextIdx]
		}
	}
	return "7d"
}

func nextDailyMetric(current dailyMetric) dailyMetric {
	switch current {
	case dailyMetricCost:
		return dailyMetricSessions
	case dailyMetricSessions:
		return dailyMetricMessages
	case dailyMetricMessages:
		return dailyMetricTokens
	default:
		return dailyMetricCost
	}
}

func hasNextSessionPage(list stats.SessionList) bool {
	if list.PageSize <= 0 {
		return false
	}
	return int64(list.Page*list.PageSize) < list.Total
}

func (m *model) clearActiveFilterDraft() {
	switch m.activeTab {
	case tabModels:
		m.models.filterDraft = ""
	case tabTools:
		m.tools.filterDraft = ""
	case tabProjects:
		m.projects.filterDraft = ""
	case tabSessions:
		m.sessions.filterDraft = ""
	}
}

func (m *model) appendActiveFilterDraft(text string) {
	switch m.activeTab {
	case tabModels:
		m.models.filterDraft += text
	case tabTools:
		m.tools.filterDraft += text
	case tabProjects:
		m.projects.filterDraft += text
	case tabSessions:
		m.sessions.filterDraft += text
	}
}

func (m *model) backspaceActiveFilterDraft() {
	switch m.activeTab {
	case tabModels:
		m.models.filterDraft = trimTrailingRune(m.models.filterDraft)
	case tabTools:
		m.tools.filterDraft = trimTrailingRune(m.tools.filterDraft)
	case tabProjects:
		m.projects.filterDraft = trimTrailingRune(m.projects.filterDraft)
	case tabSessions:
		m.sessions.filterDraft = trimTrailingRune(m.sessions.filterDraft)
	}
}

func (m *model) applyActiveFilter() (tea.Model, tea.Cmd) {
	m.filterMode = false

	switch m.activeTab {
	case tabModels:
		trimmed := strings.TrimSpace(m.models.filterDraft)
		m.models.filterDraft = ""
		if trimmed != m.models.filter {
			m.models.filter = trimmed
			m.models.cursor = 0
		}
		m.reconcileTableCursors()
		return m, nil
	case tabTools:
		trimmed := strings.TrimSpace(m.tools.filterDraft)
		m.tools.filterDraft = ""
		if trimmed != m.tools.filter {
			m.tools.filter = trimmed
			m.tools.cursor = 0
		}
		m.reconcileTableCursors()
		return m, nil
	case tabProjects:
		trimmed := strings.TrimSpace(m.projects.filterDraft)
		m.projects.filterDraft = ""
		if trimmed != m.projects.filter {
			m.projects.filter = trimmed
			m.projects.cursor = 0
		}
		m.reconcileTableCursors()
		return m, nil
	case tabSessions:
		trimmed := strings.TrimSpace(m.sessions.filterDraft)
		changed := trimmed != m.sessions.filter
		m.sessions.filter = trimmed
		m.sessions.filterDraft = ""
		if changed {
			m.sessions.page = 1
			m.sessions.cursor = 0
			m.loading = true
			return m, loadSessionsCmd(m.store, m.currentSessionQuery())
		}
		return m, nil
	default:
		return m, nil
	}
}

func trimTrailingRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func (m *model) visibleModelEntries() []stats.ModelEntry {
	items := filterModels(m.data.Models.Models, m.models.filter)
	sortModelEntries(items, m.models.sort)
	return items
}

func (m *model) visibleToolEntries() []stats.ToolEntry {
	items := filterTools(m.data.Tools.Tools, m.tools.filter)
	sortToolEntries(items, m.tools.sort)
	return items
}

func (m *model) visibleProjectEntries() []stats.ProjectEntry {
	items := filterProjects(m.data.Projects.Projects, m.projects.filter)
	sortProjectEntries(items, m.projects.sort)
	return items
}

func filterModels(items []stats.ModelEntry, filter string) []stats.ModelEntry {
	needle := strings.ToLower(strings.TrimSpace(filter))
	filtered := make([]stats.ModelEntry, 0, len(items))
	for _, item := range items {
		if needle == "" || strings.Contains(strings.ToLower(item.ModelID), needle) || strings.Contains(strings.ToLower(item.ProviderID), needle) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterTools(items []stats.ToolEntry, filter string) []stats.ToolEntry {
	needle := strings.ToLower(strings.TrimSpace(filter))
	filtered := make([]stats.ToolEntry, 0, len(items))
	for _, item := range items {
		if needle == "" || strings.Contains(strings.ToLower(item.Name), needle) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterProjects(items []stats.ProjectEntry, filter string) []stats.ProjectEntry {
	needle := strings.ToLower(strings.TrimSpace(filter))
	filtered := make([]stats.ProjectEntry, 0, len(items))
	for _, item := range items {
		if needle == "" || strings.Contains(strings.ToLower(item.ProjectName), needle) || strings.Contains(strings.ToLower(item.ProjectID), needle) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func sortModelEntries(items []stats.ModelEntry, mode modelSortMode) {
	sort.Slice(items, func(i, j int) bool {
		switch mode {
		case modelSortMessages:
			if items[i].Messages != items[j].Messages {
				return items[i].Messages > items[j].Messages
			}
		case modelSortSessions:
			if items[i].Sessions != items[j].Sessions {
				return items[i].Sessions > items[j].Sessions
			}
		case modelSortName:
			if items[i].ModelID != items[j].ModelID {
				return items[i].ModelID < items[j].ModelID
			}
		default:
			if items[i].Cost != items[j].Cost {
				return items[i].Cost > items[j].Cost
			}
		}
		if items[i].Cost != items[j].Cost {
			return items[i].Cost > items[j].Cost
		}
		if items[i].Messages != items[j].Messages {
			return items[i].Messages > items[j].Messages
		}
		return items[i].ModelID < items[j].ModelID
	})
}

func sortToolEntries(items []stats.ToolEntry, mode toolSortMode) {
	sort.Slice(items, func(i, j int) bool {
		switch mode {
		case toolSortErrors:
			if items[i].Failures != items[j].Failures {
				return items[i].Failures > items[j].Failures
			}
		case toolSortSuccess:
			if items[i].Successes != items[j].Successes {
				return items[i].Successes > items[j].Successes
			}
		case toolSortSessions:
			if items[i].Sessions != items[j].Sessions {
				return items[i].Sessions > items[j].Sessions
			}
		case toolSortName:
			if items[i].Name != items[j].Name {
				return items[i].Name < items[j].Name
			}
		default:
			if items[i].Invocations != items[j].Invocations {
				return items[i].Invocations > items[j].Invocations
			}
		}
		if items[i].Invocations != items[j].Invocations {
			return items[i].Invocations > items[j].Invocations
		}
		return items[i].Name < items[j].Name
	})
}

func sortProjectEntries(items []stats.ProjectEntry, mode projectSortMode) {
	sort.Slice(items, func(i, j int) bool {
		switch mode {
		case projectSortMessages:
			if items[i].Messages != items[j].Messages {
				return items[i].Messages > items[j].Messages
			}
		case projectSortSessions:
			if items[i].Sessions != items[j].Sessions {
				return items[i].Sessions > items[j].Sessions
			}
		case projectSortName:
			if items[i].ProjectName != items[j].ProjectName {
				return items[i].ProjectName < items[j].ProjectName
			}
		default:
			if items[i].Cost != items[j].Cost {
				return items[i].Cost > items[j].Cost
			}
		}
		if items[i].Cost != items[j].Cost {
			return items[i].Cost > items[j].Cost
		}
		return items[i].ProjectName < items[j].ProjectName
	})
}

func nextModelSort(current modelSortMode) modelSortMode {
	switch current {
	case modelSortCost:
		return modelSortMessages
	case modelSortMessages:
		return modelSortSessions
	case modelSortSessions:
		return modelSortName
	default:
		return modelSortCost
	}
}

func nextToolSort(current toolSortMode) toolSortMode {
	switch current {
	case toolSortRuns:
		return toolSortErrors
	case toolSortErrors:
		return toolSortSuccess
	case toolSortSuccess:
		return toolSortSessions
	case toolSortSessions:
		return toolSortName
	default:
		return toolSortRuns
	}
}

func nextProjectSort(current projectSortMode) projectSortMode {
	switch current {
	case projectSortCost:
		return projectSortMessages
	case projectSortMessages:
		return projectSortSessions
	case projectSortSessions:
		return projectSortName
	default:
		return projectSortCost
	}
}

func renderModelSortLabel(mode modelSortMode) string {
	return string(mode)
}

func renderToolSortLabel(mode toolSortMode) string {
	return string(mode)
}

func renderProjectSortLabel(mode projectSortMode) string {
	return string(mode)
}

func nextSessionSort(current stats.SessionSortMode) stats.SessionSortMode {
	switch current {
	case stats.SessionSortNewest:
		return stats.SessionSortOldest
	case stats.SessionSortOldest:
		return stats.SessionSortCost
	case stats.SessionSortCost:
		return stats.SessionSortMessages
	default:
		return stats.SessionSortNewest
	}
}
