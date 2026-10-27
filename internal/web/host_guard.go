package web

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

// hostPolicy decides which Host headers the server answers. Without it a
// loopback server is reachable through DNS rebinding: a page on
// attacker.example re-resolves its own name to 127.0.0.1, and its requests
// then carry Host and Origin attacker.example:<port>, which every same-origin
// check accepts. IP-literal hosts cannot be rebound and localhost names always
// resolve to loopback, so only other DNS names need to be listed.
type hostPolicy struct {
	names map[string]bool
}

// newHostPolicy allows IP literals, localhost names, a hostname the server was
// bound to, the machine's own hostname when listening on every interface (LAN
// mode), and any extra names the user listed (--allowed-hosts).
func newHostPolicy(addr string, extra []string) hostPolicy {
	policy := hostPolicy{names: make(map[string]bool)}
	bindHost, _, err := net.SplitHostPort(addr)
	if err != nil {
		bindHost = addr
	}
	allInterfaces := bindHost == ""
	if bindIP, err := netip.ParseAddr(bindHost); err == nil {
		allInterfaces = bindIP.IsUnspecified()
	} else if bindHost != "" {
		policy.allow(bindHost)
	}
	if allInterfaces {
		if hostname, err := os.Hostname(); err == nil {
			policy.allow(hostname)
			policy.allow(hostname + ".local")
		}
	}
	for _, name := range extra {
		policy.allow(name)
	}
	return policy
}

func (p hostPolicy) allow(name string) {
	if host := normalizeHostName(name); host != "" {
		p.names[host] = true
	}
}

func (p hostPolicy) allows(hostHeader string) bool {
	// HTTP/1.0 clients may omit Host; browsers, which rebinding needs, never do.
	if hostHeader == "" {
		return true
	}
	host := normalizeHostName(hostHeader)
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return p.names[host]
}

// normalizeHostName drops any port, IPv6 brackets and trailing root dot, and
// lower-cases the name.
func normalizeHostName(value string) string {
	host := strings.TrimSpace(value)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func hostGuardMiddleware(policy hostPolicy, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !policy.allows(r.Host) {
				logger.Warn("rejected request for unrecognized host; pass it to --allowed-hosts if it is yours", "host", r.Host, "path", r.URL.Path, "remote", r.RemoteAddr)
				APIError{
					Error:   http.StatusText(http.StatusForbidden),
					Code:    http.StatusForbidden,
					Message: "Address the dashboard by IP address, localhost, or a hostname passed to --allowed-hosts.",
				}.Write(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
