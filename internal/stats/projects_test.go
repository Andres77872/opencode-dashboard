package stats

import (
	"testing"
)

func TestResolveProjectName(t *testing.T) {
	tests := []struct {
		name        string
		projectID   string
		displayName string
		worktree    string
		expected    string
	}{
		{
			name:        "name takes priority",
			projectID:   "abc123def456",
			displayName: "my-project",
			worktree:    "/path/to/worktree",
			expected:    "my-project",
		},
		{
			name:        "empty name uses worktree base",
			projectID:   "abc123def456",
			displayName: "",
			worktree:    "/home/user/projects/my-app",
			expected:    "my-app",
		},
		{
			name:        "empty name and worktree uses truncated projectID",
			projectID:   "abc123def456789",
			displayName: "",
			worktree:    "",
			expected:    "abc123de",
		},
		{
			name:        "short projectID preserved",
			projectID:   "abc",
			displayName: "",
			worktree:    "",
			expected:    "abc",
		},
		{
			name:        "exactly 8 char projectID preserved",
			projectID:   "abcd1234",
			displayName: "",
			worktree:    "",
			expected:    "abcd1234",
		},
		{
			name:        "worktree with nested path",
			projectID:   "xyz789",
			displayName: "",
			worktree:    "/home/user/dev/projects/nested/deep-project",
			expected:    "deep-project",
		},
		{
			name:        "worktree with trailing slash",
			projectID:   "id123",
			displayName: "",
			worktree:    "/path/to/project/",
			expected:    "project", // filepath.Base handles trailing slash
		},
		{
			name:        "name with spaces",
			projectID:   "abc123",
			displayName: "My Project Name",
			worktree:    "/path",
			expected:    "My Project Name",
		},
		{
			name:        "empty strings all around uses projectID if short",
			projectID:   "short",
			displayName: "",
			worktree:    "",
			expected:    "short",
		},
		{
			name:        "worktree is root directory",
			projectID:   "project1",
			displayName: "",
			worktree:    "/",
			expected:    "/", // filepath.Base("/") returns "/"
		},
		{
			name:        "worktree is relative path",
			projectID:   "abc123",
			displayName: "",
			worktree:    "./my-project",
			expected:    "my-project",
		},
		{
			name:        "worktree is single directory",
			projectID:   "xyz",
			displayName: "",
			worktree:    "project",
			expected:    "project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveProjectName(tt.projectID, tt.displayName, tt.worktree)
			if result != tt.expected {
				t.Errorf("resolveProjectName(%q, %q, %q) = %q, want %q",
					tt.projectID, tt.displayName, tt.worktree, result, tt.expected)
			}
		})
	}
}

func TestResolveProjectName_Priority(t *testing.T) {
	// Explicit test of the priority order: name > worktree > truncated projectID
	t.Run("name wins over worktree", func(t *testing.T) {
		result := resolveProjectName("abc123def456", "explicit-name", "/path/to/other")
		if result != "explicit-name" {
			t.Errorf("name should win over worktree, got %q", result)
		}
	})

	t.Run("name wins over projectID truncation", func(t *testing.T) {
		result := resolveProjectName("verylongprojectid123", "explicit-name", "")
		if result != "explicit-name" {
			t.Errorf("name should win over projectID truncation, got %q", result)
		}
	})

	t.Run("worktree wins when name empty", func(t *testing.T) {
		result := resolveProjectName("verylongprojectid123", "", "/path/to/worktree-name")
		if result != "worktree-name" {
			t.Errorf("worktree should win when name empty, got %q", result)
		}
	})

	t.Run("projectID truncation when all empty", func(t *testing.T) {
		result := resolveProjectName("abcdefgh12345678", "", "")
		if result != "abcdefgh" {
			t.Errorf("projectID should truncate to 8 chars when all empty, got %q", result)
		}
	})
}

func TestResolveProjectName_TruncationEdgeCases(t *testing.T) {
	tests := []struct {
		projectID string
		expected  string
		desc      string
	}{
		{"", "", "empty projectID"},
		{"a", "a", "single char"},
		{"ab", "ab", "two chars"},
		{"abc", "abc", "three chars"},
		{"abcd", "abcd", "four chars"},
		{"abcde", "abcde", "five chars"},
		{"abcdef", "abcdef", "six chars"},
		{"abcdefg", "abcdefg", "seven chars"},
		{"abcdefgh", "abcdefgh", "exactly eight chars"},
		{"abcdefghi", "abcdefgh", "nine chars truncated"},
		{"abcdefghij123456", "abcdefgh", "long projectID truncated"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := resolveProjectName(tt.projectID, "", "")
			if result != tt.expected {
				t.Errorf("resolveProjectName(%q, \"\", \"\") = %q, want %q",
					tt.projectID, result, tt.expected)
			}
		})
	}
}
