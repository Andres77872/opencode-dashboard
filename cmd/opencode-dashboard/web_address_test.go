package main

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestWebAddresses(t *testing.T) {
	for _, tt := range []struct {
		host string
		port int
		addr string
		url  string
	}{
		{"127.0.0.1", 7450, "127.0.0.1:7450", "http://127.0.0.1:7450"},
		{"0.0.0.0", 9090, "0.0.0.0:9090", "http://127.0.0.1:9090"},
		{"192.168.1.20", 7450, "192.168.1.20:7450", "http://192.168.1.20:7450"},
		{"localhost", 7450, "localhost:7450", "http://localhost:7450"},
		{"::", 7450, "[::]:7450", "http://[::1]:7450"},
		{"fd00::20", 9090, "[fd00::20]:9090", "http://[fd00::20]:9090"},
	} {
		t.Run(tt.host, func(t *testing.T) {
			addr, err := webListenAddress(tt.host, tt.port)
			if err != nil || addr != tt.addr {
				t.Fatalf("address = %q, %v; want %q", addr, err, tt.addr)
			}
			if got := webBrowserURL(tt.host, tt.port); got != tt.url {
				t.Fatalf("browser URL = %q; want %q", got, tt.url)
			}
		})
	}
}

func TestWebRejectsInvalidBindFlagsBeforeOpeningStores(t *testing.T) {
	for _, host := range []string{"", "http://localhost", "localhost:7450", "[::1]", "bad host", "host/path"} {
		if err := cmdWeb([]string{"--host", host}); err == nil || !strings.Contains(err.Error(), "--host must be") {
			t.Errorf("--host %q: got %v, want host validation error", host, err)
		}
	}
	for _, port := range []string{"0", "-1", "65536"} {
		if err := cmdWeb([]string{"--port", port}); err == nil || !strings.Contains(err.Error(), "--port must be") {
			t.Errorf("--port %q: got %v, want port validation error", port, err)
		}
	}
}

func TestWebNetworkURLs(t *testing.T) {
	var addrs []net.Addr
	for _, cidr := range []string{"127.0.0.1/8", "::1/128", "192.168.1.20/24", "192.168.1.20/24", "fd00::20/64", "fe80::20/64", "169.254.1.20/16"} {
		ip, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		network.IP = ip
		addrs = append(addrs, network)
	}
	for _, tt := range []struct {
		host string
		want []string
	}{
		{"0.0.0.0", []string{"http://192.168.1.20:9090"}},
		{"::", []string{"http://192.168.1.20:9090", "http://[fd00::20]:9090"}},
	} {
		if got := webNetworkURLs(tt.host, 9090, addrs); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("network URLs for %s = %v; want %v", tt.host, got, tt.want)
		}
	}
}
