package tui

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestToggleDailyPeriod(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "7d toggles to 30d", input: "7d", expected: "30d"},
		{name: "30d toggles to 7d", input: "30d", expected: "7d"},
		{name: "unknown defaults to 7d", input: "14d", expected: "7d"},
		{name: "empty defaults to 7d", input: "", expected: "7d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toggleDailyPeriod(tt.input)
			if result != tt.expected {
				t.Errorf("toggleDailyPeriod(%q) = %q, want %q", tt.input, result, tt.expected)
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
