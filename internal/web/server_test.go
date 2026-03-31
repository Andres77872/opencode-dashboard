package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		origin       string
		wantOrigin   string
		wantAllowAll bool
	}{
		{
			name:         "localhost with Vite port",
			origin:       "http://localhost:7451",
			wantOrigin:   "http://localhost:7451",
			wantAllowAll: false,
		},
		{
			name:         "127.0.0.1 with Vite port",
			origin:       "http://127.0.0.1:7451",
			wantOrigin:   "http://127.0.0.1:7451",
			wantAllowAll: false,
		},
		{
			name:         "localhost with port 3000",
			origin:       "http://localhost:3000",
			wantOrigin:   "http://localhost:3000",
			wantAllowAll: false,
		},
		{
			name:         "localhost with backend port 7450",
			origin:       "http://localhost:7450",
			wantOrigin:   "http://localhost:7450",
			wantAllowAll: false,
		},
		{
			name:         "localhost with any port",
			origin:       "http://localhost:9000",
			wantOrigin:   "http://localhost:9000",
			wantAllowAll: false,
		},
		{
			name:         "no origin header",
			origin:       "",
			wantOrigin:   "",
			wantAllowAll: true,
		},
		{
			name:         "external origin falls back to wildcard",
			origin:       "https://example.com",
			wantOrigin:   "",
			wantAllowAll: true,
		},
		{
			name:         "IPv6 localhost",
			origin:       "http://[::1]:7451",
			wantOrigin:   "http://[::1]:7451",
			wantAllowAll: false,
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
			if tt.wantAllowAll {
				if gotOrigin != "*" {
					t.Errorf("Access-Control-Allow-Origin = %q, want %q", gotOrigin, "*")
				}
			} else {
				if gotOrigin != tt.wantOrigin {
					t.Errorf("Access-Control-Allow-Origin = %q, want %q", gotOrigin, tt.wantOrigin)
				}
			}

			if w.Header().Get("Access-Control-Allow-Methods") != "GET, OPTIONS" {
				t.Errorf("Access-Control-Allow-Methods = %q, want %q", w.Header().Get("Access-Control-Allow-Methods"), "GET, OPTIONS")
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
