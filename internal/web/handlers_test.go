package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseIntQuery(t *testing.T) {
	tests := []struct {
		name       string
		queryKey   string
		queryValue string
		defaultVal int
		expected   int
	}{
		{name: "empty query", queryKey: "page", queryValue: "", defaultVal: 1, expected: 1},
		{name: "valid number", queryKey: "page", queryValue: "5", defaultVal: 1, expected: 5},
		{name: "zero returns default", queryKey: "page", queryValue: "0", defaultVal: 1, expected: 1},
		{name: "negative returns default", queryKey: "page", queryValue: "-5", defaultVal: 1, expected: 1},
		{name: "non-numeric returns default", queryKey: "page", queryValue: "abc", defaultVal: 1, expected: 1},
		{name: "large number", queryKey: "limit", queryValue: "100", defaultVal: 20, expected: 100},
		{name: "URL encoded spaces", queryKey: "page", queryValue: "%205%20", defaultVal: 1, expected: 1},
		{name: "float fails", queryKey: "page", queryValue: "5.5", defaultVal: 1, expected: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawURL := "/?" + tt.queryKey + "=" + tt.queryValue
			req := httptest.NewRequest(http.MethodGet, rawURL, nil)
			result := parseIntQuery(req, tt.queryKey, tt.defaultVal)

			if result != tt.expected {
				t.Errorf("parseIntQuery(%q=%q, default=%d) = %d, want %d", tt.queryKey, tt.queryValue, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

func TestParseIntQueryMissingKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?other=value", nil)
	result := parseIntQuery(req, "page", 1)

	if result != 1 {
		t.Errorf("parseIntQuery with missing key = %d, want 1", result)
	}
}

func TestExtractSessionID(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{name: "valid simple id", path: "/api/v1/sessions/abc123", expected: "abc123"},
		{name: "valid uuid", path: "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440000", expected: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "valid with trailing slash", path: "/api/v1/sessions/abc123/", expected: "abc123"},
		{name: "missing prefix", path: "/sessions/abc123", expected: ""},
		{name: "wrong prefix", path: "/api/v2/sessions/abc123", expected: ""},
		{name: "empty id", path: "/api/v1/sessions/", expected: ""},
		{name: "no id segment", path: "/api/v1/sessions", expected: ""},
		{name: "id with slashes", path: "/api/v1/sessions/abc/123", expected: ""},
		{name: "nested path", path: "/api/v1/sessions/abc123/messages", expected: ""},
		{name: "empty path", path: "", expected: ""},
		{name: "root path", path: "/", expected: ""},
		{name: "complex id", path: "/api/v1/sessions/ses_abc-xyz_123", expected: "ses_abc-xyz_123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSessionID(tt.path)

			if result != tt.expected {
				t.Errorf("extractSessionID(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

// Security test: ensure extractSessionID rejects paths with slashes (path traversal)
func TestExtractSessionIDSecurity(t *testing.T) {
	// Paths with slashes after the session ID segment should be rejected
	// This prevents path traversal and nested resource access
	slashPaths := []string{
		"/api/v1/sessions/../config",
		"/api/v1/sessions/../../../etc/passwd",
		"/api/v1/sessions/abc/123",
		"/api/v1/sessions/abc123/messages",
	}

	for _, path := range slashPaths {
		t.Run("slash_reject_"+path, func(t *testing.T) {
			result := extractSessionID(path)

			// Paths with "/" after sessions/ should return empty
			if result != "" {
				t.Errorf("extractSessionID(%q) = %q, want empty for path with slashes", path, result)
			}
		})
	}

	// These paths DON'T have slashes after the ID, so they ARE accepted
	// Note: URL-encoded characters are NOT decoded by extractSessionID
	// They are passed through as-is (router-level concern)
	acceptedPaths := []struct {
		path     string
		expected string
	}{
		{"/api/v1/sessions/..%2F..%2Fconfig", "..%2F..%2Fconfig"},                 // URL-encoded slash NOT decoded
		{"/api/v1/sessions/%00", "%00"},                                           // URL-encoded null NOT decoded
		{"/api/v1/sessions/abc; DROP TABLE session;", "abc; DROP TABLE session;"}, // SQL in ID - DB sanitizes
	}

	for _, tc := range acceptedPaths {
		t.Run("accepted_"+tc.path, func(t *testing.T) {
			result := extractSessionID(tc.path)

			// These ARE extracted because they don't contain "/" after sessions/
			// Security is handled at different layers:
			// - URL decoding: HTTP router
			// - SQL injection: database layer (prepared statements)
			if result != tc.expected {
				t.Errorf("extractSessionID(%q) = %q, want %q", tc.path, result, tc.expected)
			}
		})
	}
}
