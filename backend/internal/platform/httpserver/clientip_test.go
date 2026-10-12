package httpserver

import (
	"net"
	"net/http/httptest"
	"testing"
)

func mustNets(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	var out []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestClientIPTrustsOnlyProxies(t *testing.T) {
	trusted := mustNets(t, "172.16.0.0/12", "127.0.0.1/32")

	// Via trusted proxy: leftmost forwarded IP wins.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.16.5.4:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 172.16.5.4")
	if got := ClientIP(req, trusted); got != "203.0.113.9" {
		t.Fatalf("proxy: %s", got)
	}
	// Direct client spoofing the header: ignored.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := ClientIP(req, trusted); got != "198.51.100.7" {
		t.Fatalf("spoof accepted: %s", got)
	}
	// Trusted peer, no header: peer itself.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	if got := ClientIP(req, trusted); got != "127.0.0.1" {
		t.Fatalf("peer: %s", got)
	}
	// No trusted nets configured: header always ignored.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.16.5.4:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := ClientIP(req, nil); got != "172.16.5.4" {
		t.Fatalf("empty trust: %s", got)
	}
}
