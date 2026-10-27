package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"opencode-dashboard/internal/source"
)

// testOrigin addresses test requests the way a browser on the default
// listener does. httptest's default Host, example.com, is a DNS name the host
// guard rejects.
const testOrigin = "http://127.0.0.1:7450"

func TestHostGuardRejectsRebindableNames(t *testing.T) {
	server := NewServer(ServerOptions{Registry: source.NewRegistry(source.SourceOpenCode)})

	for _, host := range []string{"attacker.example:7450", "attacker.example", "127.0.0.1.attacker.example:7450", "localhost.attacker.example"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Host %q: status = %d, want %d", host, rec.Code, http.StatusForbidden)
			continue
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Host %q: rejected response carries Access-Control-Allow-Origin %q", host, got)
		}
		var body APIError
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != http.StatusForbidden {
			t.Errorf("Host %q: body = %q, want a JSON 403 APIError", host, rec.Body.String())
		}
	}
}

func TestHostGuardAllowsLoopbackAndIPHosts(t *testing.T) {
	server := NewServer(ServerOptions{Registry: source.NewRegistry(source.SourceOpenCode)})

	for _, host := range []string{"127.0.0.1:7450", "localhost:7450", "LOCALHOST", "dash.localhost:7450", "[::1]:7450", "192.168.1.20:7450", "localhost.:7450"} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Host %q: status = %d, want %d", host, rec.Code, http.StatusOK)
		}
	}
}

func TestHostPolicyNamedBindAndExtraHosts(t *testing.T) {
	policy := newHostPolicy("dashboard.lan:7450", []string{" Proxy.Example.COM. ", "other.example:8443"})
	for _, host := range []string{"dashboard.lan:7450", "DASHBOARD.LAN", "proxy.example.com", "other.example"} {
		if !policy.allows(host) {
			t.Errorf("allows(%q) = false, want true", host)
		}
	}
	if policy.allows("evil.example") {
		t.Error("allows(evil.example) = true, want false")
	}
	if !policy.allows("") {
		t.Error("allows(empty Host) = false, want true for HTTP/1.0 clients")
	}
}

func TestHostPolicyLANModeAllowsMachineHostname(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		t.Skip("no machine hostname")
	}
	for _, addr := range []string{"0.0.0.0:7450", "[::]:7450", ":7450"} {
		policy := newHostPolicy(addr, nil)
		if !policy.allows(hostname+":7450") || !policy.allows(hostname+".local:7450") {
			t.Errorf("%s: machine hostname %q not allowed in LAN mode", addr, hostname)
		}
	}
	if newHostPolicy("127.0.0.1:7450", nil).allows(hostname + ".local") {
		t.Errorf("loopback bind allows %q.local; only LAN mode should", hostname)
	}
}
