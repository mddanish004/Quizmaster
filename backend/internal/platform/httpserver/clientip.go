package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP resolves the client IP for rate limiting and audit metadata.
// X-Forwarded-For is honored ONLY when the direct peer belongs to the
// configured trusted proxy CIDRs; otherwise the peer address is used.
// This prevents limit evasion and bucket poisoning via spoofed headers.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := hostOnly(r.RemoteAddr)
	if len(trusted) > 0 {
		if addr, err := net.ResolveTCPAddr("tcp", r.RemoteAddr); err == nil {
			for _, network := range trusted {
				if network.Contains(addr.IP) {
					if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
						if i := strings.Index(fwd, ","); i >= 0 {
							return strings.TrimSpace(fwd[:i])
						}
						return strings.TrimSpace(fwd)
					}
					break
				}
			}
		}
	}
	return peer
}

func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}
