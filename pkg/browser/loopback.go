package browser

import (
	"net"
	"net/url"
	"strings"
)

// NormalizeLoopbackURL rewrites localhost / ::1 hosts to 127.0.0.1.
//
// Chromium (and some runtimes) resolve "localhost" to ::1 first. Dev servers
// commonly bind IPv4 only (0.0.0.0 / 127.0.0.1), so navigation to
// http://localhost:PORT gets net::ERR_CONNECTION_REFUSED even though
// http://127.0.0.1:PORT works. curl often falls back to IPv4; Chromium may not.
//
// Prefer this over rewriting to a container bridge IP when the browser runs
// inside the same network namespace as the service (sandbox Chromium).
func NormalizeLoopbackURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return normalizeLoopbackHostLiteral(rawURL)
	}
	host := u.Hostname()
	if !isLoopbackHostname(host) {
		return rawURL
	}
	port := u.Port()
	if port != "" {
		u.Host = net.JoinHostPort("127.0.0.1", port)
	} else {
		u.Host = "127.0.0.1"
	}
	return u.String()
}

func isLoopbackHostname(host string) bool {
	h := strings.TrimSpace(host)
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	switch strings.ToLower(h) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

func normalizeLoopbackHostLiteral(s string) string {
	replacements := []struct{ old, neu string }{
		{"http://localhost", "http://127.0.0.1"},
		{"https://localhost", "https://127.0.0.1"},
		{"http://[::1]", "http://127.0.0.1"},
		{"https://[::1]", "https://127.0.0.1"},
		{"http://::1", "http://127.0.0.1"},
		{"https://::1", "https://127.0.0.1"},
	}
	out := s
	for _, r := range replacements {
		out = strings.ReplaceAll(out, r.old, r.neu)
	}
	return out
}
