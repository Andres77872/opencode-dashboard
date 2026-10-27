package main

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func webListenAddress(host string, port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("--port must be between 1 and 65535")
	}
	if _, err := netip.ParseAddr(host); err != nil {
		if host == "" || strings.ContainsAny(host, ":/\\?#@[] \t\r\n") {
			return "", fmt.Errorf("--host must be an IP address or hostname without a scheme or port")
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// webAllowedHosts splits --allowed-hosts. Each entry is a bare hostname, like
// --host: the server matches names, not ports or URLs.
func webAllowedHosts(value string) ([]string, error) {
	var hosts []string
	for _, entry := range strings.Split(value, ",") {
		host := strings.TrimSpace(entry)
		if host == "" {
			continue
		}
		if strings.ContainsAny(host, ":/\\?#@[] \t\r\n") {
			return nil, fmt.Errorf("--allowed-hosts entries must be hostnames without a scheme or port: %q", host)
		}
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func webBrowserURL(host string, port int) string {
	// Wildcard addresses describe listeners, not browser destinations.
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}).String()
}

func webNetworkURLs(host string, port int, addrs []net.Addr) []string {
	var urls []string
	seen := make(map[string]bool)
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || !ip.IsGlobalUnicast() || ip.IsLoopback() {
			continue
		}
		// Advertise IPv4 URLs when the user selected an IPv4 wildcard.
		if bindIP := net.ParseIP(host); bindIP != nil && bindIP.To4() != nil && ip.To4() == nil {
			continue
		}
		u := webBrowserURL(ip.String(), port)
		if !seen[u] {
			urls = append(urls, u)
			seen[u] = true
		}
	}
	sort.Strings(urls)
	return urls
}
