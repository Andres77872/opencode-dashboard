package stats

import (
	"strings"
	"testing"
)

func TestSessionOrderBy(t *testing.T) {
	tests := []struct {
		name     string
		sort     SessionSortMode
		expected string
	}{
		{name: "newest", sort: SessionSortNewest, expected: "s.time_created DESC"},
		{name: "oldest", sort: SessionSortOldest, expected: "s.time_created ASC"},
		{name: "cost", sort: SessionSortCost, expected: "total_cost DESC, s.time_created DESC"},
		{name: "messages", sort: SessionSortMessages, expected: "message_count DESC, s.time_created DESC"},
		{name: "empty defaults to newest", sort: SessionSortMode(""), expected: "s.time_created DESC"},
		{name: "unknown defaults to newest", sort: SessionSortMode("invalid"), expected: "s.time_created DESC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sessionOrderBy(tt.sort)

			if result != tt.expected {
				t.Errorf("sessionOrderBy(%q) = %q, want %q", tt.sort, result, tt.expected)
			}
		})
	}
}

// Security test: ensure sessionOrderBy never interpolates user input directly.
// This is critical because the function's output is concatenated into SQL.
func TestSessionOrderBySecurity(t *testing.T) {
	// Attempt to inject SQL via sort mode
	maliciousInputs := []SessionSortMode{
		SessionSortMode("cost; DROP TABLE session;--"),
		SessionSortMode("cost' OR '1'='1"),
		SessionSortMode("cost UNION SELECT * FROM message"),
		SessionSortMode("cost/**/"),
		SessionSortMode("1; DELETE FROM session"),
	}

	for _, input := range maliciousInputs {
		t.Run("security_"+string(input), func(t *testing.T) {
			result := sessionOrderBy(input)

			// All malicious inputs should fall through to default case,
			// producing safe output with NO user input present
			expectedDefault := "s.time_created DESC"

			if result != expectedDefault {
				t.Errorf("sessionOrderBy with malicious input %q returned %q, want safe default %q", input, result, expectedDefault)
			}

			// Additional check: result should NEVER contain the raw input
			if strings.Contains(result, string(input)) {
				t.Errorf("SECURITY ISSUE: sessionOrderBy(%q) output %q contains raw user input - potential SQL injection!", input, result)
			}
		})
	}
}

// Test that sessionOrderBy uses whitelist approach (enum values only)
func TestSessionOrderByWhitelistPattern(t *testing.T) {
	// The function should only produce values from a known whitelist
	whitelist := []string{
		"s.time_created DESC",
		"s.time_created ASC",
		"total_cost DESC, s.time_created DESC",
		"message_count DESC, s.time_created DESC",
	}

	allSortModes := []SessionSortMode{
		SessionSortNewest,
		SessionSortOldest,
		SessionSortCost,
		SessionSortMessages,
		SessionSortMode(""),
		SessionSortMode("random"),
		SessionSortMode("invalid"),
	}

	for _, mode := range allSortModes {
		result := sessionOrderBy(mode)

		inWhitelist := false
		for _, allowed := range whitelist {
			if result == allowed {
				inWhitelist = true
				break
			}
		}

		if !inWhitelist {
			t.Errorf("sessionOrderBy(%q) = %q is NOT in the whitelist of safe ORDER BY clauses", mode, result)
		}
	}
}
