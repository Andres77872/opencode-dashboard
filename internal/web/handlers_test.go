package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestWriteJSONCacheControl validates that writeJSON sets the Cache-Control header.
func TestWriteJSONCacheControl(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	expected := "public, max-age=30"
	got := rec.Header().Get("Cache-Control")
	if got != expected {
		t.Errorf("Cache-Control header = %q, want %q", got, expected)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// TestWriteJSONCacheControlOnErrorResponse validates Cache-Control on error responses too.
func TestWriteJSONCacheControlOnErrorResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	NotFound("test not found").Write(rec)

	expected := "public, max-age=30"
	got := rec.Header().Get("Cache-Control")
	if got != expected {
		t.Errorf("Cache-Control header on error = %q, want %q", got, expected)
	}
}

// TestExtractMessageID validates message ID extraction from URL paths.
func TestExtractMessageID(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{name: "valid simple id", path: "/api/v1/messages/msg-001", expected: "msg-001"},
		{name: "valid with trailing slash", path: "/api/v1/messages/msg-001/", expected: "msg-001"},
		{name: "missing prefix", path: "/messages/msg-001", expected: ""},
		{name: "empty id", path: "/api/v1/messages/", expected: ""},
		{name: "no id segment", path: "/api/v1/messages", expected: ""},
		{name: "id with slashes", path: "/api/v1/messages/msg/001", expected: ""},
		{name: "empty path", path: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMessageID(tt.path)
			if result != tt.expected {
				t.Errorf("extractMessageID(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parsePeriodQuery — period/from/to parameter parsing
// ---------------------------------------------------------------------------

func TestParsePeriodQuery_defaultPeriod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	pq, apierr := parsePeriodQuery(req)
	if apierr != nil {
		t.Fatalf("unexpected API error: %v", apierr)
	}
	if pq.Period != "7d" {
		t.Errorf("Period = %q, want %q", pq.Period, "7d")
	}
	if pq.From != "" {
		t.Errorf("From should be empty, got %q", pq.From)
	}
}

func TestParsePeriodQuery_periodOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?period=30d", nil)
	pq, apierr := parsePeriodQuery(req)
	if apierr != nil {
		t.Fatalf("unexpected API error: %v", apierr)
	}
	if pq.Period != "30d" {
		t.Errorf("Period = %q, want %q", pq.Period, "30d")
	}
	if pq.From != "" {
		t.Errorf("From should be empty, got %q", pq.From)
	}
}

func TestParsePeriodQuery_validFromAndTo(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-01-15&to=2026-01-20", nil)
	pq, apierr := parsePeriodQuery(req)
	if apierr != nil {
		t.Fatalf("unexpected API error: %v", apierr)
	}
	if pq.Period != "" {
		t.Errorf("Period should be empty when from is set, got %q", pq.Period)
	}
	if pq.From != "2026-01-15" {
		t.Errorf("From = %q, want %q", pq.From, "2026-01-15")
	}
	if pq.To != "2026-01-20" {
		t.Errorf("To = %q, want %q", pq.To, "2026-01-20")
	}
}

func TestParsePeriodQuery_fromOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-04-01", nil)
	pq, apierr := parsePeriodQuery(req)
	if apierr != nil {
		t.Fatalf("unexpected API error: %v", apierr)
	}
	if pq.From != "2026-04-01" {
		t.Errorf("From = %q, want %q", pq.From, "2026-04-01")
	}
	if pq.To != "" {
		t.Errorf("To should be empty, got %q", pq.To)
	}
}

func TestParsePeriodQuery_fromBeatsPeriod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-02-01&to=2026-02-10&period=1d", nil)
	pq, apierr := parsePeriodQuery(req)
	if apierr != nil {
		t.Fatalf("unexpected API error: %v", apierr)
	}
	if pq.From != "2026-02-01" {
		t.Errorf("From = %q, want %q (from should beat period)", pq.From, "2026-02-01")
	}
	if pq.To != "2026-02-10" {
		t.Errorf("To = %q, want %q", pq.To, "2026-02-10")
	}
	if pq.Period != "" {
		t.Errorf("Period should be empty, got %q", pq.Period)
	}
}

func TestParsePeriodQuery_invalidFromFormat(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		wantMsg string
	}{
		{name: "DD-MM-YYYY", from: "15-01-2026", wantMsg: "invalid from date"},
		{name: "not a date", from: "abc", wantMsg: "invalid from date"},
		{name: "partial", from: "2026-01", wantMsg: "invalid from date"},
		{name: "slash format", from: "2026/01/15", wantMsg: "invalid from date"},
		{name: "empty value", from: "", wantMsg: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from="+tt.from, nil)
			_, apierr := parsePeriodQuery(req)

			if tt.wantMsg == "" {
				if apierr != nil {
					t.Errorf("unexpected API error for from=%q: %v", tt.from, apierr)
				}
				return
			}

			if apierr == nil {
				t.Fatalf("expected API error for from=%q, got nil", tt.from)
			}
			if apierr.Code != http.StatusBadRequest {
				t.Errorf("error code = %d, want %d", apierr.Code, http.StatusBadRequest)
			}
			if !strings.Contains(apierr.Message, tt.wantMsg) {
				t.Errorf("error message = %q, want containing %q", apierr.Message, tt.wantMsg)
			}
		})
	}
}

