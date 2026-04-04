package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"opencode-dashboard/internal/stats"
)

func TestNextDailyPeriod(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "1d cycles to 7d", input: "1d", expected: "7d"},
		{name: "7d cycles to 30d", input: "7d", expected: "30d"},
		{name: "30d cycles to 1y", input: "30d", expected: "1y"},
		{name: "1y cycles to all", input: "1y", expected: "all"},
		{name: "all cycles to 1d", input: "all", expected: "1d"},
		{name: "unknown defaults to 7d", input: "14d", expected: "7d"},
		{name: "empty defaults to 7d", input: "", expected: "7d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextDailyPeriod(tt.input)
			if result != tt.expected {
				t.Errorf("nextDailyPeriod(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNextDailyMetric(t *testing.T) {
	tests := []struct {
		name     string
		input    dailyMetric
		expected dailyMetric
	}{
		{name: "cost cycles to sessions", input: dailyMetricCost, expected: dailyMetricSessions},
		{name: "sessions cycles to messages", input: dailyMetricSessions, expected: dailyMetricMessages},
		{name: "messages cycles to tokens", input: dailyMetricMessages, expected: dailyMetricTokens},
		{name: "tokens cycles to cost", input: dailyMetricTokens, expected: dailyMetricCost},
		{name: "unknown defaults to cost", input: dailyMetric("unknown"), expected: dailyMetricCost},
		{name: "empty defaults to cost", input: dailyMetric(""), expected: dailyMetricCost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextDailyMetric(tt.input)
			if result != tt.expected {
				t.Errorf("nextDailyMetric(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNextSessionSort(t *testing.T) {
	tests := []struct {
		name     string
		input    stats.SessionSortMode
		expected stats.SessionSortMode
	}{
		{name: "newest cycles to oldest", input: stats.SessionSortNewest, expected: stats.SessionSortOldest},
		{name: "oldest cycles to cost", input: stats.SessionSortOldest, expected: stats.SessionSortCost},
		{name: "cost cycles to messages", input: stats.SessionSortCost, expected: stats.SessionSortMessages},
		{name: "messages cycles to newest", input: stats.SessionSortMessages, expected: stats.SessionSortNewest},
		{name: "unknown defaults to newest", input: stats.SessionSortMode("unknown"), expected: stats.SessionSortNewest},
		{name: "empty defaults to newest", input: stats.SessionSortMode(""), expected: stats.SessionSortNewest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextSessionSort(tt.input)
			if result != tt.expected {
				t.Errorf("nextSessionSort(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNextModelSort(t *testing.T) {
	tests := []struct {
		name     string
		input    modelSortMode
		expected modelSortMode
	}{
		{name: "cost cycles to messages", input: modelSortCost, expected: modelSortMessages},
		{name: "messages cycles to sessions", input: modelSortMessages, expected: modelSortSessions},
		{name: "sessions cycles to name", input: modelSortSessions, expected: modelSortName},
		{name: "name cycles to cost", input: modelSortName, expected: modelSortCost},
		{name: "unknown defaults to cost", input: modelSortMode("unknown"), expected: modelSortCost},
		{name: "empty defaults to cost", input: modelSortMode(""), expected: modelSortCost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextModelSort(tt.input)
			if result != tt.expected {
				t.Errorf("nextModelSort(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNextToolSort(t *testing.T) {
	tests := []struct {
		name     string
		input    toolSortMode
		expected toolSortMode
	}{
		{name: "runs cycles to errors", input: toolSortRuns, expected: toolSortErrors},
		{name: "errors cycles to success", input: toolSortErrors, expected: toolSortSuccess},
		{name: "success cycles to sessions", input: toolSortSuccess, expected: toolSortSessions},
		{name: "sessions cycles to name", input: toolSortSessions, expected: toolSortName},
		{name: "name cycles to runs", input: toolSortName, expected: toolSortRuns},
		{name: "unknown defaults to runs", input: toolSortMode("unknown"), expected: toolSortRuns},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextToolSort(tt.input)
			if result != tt.expected {
				t.Errorf("nextToolSort(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNextProjectSort(t *testing.T) {
	tests := []struct {
		name     string
		input    projectSortMode
		expected projectSortMode
	}{
		{name: "cost cycles to messages", input: projectSortCost, expected: projectSortMessages},
		{name: "messages cycles to sessions", input: projectSortMessages, expected: projectSortSessions},
		{name: "sessions cycles to name", input: projectSortSessions, expected: projectSortName},
		{name: "name cycles to cost", input: projectSortName, expected: projectSortCost},
		{name: "unknown defaults to cost", input: projectSortMode("unknown"), expected: projectSortCost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextProjectSort(tt.input)
			if result != tt.expected {
				t.Errorf("nextProjectSort(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHasNextSessionPage(t *testing.T) {
	tests := []struct {
		name     string
		list     stats.SessionList
		expected bool
	}{
		{
			name:     "no more pages when total equals current",
			list:     stats.SessionList{Total: 20, Page: 1, PageSize: 20},
			expected: false,
		},
		{
			name:     "has next page",
			list:     stats.SessionList{Total: 100, Page: 1, PageSize: 20},
			expected: true,
		},
		{
			name:     "last page no next",
			list:     stats.SessionList{Total: 100, Page: 5, PageSize: 20},
			expected: false,
		},
		{
			name:     "partial last page no next",
			list:     stats.SessionList{Total: 25, Page: 2, PageSize: 20},
			expected: false,
		},
		{
			name:     "page 2 with more remaining",
			list:     stats.SessionList{Total: 100, Page: 2, PageSize: 20},
			expected: true,
		},
		{
			name:     "zero total",
			list:     stats.SessionList{Total: 0, Page: 1, PageSize: 20},
			expected: false,
		},
		{
			name:     "zero page size returns false",
			list:     stats.SessionList{Total: 100, Page: 1, PageSize: 0},
			expected: false,
		},
		{
			name:     "negative page size returns false",
			list:     stats.SessionList{Total: 100, Page: 1, PageSize: -10},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasNextSessionPage(tt.list)
			if result != tt.expected {
				t.Errorf("hasNextSessionPage(%v) = %v, want %v", tt.list, result, tt.expected)
			}
		})
	}
}

func TestFilterModels(t *testing.T) {
	items := []stats.ModelEntry{
		{ModelID: "gpt-4", ProviderID: "openai", Cost: 10.0},
		{ModelID: "gpt-3.5-turbo", ProviderID: "openai", Cost: 5.0},
		{ModelID: "claude-3", ProviderID: "anthropic", Cost: 8.0},
		{ModelID: "gemini-pro", ProviderID: "google", Cost: 3.0},
	}

	tests := []struct {
		name     string
		filter   string
		expected []stats.ModelEntry
	}{
		{
			name:     "empty filter returns all",
			filter:   "",
			expected: items,
		},
		{
			name:   "filter by model id",
			filter: "gpt",
			expected: []stats.ModelEntry{
				{ModelID: "gpt-4", ProviderID: "openai", Cost: 10.0},
				{ModelID: "gpt-3.5-turbo", ProviderID: "openai", Cost: 5.0},
			},
		},
		{
			name:   "filter by provider",
			filter: "anthropic",
			expected: []stats.ModelEntry{
				{ModelID: "claude-3", ProviderID: "anthropic", Cost: 8.0},
			},
		},
		{
			name:   "case insensitive",
			filter: "GPT",
			expected: []stats.ModelEntry{
				{ModelID: "gpt-4", ProviderID: "openai", Cost: 10.0},
				{ModelID: "gpt-3.5-turbo", ProviderID: "openai", Cost: 5.0},
			},
		},
		{
			name:     "no match returns empty",
			filter:   "unknown",
			expected: []stats.ModelEntry{},
		},
		{
			name:   "whitespace trimmed",
			filter: "  gpt  ",
			expected: []stats.ModelEntry{
				{ModelID: "gpt-4", ProviderID: "openai", Cost: 10.0},
				{ModelID: "gpt-3.5-turbo", ProviderID: "openai", Cost: 5.0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterModels(items, tt.filter)

			if len(result) != len(tt.expected) {
				t.Errorf("filterModels(%q) returned %d items, want %d", tt.filter, len(result), len(tt.expected))
				return
			}

			for i, got := range result {
				want := tt.expected[i]
				if got.ModelID != want.ModelID || got.ProviderID != want.ProviderID {
					t.Errorf("filterModels(%q)[%d] = {ModelID:%q, ProviderID:%q}, want {ModelID:%q, ProviderID:%q}",
						tt.filter, i, got.ModelID, got.ProviderID, want.ModelID, want.ProviderID)
				}
			}
		})
	}
}

func TestFilterTools(t *testing.T) {
	items := []stats.ToolEntry{
		{Name: "bash", Invocations: 100},
		{Name: "read", Invocations: 50},
		{Name: "write", Invocations: 30},
		{Name: "glob", Invocations: 20},
	}

	tests := []struct {
		name     string
		filter   string
		expected []stats.ToolEntry
	}{
		{
			name:     "empty filter returns all",
			filter:   "",
			expected: items,
		},
		{
			name:   "filter by name",
			filter: "read",
			expected: []stats.ToolEntry{
				{Name: "read", Invocations: 50},
			},
		},
		{
			name:   "partial match",
			filter: "w",
			expected: []stats.ToolEntry{
				{Name: "write", Invocations: 30},
			},
		},
		{
			name:     "case insensitive",
			filter:   "BASH",
			expected: []stats.ToolEntry{{Name: "bash", Invocations: 100}},
		},
		{
			name:     "no match",
			filter:   "unknown",
			expected: []stats.ToolEntry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterTools(items, tt.filter)

			if len(result) != len(tt.expected) {
				t.Errorf("filterTools(%q) returned %d items, want %d", tt.filter, len(result), len(tt.expected))
				return
			}

			for i, got := range result {
				want := tt.expected[i]
				if got.Name != want.Name {
					t.Errorf("filterTools(%q)[%d].Name = %q, want %q", tt.filter, i, got.Name, want.Name)
				}
			}
		})
	}
}

func TestFilterProjects(t *testing.T) {
	items := []stats.ProjectEntry{
		{ProjectID: "proj-123", ProjectName: "opencode-dashboard", Cost: 10.0},
		{ProjectID: "proj-456", ProjectName: "my-app", Cost: 5.0},
		{ProjectID: "proj-789", ProjectName: "another-project", Cost: 8.0},
	}

	tests := []struct {
		name     string
		filter   string
		expected []stats.ProjectEntry
	}{
		{
			name:     "empty filter returns all",
			filter:   "",
			expected: items,
		},
		{
			name:   "filter by project name",
			filter: "opencode",
			expected: []stats.ProjectEntry{
				{ProjectID: "proj-123", ProjectName: "opencode-dashboard", Cost: 10.0},
			},
		},
		{
			name:   "filter by project id",
			filter: "proj-456",
			expected: []stats.ProjectEntry{
				{ProjectID: "proj-456", ProjectName: "my-app", Cost: 5.0},
			},
		},
		{
			name:   "partial id match",
			filter: "proj",
			expected: []stats.ProjectEntry{
				{ProjectID: "proj-123", ProjectName: "opencode-dashboard", Cost: 10.0},
				{ProjectID: "proj-456", ProjectName: "my-app", Cost: 5.0},
				{ProjectID: "proj-789", ProjectName: "another-project", Cost: 8.0},
			},
		},
		{
			name:     "no match",
			filter:   "unknown",
			expected: []stats.ProjectEntry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterProjects(items, tt.filter)

			if len(result) != len(tt.expected) {
				t.Errorf("filterProjects(%q) returned %d items, want %d", tt.filter, len(result), len(tt.expected))
				return
			}

			for i, got := range result {
				want := tt.expected[i]
				if got.ProjectID != want.ProjectID || got.ProjectName != want.ProjectName {
					t.Errorf("filterProjects(%q)[%d] = {ProjectID:%q, ProjectName:%q}, want {ProjectID:%q, ProjectName:%q}",
						tt.filter, i, got.ProjectID, got.ProjectName, want.ProjectID, want.ProjectName)
				}
			}
		})
	}
}

func TestTrimTrailingRune(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple trim", input: "hello", expected: "hell"},
		{name: "single char", input: "x", expected: ""},
		{name: "empty string", input: "", expected: ""},
		{name: "unicode char", input: "héllo", expected: "héll"},
		{name: "emoji", input: "hello🌍", expected: "hello"},
		{name: "multi-byte char", input: "test中", expected: "test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimTrailingRune(tt.input)
			if result != tt.expected {
				t.Errorf("trimTrailingRune(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// === Parity Verification Tests ===

func TestTabStripWidthStability(t *testing.T) {
	// Verify TabActive and TabInactive render to same width (underline border fix)
	s := newStyles()

	tabs := []string{"Overview", "Daily", "Models", "Tools", "Projects", "Sessions", "Config"}

	// Measure width of each tab in both states
	for _, label := range tabs {
		activeRender := s.TabActive.Render(label)
		inactiveRender := s.TabInactive.Render(label)

		activeWidth := lipgloss.Width(activeRender)
		inactiveWidth := lipgloss.Width(inactiveRender)

		if activeWidth != inactiveWidth {
			t.Errorf("Tab width mismatch for %q: active=%d, inactive=%d (should be equal for stable layout)", label, activeWidth, inactiveWidth)
		}
	}

	// Verify total strip width remains constant when switching tabs
	var stripWidths []int
	for activeIdx := 0; activeIdx < len(tabs); activeIdx++ {
		renderedTabs := make([]string, len(tabs))
		for i, label := range tabs {
			if i == activeIdx {
				renderedTabs[i] = s.TabActive.Render(label)
			} else {
				renderedTabs[i] = s.TabInactive.Render(label)
			}
		}
		strip := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
		stripWidths = append(stripWidths, lipgloss.Width(strip))
	}

	// All strip widths should be identical
	for i := 1; i < len(stripWidths); i++ {
		if stripWidths[i] != stripWidths[0] {
			t.Errorf("Strip width changed when switching tabs: width[0]=%d, width[%d]=%d (should be constant)", stripWidths[0], i, stripWidths[i])
		}
	}
}

func TestTableWidthThresholdDegradation(t *testing.T) {
	s := newStyles()
	items := []stats.ModelEntry{
		{ModelID: "claude-3", ProviderID: "anthropic", Sessions: 42, Messages: 180, Cost: 12.50},
		{ModelID: "gpt-4", ProviderID: "openai", Sessions: 28, Messages: 120, Cost: 6.25},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	tests := []struct {
		name             string
		width            int
		shouldContain    string
		shouldNotContain string
	}{
		{name: "full columns at 120", width: 120, shouldContain: "AVG$", shouldNotContain: ""},
		{name: "hide avg/msg at 94", width: 94, shouldNotContain: "AVG$"},
		{name: "show share at 110", width: 110, shouldContain: "SHARE"},
		{name: "hide share at 109", width: 109, shouldNotContain: "SHARE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderModels(s, tt.width, 20, items, 2, state)
			if tt.shouldContain != "" && !strings.Contains(result, tt.shouldContain) {
				t.Errorf("renderModels(width=%d) should contain %q, got:\n%s", tt.width, tt.shouldContain, result)
			}
			if tt.shouldNotContain != "" && strings.Contains(result, tt.shouldNotContain) {
				t.Errorf("renderModels(width=%d) should NOT contain %q, got:\n%s", tt.width, tt.shouldNotContain, result)
			}
		})
	}
}

func TestToolsStatusBadgeNoData(t *testing.T) {
	s := newStyles()
	// Tool with 0 invocations should show neutral (--), not FAIL
	items := []stats.ToolEntry{
		{Name: "unused-tool", Invocations: 0, Successes: 0, Failures: 0, Sessions: 0},
		{Name: "good-tool", Invocations: 100, Successes: 98, Failures: 2, Sessions: 5},
	}
	state := tableViewState{cursor: 0, sortLabel: "runs"}

	// Render at width that shows status column (>=130)
	result := renderTools(s, 140, 20, items, 2, state)

	// Unused tool should have neutral indicator (--), not FAIL
	if strings.Contains(result, "FAIL") {
		t.Errorf("Tools with 0 invocations should show neutral (--), not FAIL:\n%s", result)
	}
	if !strings.Contains(result, "--") {
		t.Errorf("Tools with 0 invocations should show neutral (--):\n%s", result)
	}
}

func TestOverviewZeroDataState(t *testing.T) {
	s := newStyles()
	// Empty data should render zero metrics with empty bars, not generic empty state
	data := dashboardData{
		Overview: stats.OverviewStats{Sessions: 0, Messages: 0, Cost: 0},
		Models:   stats.ModelStats{Models: []stats.ModelEntry{}},
		Projects: stats.ProjectStats{Projects: []stats.ProjectEntry{}},
		Tools:    stats.ToolStats{Tools: []stats.ToolEntry{}},
		Sessions: stats.SessionList{Sessions: []stats.SessionEntry{}, Total: 0},
	}

	result := renderOverview(s, 100, 30, data)

	// Should NOT show generic empty state message
	if strings.Contains(result, "No OpenCode activity found") {
		t.Errorf("Overview should render zero metrics, not generic empty state:\n%s", result)
	}

	// Should show zero values
	if !strings.Contains(result, "0") {
		t.Errorf("Overview should show zero values:\n%s", result)
	}
}

func TestSessionDetailCountSummary(t *testing.T) {
	s := newStyles()
	// Session detail must show count summary "U: x | A: y | S: z"
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 5,
		Messages: []stats.SessionMessage{
			{Role: "user"},
			{Role: "assistant"},
			{Role: "user"},
			{Role: "assistant"},
			{Role: "system"},
		},
	}
	state := sessionOverlayState{detail: detail}

	result := renderSessionDetailOverlay(s, 80, 40, state)

	// Must contain count summary format
	if !strings.Contains(result, "U: 2 | A: 2 | S: 1") {
		t.Errorf("Session detail must show count summary 'U: x | A: y | S: z':\n%s", result)
	}
}

func TestSessionDetailCacheTokenPercentage(t *testing.T) {
	s := newStyles()
	// Cache tokens must show percentage when total > 0
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 1,
		TotalTokens: stats.TokenStats{
			Input:  1000,
			Output: 500,
			Cache:  stats.CacheStats{Read: 200, Write: 100},
		},
		Messages: []stats.SessionMessage{
			{Role: "assistant"},
		},
	}
	state := sessionOverlayState{detail: detail}

	result := renderSessionDetailOverlay(s, 80, 40, state)

	// Must show cache percentage (total = 1800, cache read = 200 = 11.1%)
	if !strings.Contains(result, "11.1%") {
		t.Errorf("Session detail must show cache token percentage:\n%s", result)
	}
}

// === Comprehensive Render Tests for Spec Compliance ===

// Tab Strip Tests

func TestTabStripActiveAffordance(t *testing.T) {
	s := newStyles()
	// Active tab must have distinct visual treatment from inactive
	active := s.TabActive.Render("Overview")
	inactive := s.TabInactive.Render("Overview")

	// Active must be visually different (background + bold + border)
	if active == inactive {
		t.Error("Active and inactive tabs must have distinct visual treatment")
	}

	// Active should contain ANSI codes (styling applied)
	if !strings.Contains(active, "\x1b[") {
		t.Error("Active tab must have ANSI styling for visual distinction")
	}

	// Active should have different background color (amber vs bg2)
	// ANSI codes indicate colors are applied
}

func TestTabStrip80ColumnReadability(t *testing.T) {
	s := newStyles()
	// All 7 tab labels must be visible at 80 cols (using short labels)
	width := 80

	// Render all tabs
	renderedTabs := make([]string, len(allTabs))
	for i, tab := range allTabs {
		label := fmt.Sprintf("%d %s", tab.Index, tabLabel(tab, width))
		if i == 0 {
			renderedTabs[i] = s.TabActive.Render(label)
		} else {
			renderedTabs[i] = s.TabInactive.Render(label)
		}
	}

	strip := lipgloss.JoinHorizontal(lipgloss.Left, renderedTabs...)
	stripWidth := lipgloss.Width(strip)

	// Strip should fit within 80 columns
	if stripWidth > width {
		t.Errorf("Tab strip width %d exceeds terminal width %d:\n%s", stripWidth, width, strip)
	}

	// All short labels should be present
	for _, tab := range allTabs {
		shortLabel := tab.ShortLabel
		found := false
		for _, rendered := range renderedTabs {
			if strings.Contains(rendered, shortLabel) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Tab label %q not found in strip at 80 cols", shortLabel)
		}
	}
}

// Models View Tests

func TestModelsViewHighValueColumns(t *testing.T) {
	s := newStyles()
	items := []stats.ModelEntry{
		{ModelID: "claude-3", ProviderID: "anthropic", Sessions: 42, Messages: 180, Cost: 12.50},
		{ModelID: "gpt-4", ProviderID: "openai", Sessions: 28, Messages: 120, Cost: 6.25},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderModels(s, 120, 20, items, 2, state)

	// Must display all high-value columns at sufficient width
	expectedColumns := []string{"MODEL", "PROVIDER", "SESS", "MSG", "COST"}
	for _, col := range expectedColumns {
		if !strings.Contains(result, col) {
			t.Errorf("Models view should contain column %q:\n%s", col, result)
		}
	}
}

func TestModelsViewCostShareBar(t *testing.T) {
	s := newStyles()
	items := []stats.ModelEntry{
		{ModelID: "claude-3", ProviderID: "anthropic", Cost: 12.50}, // 66.67%
		{ModelID: "gpt-4", ProviderID: "openai", Cost: 6.25},        // 33.33%
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderModels(s, 120, 20, items, 2, state)

	// Must show SHARE column with progress bar + percentage
	if !strings.Contains(result, "SHARE") {
		t.Error("Models view should show SHARE column at width 120")
	}

	// Must contain progress bar characters
	if !strings.Contains(result, "█") || !strings.Contains(result, "░") {
		t.Error("Models view cost share should use progress bar characters")
	}

	// Must contain percentage
	if !strings.Contains(result, "%") {
		t.Error("Models view cost share should show percentage")
	}
}

func TestModelsViewLeaderSummary(t *testing.T) {
	s := newStyles()
	items := []stats.ModelEntry{
		{ModelID: "claude-3", Cost: 12.50},
		{ModelID: "gpt-4", Cost: 6.25},
		{ModelID: "gemini-pro", Cost: 2.10},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderModels(s, 100, 20, items, 3, state)

	// Must show leader summary section
	if !strings.Contains(result, "#1") {
		t.Error("Models view should show leader summary with #1 card")
	}
	if !strings.Contains(result, "#2") {
		t.Error("Models view should show leader summary with #2 card")
	}
}

func TestModelsViewSingleModel(t *testing.T) {
	s := newStyles()
	items := []stats.ModelEntry{
		{ModelID: "claude-3", Cost: 10.0, Sessions: 5, Messages: 20},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderModels(s, 100, 20, items, 1, state)

	// Must display single model without error
	if !strings.Contains(result, "claude-3") {
		t.Error("Models view should display single model")
	}

	// Single model should NOT show leader summary (per spec)
	if strings.Contains(result, "#1") {
		t.Error("Models view should NOT show leader summary for single model")
	}
}

func TestModelsViewWidthThresholds(t *testing.T) {
	s := newStyles()
	items := []stats.ModelEntry{
		{ModelID: "claude-3", ProviderID: "anthropic", Cost: 10.0},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	tests := []struct {
		width            int
		shouldContain    string
		shouldNotContain string
	}{
		{width: 120, shouldContain: "AVG$", shouldNotContain: ""},
		{width: 110, shouldContain: "SHARE", shouldNotContain: ""},
		{width: 109, shouldContain: "", shouldNotContain: "SHARE"},
		{width: 94, shouldContain: "", shouldNotContain: "AVG$"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("width_%d", tt.width), func(t *testing.T) {
			result := renderModels(s, tt.width, 20, items, 1, state)
			if tt.shouldContain != "" && !strings.Contains(result, tt.shouldContain) {
				t.Errorf("width %d should contain %q:\n%s", tt.width, tt.shouldContain, result)
			}
			if tt.shouldNotContain != "" && strings.Contains(result, tt.shouldNotContain) {
				t.Errorf("width %d should NOT contain %q:\n%s", tt.width, tt.shouldNotContain, result)
			}
		})
	}
}

// Tools View Tests

func TestToolsViewSuccessRateColumn(t *testing.T) {
	s := newStyles()
	items := []stats.ToolEntry{
		{Name: "bash", Invocations: 100, Successes: 98, Failures: 2, Sessions: 5},
		{Name: "read", Invocations: 50, Successes: 48, Failures: 2, Sessions: 3},
	}
	state := tableViewState{cursor: 0, sortLabel: "runs"}

	result := renderTools(s, 130, 20, items, 2, state)

	// Must show success rate column
	if !strings.Contains(result, "RATE") {
		t.Error("Tools view should show RATE column")
	}

	// Must show percentage format
	if !strings.Contains(result, "98.0%") {
		t.Error("Tools view should show success rate as percentage")
	}
}

func TestToolsViewShareBar(t *testing.T) {
	s := newStyles()
	items := []stats.ToolEntry{
		{Name: "bash", Invocations: 100},
		{Name: "read", Invocations: 50},
	}
	state := tableViewState{cursor: 0, sortLabel: "runs"}

	result := renderTools(s, 130, 20, items, 2, state)

	// Must show SHARE column with progress bar
	if !strings.Contains(result, "SHARE") {
		t.Error("Tools view should show SHARE column")
	}

	// Must contain progress bar characters
	if !strings.Contains(result, "█") || !strings.Contains(result, "░") {
		t.Error("Tools view share should use progress bar characters")
	}
}

func TestToolsViewStatusBadge(t *testing.T) {
	s := newStyles()
	items := []stats.ToolEntry{
		{Name: "perfect-tool", Invocations: 100, Successes: 100, Failures: 0}, // 100% = OK
		{Name: "good-tool", Invocations: 100, Successes: 90, Failures: 10},    // 90% = -- (neutral)
		{Name: "bad-tool", Invocations: 100, Successes: 50, Failures: 50},     // 50% = WARN
		{Name: "unused-tool", Invocations: 0, Successes: 0, Failures: 0},      // no data = --
	}
	state := tableViewState{cursor: 0, sortLabel: "runs"}

	result := renderTools(s, 140, 20, items, 4, state)

	// Must show status badges per spec thresholds
	if !strings.Contains(result, "OK") {
		t.Error("Tools with >95% success should show OK badge")
	}
	if !strings.Contains(result, "WARN") {
		t.Error("Tools with <80% success should show WARN badge")
	}
	if !strings.Contains(result, "--") {
		t.Error("Tools with ≥80-≤95% success or no data should show -- badge")
	}

	// Should not show FAIL (not in spec)
	if strings.Contains(result, "FAIL") {
		t.Error("Tools view should NOT show FAIL badge (not in spec)")
	}
}

func TestToolsViewLeaderSummary(t *testing.T) {
	s := newStyles()
	items := []stats.ToolEntry{
		{Name: "bash", Invocations: 100},
		{Name: "read", Invocations: 50},
		{Name: "write", Invocations: 30},
	}
	state := tableViewState{cursor: 0, sortLabel: "runs"}

	result := renderTools(s, 100, 20, items, 3, state)

	// Must show leader summary for top 2 by invocations
	if !strings.Contains(result, "#1") {
		t.Error("Tools view should show leader summary #1")
	}
	if !strings.Contains(result, "#2") {
		t.Error("Tools view should show leader summary #2")
	}
}

func TestToolsViewWidthThresholds(t *testing.T) {
	s := newStyles()
	items := []stats.ToolEntry{
		{Name: "bash", Invocations: 100, Successes: 90, Failures: 10},
	}
	state := tableViewState{cursor: 0, sortLabel: "runs"}

	tests := []struct {
		width            int
		shouldContain    string
		shouldNotContain string
	}{
		{width: 140, shouldContain: "STATUS", shouldNotContain: ""},
		{width: 130, shouldContain: "STATUS", shouldNotContain: ""},
		{width: 129, shouldContain: "", shouldNotContain: "STATUS"},
		{width: 120, shouldContain: "SHARE", shouldNotContain: ""},
		{width: 119, shouldContain: "", shouldNotContain: "SHARE"},
		{width: 100, shouldContain: "RATE", shouldNotContain: ""},
		{width: 99, shouldContain: "", shouldNotContain: "RATE"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("width_%d", tt.width), func(t *testing.T) {
			result := renderTools(s, tt.width, 20, items, 1, state)
			if tt.shouldContain != "" && !strings.Contains(result, tt.shouldContain) {
				t.Errorf("width %d should contain %q:\n%s", tt.width, tt.shouldContain, result)
			}
			if tt.shouldNotContain != "" && strings.Contains(result, tt.shouldNotContain) {
				t.Errorf("width %d should NOT contain %q:\n%s", tt.width, tt.shouldNotContain, result)
			}
		})
	}
}

// Projects View Tests

func TestProjectsViewTokensColumn(t *testing.T) {
	s := newStyles()
	items := []stats.ProjectEntry{
		{ProjectName: "opencode", Sessions: 5, Messages: 20, Cost: 10.0, Tokens: stats.TokenStats{Input: 1000, Output: 500}},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderProjects(s, 120, 20, items, 1, state)

	// Must show TOK column at sufficient width
	if !strings.Contains(result, "TOK") {
		t.Error("Projects view should show TOK column at width 120")
	}
}

func TestProjectsViewShareColumn(t *testing.T) {
	s := newStyles()
	items := []stats.ProjectEntry{
		{ProjectName: "opencode", Cost: 10.0},
		{ProjectName: "my-app", Cost: 5.0},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderProjects(s, 130, 20, items, 2, state)

	// Must show SHARE column
	if !strings.Contains(result, "SHARE") {
		t.Error("Projects view should show SHARE column")
	}

	// Must contain progress bar
	if !strings.Contains(result, "█") {
		t.Error("Projects view share should use progress bar")
	}
}

func TestProjectsViewAvgSessionColumn(t *testing.T) {
	s := newStyles()
	items := []stats.ProjectEntry{
		{ProjectName: "opencode", Sessions: 10, Cost: 50.0}, // avg $5/session
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderProjects(s, 140, 20, items, 1, state)

	// Must show AVG/S column at sufficient width
	if !strings.Contains(result, "AVG/S") {
		t.Error("Projects view should show AVG/S column at width 140")
	}
}

func TestProjectsViewLeaderSummary(t *testing.T) {
	s := newStyles()
	items := []stats.ProjectEntry{
		{ProjectName: "opencode", Cost: 12.50},
		{ProjectName: "my-app", Cost: 6.25},
		{ProjectName: "other", Cost: 2.10},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	result := renderProjects(s, 100, 20, items, 3, state)

	// Must show leader summary for top 3 by cost
	if !strings.Contains(result, "#1") {
		t.Error("Projects view should show leader summary #1")
	}
	if !strings.Contains(result, "#2") {
		t.Error("Projects view should show leader summary #2")
	}
}

func TestProjectsViewWidthThresholds(t *testing.T) {
	s := newStyles()
	items := []stats.ProjectEntry{
		{ProjectName: "opencode", Cost: 10.0, Sessions: 5},
	}
	state := tableViewState{cursor: 0, sortLabel: "cost"}

	tests := []struct {
		width            int
		shouldContain    string
		shouldNotContain string
	}{
		{width: 140, shouldContain: "AVG/S", shouldNotContain: ""},
		{width: 130, shouldContain: "AVG/S", shouldNotContain: ""},
		{width: 129, shouldContain: "", shouldNotContain: "AVG/S"},
		{width: 120, shouldContain: "SHARE", shouldNotContain: ""},
		{width: 119, shouldContain: "", shouldNotContain: "SHARE"},
		{width: 100, shouldContain: "TOK", shouldNotContain: ""},
		{width: 99, shouldContain: "", shouldNotContain: "TOK"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("width_%d", tt.width), func(t *testing.T) {
			result := renderProjects(s, tt.width, 20, items, 1, state)
			if tt.shouldContain != "" && !strings.Contains(result, tt.shouldContain) {
				t.Errorf("width %d should contain %q:\n%s", tt.width, tt.shouldContain, result)
			}
			if tt.shouldNotContain != "" && strings.Contains(result, tt.shouldNotContain) {
				t.Errorf("width %d should NOT contain %q:\n%s", tt.width, tt.shouldNotContain, result)
			}
		})
	}
}

// Sessions View Tests

func TestSessionsViewCostShareBar(t *testing.T) {
	s := newStyles()
	sessions := []stats.SessionEntry{
		{Title: "session-1", Cost: 10.0, TimeUpdated: time.Now()},
		{Title: "session-2", Cost: 5.0, TimeUpdated: time.Now()},
	}
	list := stats.SessionList{Sessions: sessions, Total: 2, Page: 1, PageSize: 20}
	state := sessionsViewState{cursor: 0, sort: stats.SessionSortNewest}

	result := renderSessions(s, 120, 20, list, state)

	// Must show SHARE column
	if !strings.Contains(result, "SHARE") {
		t.Error("Sessions view should show SHARE column at width 120")
	}

	// Must contain progress bar
	if !strings.Contains(result, "█") {
		t.Error("Sessions view share should use progress bar")
	}
}

func TestSessionsViewPageRelativeShare(t *testing.T) {
	s := newStyles()
	// Share is relative to page total, not global total
	sessions := []stats.SessionEntry{
		{Title: "session-1", Cost: 10.0}, // 66.67% of page
		{Title: "session-2", Cost: 5.0},  // 33.33% of page
	}
	list := stats.SessionList{Sessions: sessions, Total: 100, Page: 1, PageSize: 20}
	state := sessionsViewState{cursor: 0, sort: stats.SessionSortNewest}

	result := renderSessions(s, 120, 20, list, state)

	// Should show share percentages relative to displayed sessions
	// (not to global total of 100)
	if !strings.Contains(result, "%") {
		t.Error("Sessions view should show percentage")
	}
}

func TestSessionsViewWidthThreshold(t *testing.T) {
	s := newStyles()
	sessions := []stats.SessionEntry{
		{Title: "session-1", Cost: 10.0, TimeUpdated: time.Now()},
	}
	list := stats.SessionList{Sessions: sessions, Total: 1}
	state := sessionsViewState{cursor: 0}

	tests := []struct {
		width            int
		shouldContain    string
		shouldNotContain string
	}{
		{width: 120, shouldContain: "SHARE", shouldNotContain: ""},
		{width: 110, shouldContain: "SHARE", shouldNotContain: ""},
		{width: 109, shouldContain: "", shouldNotContain: "SHARE"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("width_%d", tt.width), func(t *testing.T) {
			result := renderSessions(s, tt.width, 20, list, state)
			if tt.shouldContain != "" && !strings.Contains(result, tt.shouldContain) {
				t.Errorf("width %d should contain %q:\n%s", tt.width, tt.shouldContain, result)
			}
			if tt.shouldNotContain != "" && strings.Contains(result, tt.shouldNotContain) {
				t.Errorf("width %d should NOT contain %q:\n%s", tt.width, tt.shouldNotContain, result)
			}
		})
	}
}

// Overview View Tests

func TestOverviewEfficiencyMetrics(t *testing.T) {
	s := newStyles()
	data := dashboardData{
		Overview: stats.OverviewStats{
			Sessions: 10,
			Messages: 100,
			Cost:     10.0,
			Tokens:   stats.TokenStats{Input: 1000, Output: 500},
		},
		Models:   stats.ModelStats{Models: []stats.ModelEntry{{ModelID: "claude-3", Cost: 5.0}}},
		Projects: stats.ProjectStats{Projects: []stats.ProjectEntry{{ProjectName: "test", Cost: 5.0}}},
		Tools:    stats.ToolStats{Tools: []stats.ToolEntry{{Name: "bash", Invocations: 50}}},
		Sessions: stats.SessionList{},
	}

	result := renderOverview(s, 100, 30, data)

	// Must show tokens per session (1500 tokens / 10 sessions = 150)
	if !strings.Contains(result, "tok/sess") {
		t.Error("Overview should show tokens per session")
	}

	// Must show cost per message ($10 / 100 messages = $0.10)
	if !strings.Contains(result, "$0.10/msg") {
		t.Errorf("Overview should show cost per message $0.10/msg:\n%s", result)
	}
}

func TestOverviewTokenBreakdownWithCache(t *testing.T) {
	s := newStyles()
	data := dashboardData{
		Overview: stats.OverviewStats{
			Sessions: 1,
			Tokens: stats.TokenStats{
				Input:     1000,
				Output:    500,
				Reasoning: 200,
				Cache:     stats.CacheStats{Read: 300, Write: 100},
			},
		},
		Models:   stats.ModelStats{},
		Projects: stats.ProjectStats{},
		Tools:    stats.ToolStats{},
		Sessions: stats.SessionList{},
	}

	result := renderOverview(s, 100, 30, data)

	// Must show ALL token categories including cache read/write
	if !strings.Contains(result, "Input") {
		t.Error("Overview token breakdown should show Input")
	}
	if !strings.Contains(result, "Output") {
		t.Error("Overview token breakdown should show Output")
	}
	if !strings.Contains(result, "Reasoning") {
		t.Error("Overview token breakdown should show Reasoning")
	}
	if !strings.Contains(result, "Cache Read") {
		t.Error("Overview token breakdown should show Cache Read as separate category")
	}
	if !strings.Contains(result, "Cache Write") {
		t.Error("Overview token breakdown should show Cache Write as separate category")
	}

	// Must show progress bars for all categories
	if !strings.Contains(result, "█") {
		t.Error("Overview token breakdown should use progress bars")
	}

	// Must show percentages for all categories
	if !strings.Contains(result, "%") {
		t.Error("Overview token breakdown should show percentages")
	}
}

func TestOverviewZeroData(t *testing.T) {
	s := newStyles()
	data := dashboardData{
		Overview: stats.OverviewStats{},
		Models:   stats.ModelStats{},
		Projects: stats.ProjectStats{},
		Tools:    stats.ToolStats{},
		Sessions: stats.SessionList{},
	}

	result := renderOverview(s, 100, 30, data)

	// Must render zero values, not empty state message
	if !strings.Contains(result, "0") {
		t.Error("Overview should show zero values for empty data")
	}

	// Should NOT show "No OpenCode activity" message
	if strings.Contains(result, "No OpenCode activity") {
		t.Error("Overview should NOT show generic empty state message")
	}

	// Should show empty progress bars (░░░░░░░░)
	if !strings.Contains(result, "░") {
		t.Error("Overview should show empty progress bars for zero data")
	}
}

// Session Detail Tests

func TestSessionDetailFactsSummary(t *testing.T) {
	s := newStyles()
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 10,
		TotalCost:    5.0,
		TimeCreated:  time.Now().Add(-1 * time.Hour),
		TimeUpdated:  time.Now(),
		Messages: []stats.SessionMessage{
			{Role: "assistant", ModelID: "claude-3"},
			{Role: "assistant", ModelID: "claude-3"},
			{Role: "user"},
		},
	}
	state := sessionOverlayState{detail: detail}

	result := renderSessionDetailOverlay(s, 80, 40, state)

	// Must show facts summary
	if !strings.Contains(result, "Duration") {
		t.Error("Session detail should show Duration in facts summary")
	}
	if !strings.Contains(result, "Primary model") {
		t.Error("Session detail should show Primary model in facts summary")
	}
}

func TestSessionDetailMessageMix(t *testing.T) {
	s := newStyles()
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 6,
		Messages: []stats.SessionMessage{
			{Role: "user"},
			{Role: "user"},
			{Role: "assistant"},
			{Role: "assistant"},
			{Role: "assistant"},
			{Role: "system"},
		},
	}
	state := sessionOverlayState{detail: detail}

	result := renderSessionDetailOverlay(s, 80, 40, state)

	// Must show message mix section
	if !strings.Contains(result, "Message mix") {
		t.Error("Session detail should show Message mix section")
	}

	// Must show percentages
	if !strings.Contains(result, "%") {
		t.Error("Session detail message mix should show percentages")
	}

	// Must show progress bars
	if !strings.Contains(result, "█") {
		t.Error("Session detail message mix should use progress bars")
	}
}

func TestSessionDetailPeakRow(t *testing.T) {
	s := newStyles()
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 3,
		Messages: []stats.SessionMessage{
			{Role: "user", Tokens: &stats.TokenStats{Input: 100}},
			{Role: "assistant", Tokens: &stats.TokenStats{Input: 500, Output: 300}}, // peak
			{Role: "user", Tokens: &stats.TokenStats{Input: 200}},
		},
	}
	state := sessionOverlayState{detail: detail}

	result := renderSessionDetailOverlay(s, 80, 40, state)

	// Must show peak message identification
	if !strings.Contains(result, "Peak message") {
		t.Error("Session detail should show Peak message section")
	}

	// Must show row number and token count
	if !strings.Contains(result, "Row") {
		t.Error("Session detail peak should show row number")
	}
	if !strings.Contains(result, "800") {
		t.Error("Session detail peak should show token count (500+300=800)")
	}
}

func TestSessionDetailTokenBreakdown(t *testing.T) {
	s := newStyles()
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 1,
		TotalTokens: stats.TokenStats{
			Input:     1000,
			Output:    500,
			Reasoning: 200,
			Cache:     stats.CacheStats{Read: 300, Write: 100},
		},
		Messages: []stats.SessionMessage{{Role: "assistant"}},
	}
	state := sessionOverlayState{detail: detail}

	result := renderSessionDetailOverlay(s, 80, 40, state)

	// Must show token breakdown section
	if !strings.Contains(result, "Token breakdown") {
		t.Error("Session detail should show Token breakdown section")
	}

	// Must show all categories
	if !strings.Contains(result, "Input") {
		t.Error("Session detail token breakdown should show Input")
	}
	if !strings.Contains(result, "Output") {
		t.Error("Session detail token breakdown should show Output")
	}
	if !strings.Contains(result, "Reasoning") {
		t.Error("Session detail token breakdown should show Reasoning")
	}

	// Must show progress bars
	if !strings.Contains(result, "█") {
		t.Error("Session detail token breakdown should use progress bars")
	}
}

func TestSessionDetailHeightConstraint(t *testing.T) {
	s := newStyles()
	detail := &stats.SessionDetail{
		Title:        "Test session",
		MessageCount: 50,
		TotalCost:    10.0,
		Messages:     make([]stats.SessionMessage, 50), // many messages
	}
	for i := range detail.Messages {
		detail.Messages[i] = stats.SessionMessage{Role: "user"}
	}
	state := sessionOverlayState{detail: detail}

	// Limited height (24 rows)
	result := renderSessionDetailOverlay(s, 80, 24, state)

	// Must still render core info
	if !strings.Contains(result, "Session detail") {
		t.Error("Session detail should show title even with limited height")
	}

	// Should not panic or fail
}

func TestMessageDetailOverlayShowsToolParts(t *testing.T) {
	s := newStyles()
	state := messageDetailOverlayState{
		detail: &stats.MessageDetail{
			MessageEntry: stats.MessageEntry{
				ID:           "msg-1",
				SessionID:    "ses-1",
				SessionTitle: "Tooling session",
				Role:         "assistant",
				TimeCreated:  time.Now(),
				Cost:         0.12,
				ModelID:      "claude-3-sonnet",
				ProviderID:   "anthropic",
				Tokens: &stats.TokenStats{
					Input:     100,
					Output:    50,
					Reasoning: 20,
				},
			},
			Content: stats.MessageContent{
				ToolParts: []stats.ToolPart{
					{
						Type:   "tool",
						CallID: "call-1",
						Tool:   "bash",
						State: stats.ToolState{
							Status: "completed",
							Title:  "Run command",
							Input: map[string]interface{}{
								"command": "ls -la",
							},
							Output: "file-a\nfile-b",
						},
					},
					{
						Type:   "tool",
						CallID: "call-2",
						Tool:   "write",
						State: stats.ToolState{
							Status: "error",
							Input: map[string]interface{}{
								"path": "/tmp/out.txt",
							},
							Error: "permission denied",
						},
					},
				},
			},
		},
	}

	result := renderMessageDetailOverlayContent(s, 90, 36, state)

	if !strings.Contains(result, "Tool activity") {
		t.Error("message detail overlay should show Tool activity section")
	}
	if !strings.Contains(result, "bash [COMPLETED]") {
		t.Error("message detail overlay should show completed tool header")
	}
	if !strings.Contains(result, "in: command=ls -la") {
		t.Error("message detail overlay should summarize tool input")
	}
	if !strings.Contains(result, "out: file-a") {
		t.Error("message detail overlay should show tool output summary")
	}
	if !strings.Contains(result, "err: permission denied") {
		t.Error("message detail overlay should show tool error summary")
	}
}

// Leader Summary Helper Tests

func TestRenderLeaderSection(t *testing.T) {
	s := newStyles()
	leaders := []LeaderEntry{
		{Name: "claude-3", Value: 10.0},
		{Name: "gpt-4", Value: 5.0},
		{Name: "gemini", Value: 2.5},
	}

	result := renderLeaderSection(s, 100, leaders, 17.5, "#%d", formatMoney)

	// Must show leader cards
	if result == "" {
		t.Error("Leader section should render for 2+ items")
	}
	if !strings.Contains(result, "#1") {
		t.Error("Leader section should show #1 card")
	}
	if !strings.Contains(result, "#2") {
		t.Error("Leader section should show #2 card")
	}

	// Must show progress bars
	if !strings.Contains(result, "█") {
		t.Error("Leader section should use progress bars")
	}
}

func TestRenderLeaderSectionSingleItem(t *testing.T) {
	s := newStyles()
	leaders := []LeaderEntry{
		{Name: "claude-3", Value: 10.0},
	}

	result := renderLeaderSection(s, 100, leaders, 10.0, "#%d", formatMoney)

	// Must NOT render leader section for single item
	if result != "" {
		t.Error("Leader section should NOT render for single item per spec")
	}
}

func TestRenderLeaderSectionNarrowWidth(t *testing.T) {
	s := newStyles()
	leaders := []LeaderEntry{
		{Name: "claude-3", Value: 10.0},
		{Name: "gpt-4", Value: 5.0},
	}

	// Narrow width (< 70) should stack vertically
	result := renderLeaderSection(s, 60, leaders, 15.0, "#%d", formatMoney)

	// Should still render
	if result == "" {
		t.Error("Leader section should render even at narrow width")
	}
}

func TestSessionOverlayResponsiveSizing(t *testing.T) {
	tests := []struct {
		name         string
		termWidth    int
		bodyHeight   int
		expectedMinW int
		expectedMaxW int
		expectedMinH int
		expectedMaxH int
	}{
		{name: "small terminal", termWidth: 80, bodyHeight: 24, expectedMinW: 48, expectedMaxW: 68, expectedMinH: 12, expectedMaxH: 20},
		{name: "medium terminal", termWidth: 120, bodyHeight: 36, expectedMinW: 102, expectedMaxW: 102, expectedMinH: 30, expectedMaxH: 30},
		{name: "large terminal", termWidth: 160, bodyHeight: 48, expectedMinW: 136, expectedMaxW: 140, expectedMinH: 40, expectedMaxH: 40},
		{name: "very large terminal", termWidth: 200, bodyHeight: 60, expectedMinW: 140, expectedMaxW: 140, expectedMinH: 40, expectedMaxH: 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width := calculateOverlayWidth(tt.termWidth)
			height := calculateOverlayHeight(tt.bodyHeight)

			if width < tt.expectedMinW || width > tt.expectedMaxW {
				t.Errorf("width %d outside expected range [%d, %d]", width, tt.expectedMinW, tt.expectedMaxW)
			}
			if height < tt.expectedMinH || height > tt.expectedMaxH {
				t.Errorf("height %d outside expected range [%d, %d]", height, tt.expectedMinH, tt.expectedMaxH)
			}
		})
	}
}

func TestDailyBarWidthCalculation(t *testing.T) {
	tests := []struct {
		name          string
		width         int
		expectedRange [2]int
	}{
		{name: "wide terminal", width: 120, expectedRange: [2]int{98, 98}},
		{name: "medium terminal", width: 80, expectedRange: [2]int{58, 58}},
		{name: "narrow terminal", width: 40, expectedRange: [2]int{26, 26}},
		{name: "very narrow", width: 25, expectedRange: [2]int{11, 11}},
		{name: "minimal", width: 20, expectedRange: [2]int{8, 8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			barWidth := calculateDailyBarWidth(tt.width)
			if barWidth < tt.expectedRange[0] || barWidth > tt.expectedRange[1] {
				t.Errorf("barWidth %d outside expected range [%d, %d]", barWidth, tt.expectedRange[0], tt.expectedRange[1])
			}
		})
	}
}

func TestSessionDetailMessageRowsScaling(t *testing.T) {
	tests := []struct {
		name        string
		height      int
		lineCount   int
		expectedMin int
		expectedMax int
	}{
		{name: "small height", height: 24, lineCount: 20, expectedMin: 3, expectedMax: 4},
		{name: "medium height", height: 36, lineCount: 20, expectedMin: 6, expectedMax: 13},
		{name: "large height", height: 48, lineCount: 20, expectedMin: 8, expectedMax: 25},
		{name: "very large", height: 60, lineCount: 20, expectedMin: 10, expectedMax: 37},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := calculateMessageRows(tt.height, tt.lineCount)
			if rows < tt.expectedMin || rows > tt.expectedMax {
				t.Errorf("rows %d outside expected range [%d, %d]", rows, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestDailyViewRenderWidthThreshold(t *testing.T) {
	s := newStyles()
	daily := stats.DailyStats{
		Days: []stats.DayStats{
			{Date: "2026-04-01", Cost: 10.0, Sessions: 5, Messages: 50},
			{Date: "2026-04-02", Cost: 15.0, Sessions: 7, Messages: 70},
		},
		Granularity: stats.GranularityDay,
	}

	tests := []struct {
		name  string
		width int
	}{
		{name: "wide", width: 120},
		{name: "medium", width: 80},
		{name: "narrow", width: 60},
		{name: "minimal", width: 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderDaily(s, tt.width, 20, daily, "7d", dailyMetricCost, false, 0)
			if result == "" {
				t.Errorf("width %d: renderDaily should not return empty", tt.width)
			}
			_ = calculateDailyBarWidth(tt.width)
			if !strings.Contains(result, "Daily activity") {
				t.Errorf("width %d: missing header", tt.width)
			}
		})
	}
}

func TestTabRowHasBackground(t *testing.T) {
	s := newStyles()
	rendered := s.TabRow.Render("test")
	if !strings.Contains(rendered, "\x1b[") {
		t.Error("TabRow style must have ANSI styling (background color) to prevent terminal bg leak")
	}
}

func TestAppStyleHeightFillsViewport(t *testing.T) {
	s := newStyles()
	width := 120
	height := 36
	rendered := s.App.Width(width).Height(height).Render("content")
	if lipgloss.Height(rendered) != height {
		t.Errorf("App style with Height(%d) should render %d lines, got %d", height, height, lipgloss.Height(rendered))
	}
	if lipgloss.Width(rendered) != width {
		t.Errorf("App style with Width(%d) should render %d columns, got %d", width, width, lipgloss.Width(rendered))
	}
}

func TestPlaceWhitespaceBackground(t *testing.T) {
	bgStyle := lipgloss.NewStyle().Background(lipgloss.Color("#171A1F"))
	box := lipgloss.NewStyle().Render("centered")
	placed := lipgloss.Place(40, 10, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(bgStyle))
	if lipgloss.Height(placed) != 10 {
		t.Errorf("Place with height 10 should render 10 lines, got %d", lipgloss.Height(placed))
	}
	if lipgloss.Width(placed) != 40 {
		t.Errorf("Place with width 40 should render 40 columns, got %d", lipgloss.Width(placed))
	}
	if !strings.Contains(placed, "\x1b[") {
		t.Error("Place with WithWhitespaceStyle must have ANSI styling for whitespace background")
	}
}

func TestContentAreaFillsWidth(t *testing.T) {
	s := newStyles()
	width := 120
	height := 30
	innerPanel := s.Panel.Width(width - 4).Height(height).Render("content")
	wrapped := s.ContentArea.Width(width).Height(height).Render(innerPanel)
	if lipgloss.Width(wrapped) != width {
		t.Errorf("ContentArea wrapper should fill width %d, got %d", width, lipgloss.Width(wrapped))
	}
	if lipgloss.Height(wrapped) != height {
		t.Errorf("ContentArea wrapper should fill height %d, got %d", height, lipgloss.Height(wrapped))
	}
	if !strings.Contains(wrapped, "\x1b[") {
		t.Error("ContentArea must have ANSI styling (background color) to prevent terminal bg leak")
	}
}

func TestContentAreaCoversPanelWidthGap(t *testing.T) {
	s := newStyles()
	termWidth := 120
	bodyHeight := 30
	panelWidth := max(termWidth-4, 40)

	panel := s.Panel.Width(panelWidth).Height(bodyHeight).Render("inner content")
	wrapped := s.ContentArea.Width(termWidth).Height(bodyHeight).Render(panel)

	panelRenderWidth := lipgloss.Width(panel)
	wrappedWidth := lipgloss.Width(wrapped)

	if panelRenderWidth != panelWidth {
		t.Errorf("Panel should render at width %d, got %d", panelWidth, panelRenderWidth)
	}
	if wrappedWidth != termWidth {
		t.Errorf("ContentArea wrapper should fill terminal width %d, got %d (gap of %d chars)", termWidth, wrappedWidth, termWidth-wrappedWidth)
	}

	gap := termWidth - panelRenderWidth
	if gap != 4 {
		t.Errorf("Expected 4-char gap between Panel and terminal width, got %d", gap)
	}

	if !strings.Contains(wrapped, "\x1b[48;5;") && !strings.Contains(wrapped, "\x1b[4") {
		t.Error("ContentArea wrapper must set background color to fill gap with styled bg1, not terminal default")
	}
}

func TestBorderedStylesPaintOwnBorderBackground(t *testing.T) {
	s := newStyles()
	tests := []struct {
		name     string
		rendered string
	}{
		{name: "Panel", rendered: s.Panel.Render("content")},
		{name: "MetricCard", rendered: s.MetricCard.Render("content")},
		{name: "OverlayPanel", rendered: s.OverlayPanel.Render("content")},
		{name: "HelpPanel", rendered: s.HelpPanel.Render("content")},
		{name: "EmptyState", rendered: s.EmptyState.Render("content")},
		{name: "TabActive", rendered: s.TabActive.Render("Overview")},
		{name: "TabInactive", rendered: s.TabInactive.Render("Overview")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			borderLine := firstBorderLine(tt.rendered)
			if borderLine == "" {
				t.Fatalf("%s should render at least one visible border line", tt.name)
			}

			glyphIndex := strings.IndexFunc(borderLine, isBorderGlyph)
			if glyphIndex == -1 {
				t.Fatalf("%s should render visible border glyphs", tt.name)
			}

			prefix := borderLine[:glyphIndex]
			if !hasExplicitBackgroundANSI(prefix) {
				t.Errorf("%s border glyphs must include explicit background ANSI before the first border rune to avoid terminal background bleed; line=%q", tt.name, borderLine)
			}
		})
	}
}

func firstBorderLine(rendered string) string {
	for _, line := range strings.Split(rendered, "\n") {
		if strings.IndexFunc(line, isBorderGlyph) != -1 {
			return line
		}
	}
	return ""
}

func isBorderGlyph(r rune) bool {
	switch r {
	case '╭', '╮', '╰', '╯', '│', '─', '━', '┌', '┐', '└', '┘', '├', '┤', '┬', '┴', '┼', '║', '═', '╔', '╗', '╚', '╝', '╠', '╣', '╦', '╩', '╬':
		return true
	default:
		return false
	}
}

func hasExplicitBackgroundANSI(segment string) bool {
	return strings.Contains(segment, "[48;") ||
		strings.Contains(segment, ";48;") ||
		strings.Contains(segment, "[40m") ||
		strings.Contains(segment, ";40m") ||
		strings.Contains(segment, "[41m") ||
		strings.Contains(segment, ";41m") ||
		strings.Contains(segment, "[42m") ||
		strings.Contains(segment, ";42m") ||
		strings.Contains(segment, "[43m") ||
		strings.Contains(segment, ";43m") ||
		strings.Contains(segment, "[44m") ||
		strings.Contains(segment, ";44m") ||
		strings.Contains(segment, "[45m") ||
		strings.Contains(segment, ";45m") ||
		strings.Contains(segment, "[46m") ||
		strings.Contains(segment, ";46m") ||
		strings.Contains(segment, "[47m") ||
		strings.Contains(segment, ";47m") ||
		strings.Contains(segment, "[100m") ||
		strings.Contains(segment, ";100m") ||
		strings.Contains(segment, "[101m") ||
		strings.Contains(segment, ";101m") ||
		strings.Contains(segment, "[102m") ||
		strings.Contains(segment, ";102m") ||
		strings.Contains(segment, "[103m") ||
		strings.Contains(segment, ";103m") ||
		strings.Contains(segment, "[104m") ||
		strings.Contains(segment, ";104m") ||
		strings.Contains(segment, "[105m") ||
		strings.Contains(segment, ";105m") ||
		strings.Contains(segment, "[106m") ||
		strings.Contains(segment, ";106m") ||
		strings.Contains(segment, "[107m") ||
		strings.Contains(segment, ";107m")
}

func TestDailyWidthThresholdResponsiveBehavior(t *testing.T) {
	s := newStyles()
	daily := stats.DailyStats{
		Days: []stats.DayStats{
			{Date: "2026-04-01", Cost: 10.0, Sessions: 5, Messages: 50, Tokens: stats.TokenStats{Input: 1000, Output: 500}},
			{Date: "2026-04-02", Cost: 15.0, Sessions: 7, Messages: 70, Tokens: stats.TokenStats{Input: 2000, Output: 1000}},
		},
		Granularity: stats.GranularityDay,
	}

	t.Run("wide_terminal_shows_secondary", func(t *testing.T) {
		result := renderDaily(s, 120, 20, daily, "7d", dailyMetricCost, false, 0)
		if !strings.Contains(result, "sess") {
			t.Error("wide terminal should show secondary 'sess' column")
		}
		if !strings.Contains(result, "messages") {
			t.Error("wide terminal should show full footer with 'messages'")
		}
		if !strings.Contains(result, "Peak") {
			t.Error("wide terminal should show full summary with 'Peak'")
		}
	})

	t.Run("narrow_terminal_hides_secondary", func(t *testing.T) {
		result := renderDaily(s, 50, 20, daily, "7d", dailyMetricCost, false, 0)
		if strings.Contains(result, " sess") {
			t.Error("narrow terminal should NOT show secondary 'sess' column")
		}
		if strings.Contains(result, "messages") {
			t.Error("narrow terminal should NOT show full footer with 'messages'")
		}
	})

	t.Run("very_narrow_compact_summary", func(t *testing.T) {
		result := renderDaily(s, 60, 20, daily, "7d", dailyMetricCost, false, 0)
		if strings.Contains(result, "Peak") {
			t.Error("very narrow terminal should NOT show 'Peak' in summary")
		}
	})
}
