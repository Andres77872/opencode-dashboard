package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// mutationOriginMiddleware rejects browser-forged actions before any handler
// can change settings, rebuild the cache, or start a provider request. Native
// clients without browser headers retain the existing local API contract.
func mutationOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Read routes and CORS preflights do not perform user mutations.
		default:
			if !isAllowedDashboardRequest(r) {
				APIError{
					Error:   http.StatusText(http.StatusForbidden),
					Code:    http.StatusForbidden,
					Message: "dashboard mutations are allowed only from the dashboard origin",
				}.Write(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isAllowedDashboardRequest(r *http.Request) bool {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return isAllowedDashboardOrigin(origin, r)
	}
	// Browsers can omit Origin on some requests. Same-site is not enough:
	// another localhost port is a different application. Native clients omit
	// both headers, while same-origin browser requests remain valid.
	site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))
	return site == "" || site == "same-origin"
}

// The Host guard must run first: comparing Origin to an unchecked Host would
// accept a rebindable attacker hostname. All browser-facing paths share this
// policy so a CORS grant cannot undermine the assistant's origin restrictions.
func isAllowedDashboardOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if parsed.Scheme != scheme {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}

	// Preserve the checked-in Vite development server's cross-port access.
	// Embedded assets, including LAN/custom-port deployments, use same-origin.
	requestURL := &url.URL{Scheme: scheme, Host: r.Host}
	return parsed.Port() == "7451" && requestURL.Port() == strconv.Itoa(DefaultPort) &&
		isLoopbackHostname(parsed.Hostname()) && isLoopbackHostname(requestURL.Hostname())
}

func isLoopbackHostname(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}
