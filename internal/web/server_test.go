package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggingMiddlewareReportsCanceledRequestsAsClientClosed(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := LoggingMiddleware(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/canceled", nil).WithContext(ctx)

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(logs.String(), "status=499") {
		t.Fatalf("canceled request log = %q, want status=499", logs.String())
	}
}

func TestCORSMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		wantOrigin string
	}{
		{
			name:       "localhost with Vite port",
			origin:     "http://localhost:7451",
			wantOrigin: "http://localhost:7451",
		},
		{
			name:       "127.0.0.1 with Vite port",
			origin:     "http://127.0.0.1:7451",
			wantOrigin: "http://127.0.0.1:7451",
		},
		{
			name:       "localhost with port 3000",
			origin:     "http://localhost:3000",
			wantOrigin: "http://localhost:3000",
		},
		{
			name:       "localhost with backend port 7450",
			origin:     "http://localhost:7450",
			wantOrigin: "http://localhost:7450",
		},
		{
			name:       "localhost with any port",
			origin:     "http://localhost:9000",
			wantOrigin: "http://localhost:9000",
		},
		{
			name:       "no origin header gets no CORS grant",
			origin:     "",
			wantOrigin: "",
		},
		{
			// The API is unauthenticated and serves local project paths, session
			// titles and config. A wildcard here would let any site the user visits
			// read it, so a non-local origin must get no grant at all.
			name:       "external origin gets no CORS grant",
			origin:     "https://example.com",
			wantOrigin: "",
		},
		{
			name:       "IPv6 localhost",
			origin:     "http://[::1]:7451",
			wantOrigin: "http://[::1]:7451",
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			w := httptest.NewRecorder()
			corsMiddleware(handler).ServeHTTP(w, req)

			gotOrigin := w.Header().Get("Access-Control-Allow-Origin")
			if gotOrigin != tt.wantOrigin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", gotOrigin, tt.wantOrigin)
			}
			if gotOrigin == "*" {
				t.Error("Access-Control-Allow-Origin must never be a wildcard: the API is unauthenticated")
			}

			if w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, DELETE, OPTIONS" {
				t.Errorf("Access-Control-Allow-Methods = %q, want %q", w.Header().Get("Access-Control-Allow-Methods"), "GET, POST, DELETE, OPTIONS")
			}
		})
	}
}

func TestCORSMiddlewareOptionsRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for OPTIONS request")
	})

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:7451")

	w := httptest.NewRecorder()
	corsMiddleware(handler).ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusNoContent)
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:7451" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", w.Header().Get("Access-Control-Allow-Origin"), "http://localhost:7451")
	}
}

func TestIsLocalOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:7451", true},
		{"http://localhost:3000", true},
		{"http://localhost:7450", true},
		{"http://localhost:5123", true},
		{"http://localhost:4000", true},
		{"http://localhost:9000", true},
		{"http://localhost:80", true},
		{"http://127.0.0.1:7451", true},
		{"http://127.0.0.1:3000", true},
		{"http://127.0.0.1:7450", true},
		{"http://127.0.0.1:9000", true},
		{"http://[::1]:7451", true},
		{"http://[::1]:3000", true},
		{"http://localhost", true},
		{"https://example.com", false},
		{"http://192.168.1.1:7451", false},
		{"not-a-url", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			got := isLocalOrigin(tt.origin)
			if got != tt.want {
				t.Errorf("isLocalOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}
