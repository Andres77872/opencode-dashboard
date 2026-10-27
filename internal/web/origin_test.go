package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCacheSyncRejectsCrossOriginBeforeMutation(t *testing.T) {
	for _, test := range []struct{ name, origin, site string }{
		{"foreign form", "https://untrusted.invalid", "cross-site"},
		{"opaque origin", "null", "cross-site"},
		{"unrelated local app", "http://localhost:8000", "same-site"},
		{"missing origin cross-site", "", "cross-site"},
		{"missing origin same-site", "", "same-site"},
		{"foreign origin with same-origin metadata", "https://untrusted.invalid", "same-origin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &fakeCacheManager{}
			server := NewServer(ServerOptions{Cache: manager})
			req := httptest.NewRequest(http.MethodPost, testOrigin+"/api/v1/cache/sync?mode=rebuild&source=codex", nil)
			req.Header.Set("Origin", test.origin)
			req.Header.Set("Sec-Fetch-Site", test.site)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			server.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden || manager.syncedMode != "" {
				t.Fatalf("status=%d synced=%q, want 403 without cache work; body=%s", rec.Code, manager.syncedMode, rec.Body.String())
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Error("rejected origin received a CORS grant")
			}
		})
	}
}

func TestCacheSyncPreservesDashboardAndNativeClients(t *testing.T) {
	for _, test := range []struct{ name, target, origin, site string }{
		{"native client", testOrigin, "", ""},
		{"same-origin browser", testOrigin, testOrigin, "same-origin"},
		{"same-origin metadata", testOrigin, "", "same-origin"},
		{"Vite localhost", testOrigin, "http://localhost:7451", "cross-site"},
		{"Vite IP", testOrigin, "http://127.0.0.1:7451", "same-site"},
		{"Vite IPv6", testOrigin, "http://[::1]:7451", "cross-site"},
		{"LAN browser", "http://192.168.1.20:8399", "http://192.168.1.20:8399", "same-origin"},
		{"IPv6 browser", "http://[::1]:8399", "http://[::1]:8399", "same-origin"},
		{"named host", "http://dashboard.lan:8399", "http://dashboard.lan:8399", "same-origin"},
		{"HTTPS browser", "https://dashboard.lan", "https://dashboard.lan", "same-origin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &fakeCacheManager{}
			server := NewServer(ServerOptions{Cache: manager, AllowedHosts: []string{"dashboard.lan"}})
			req := httptest.NewRequest(http.MethodPost, test.target+"/api/v1/cache/sync?mode=incremental&source=codex", nil)
			req.Header.Set("Origin", test.origin)
			req.Header.Set("Sec-Fetch-Site", test.site)
			if test.target == "https://dashboard.lan" {
				req.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			server.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || manager.syncedMode != "incremental" || manager.syncedSource != "codex" {
				t.Fatalf("status=%d source=%q mode=%q; body=%s", rec.Code, manager.syncedSource, manager.syncedMode, rec.Body.String())
			}
		})
	}
}

func TestMutationOriginMiddlewareProtectsAllUnsafeMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			called := false
			handler := mutationOriginMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			req := httptest.NewRequest(method, testOrigin+"/api/v1/settings", nil)
			req.Header.Set("Origin", "https://untrusted.invalid")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if called || rec.Code != http.StatusForbidden {
				t.Fatalf("called=%v status=%d, want no work and 403", called, rec.Code)
			}
		})
	}
}

func TestCacheSyncPreflightDoesNotPerformWork(t *testing.T) {
	for _, test := range []struct{ origin, wantOrigin string }{
		{"http://localhost:7451", "http://localhost:7451"},
		{"http://localhost:8000", ""},
		{"https://untrusted.invalid", ""},
		{"null", ""},
	} {
		t.Run(test.origin, func(t *testing.T) {
			manager := &fakeCacheManager{}
			server := NewServer(ServerOptions{Cache: manager})
			req := httptest.NewRequest(http.MethodOptions, testOrigin+"/api/v1/cache/sync?mode=rebuild", nil)
			req.Header.Set("Origin", test.origin)
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			rec := httptest.NewRecorder()
			server.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || manager.syncedMode != "" {
				t.Fatalf("status=%d synced=%q, want 204 without cache work", rec.Code, manager.syncedMode)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != test.wantOrigin {
				t.Errorf("allow origin=%q, want %q", got, test.wantOrigin)
			}
		})
	}
}