func TestParsePeriodQuery_invalidToFormat(t *testing.T) {
	tests := []struct {
		name    string
		to      string
		wantMsg string
	}{
		{name: "DD-MM-YYYY", to: "20-01-2026", wantMsg: "invalid to date"},
		{name: "not a date", to: "abc", wantMsg: "invalid to date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-01-01&to="+tt.to, nil)
			_, apierr := parsePeriodQuery(req)

			if apierr == nil {
				t.Fatalf("expected API error, got nil")
			}
			if apierr.Code != http.StatusBadRequest {
				t.Errorf("error code = %d, want %d", apierr.Code, http.StatusBadRequest)
			}
			if !strings.Contains(apierr.Message, tt.wantMsg) {
				t.Errorf("error message = %q, want containing %q", apierr.Message, tt.wantMsg)
			}
		})
	}
}

func TestParsePeriodQuery_futureFromDate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2099-01-01", nil)
	_, apierr := parsePeriodQuery(req)

	if apierr == nil {
		t.Fatal("expected API error for future from date, got nil")
	}
	if apierr.Code != http.StatusBadRequest {
		t.Errorf("error code = %d, want %d", apierr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(apierr.Message, "future") {
		t.Errorf("error message = %q, want containing 'future'", apierr.Message)
	}
}

func TestParsePeriodQuery_futureToDate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-01-01&to=2099-12-31", nil)
	_, apierr := parsePeriodQuery(req)

	if apierr == nil {
		t.Fatal("expected API error for future to date, got nil")
	}
	if apierr.Code != http.StatusBadRequest {
		t.Errorf("error code = %d, want %d", apierr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(apierr.Message, "future") {
		t.Errorf("error message = %q, want containing 'future'", apierr.Message)
	}
}

func TestParsePeriodQuery_fromAfterTo(t *testing.T) {
	// Both dates must be in the past; "from" must be after "to"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-05-01&to=2026-04-15", nil)
	_, apierr := parsePeriodQuery(req)

	if apierr == nil {
		t.Fatal("expected API error for from > to, got nil")
	}
	if apierr.Code != http.StatusBadRequest {
		t.Errorf("error code = %d, want %d", apierr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(apierr.Message, "before or equal") {
		t.Errorf("error message = %q, want containing 'before or equal'", apierr.Message)
	}
}

func TestParsePeriodQuery_sameFromAndTo(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?from=2026-04-15&to=2026-04-15", nil)
	pq, apierr := parsePeriodQuery(req)
	if apierr != nil {
		t.Fatalf("unexpected API error for same from/to: %v", apierr)
	}
	if pq.From != "2026-04-15" || pq.To != "2026-04-15" {
		t.Errorf("pq = %+v, want From=2026-04-15, To=2026-04-15", pq)
	}
}

func TestParsePeriodQuery_allPresetsPassthrough(t *testing.T) {
	presets := []string{"1h", "6h", "12h", "24h", "72h", "1d", "7d", "14d", "30d", "1y", "all"}
	for _, p := range presets {
		t.Run("preset_"+p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?period="+p, nil)
			pq, apierr := parsePeriodQuery(req)
			if apierr != nil {
				t.Fatalf("unexpected API error for period=%q: %v", p, apierr)
			}
			if pq.Period != p {
				t.Errorf("Period = %q, want %q", pq.Period, p)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// End-to-end: handler routes call parsePeriodQuery and return 400 on bad input
// ---------------------------------------------------------------------------

func TestHandler_InvalidCustomRangeReturns400(t *testing.T) {
	t.Skip("Skipping handler e2e — requires real store with DB")
}

func TestHandler_ValidCustomRangePasses(t *testing.T) {
	t.Skip("Skipping handler e2e — requires real store with DB")
}
