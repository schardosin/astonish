package api

import (
	"net"
	"strings"
	"testing"
)

func TestCredentialSuffixParsing(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantURL    string
		wantCred   string
	}{
		{
			name:     "no credential",
			url:      "https://api.example.com/data",
			wantURL:  "https://api.example.com/data",
			wantCred: "",
		},
		{
			name:     "simple credential",
			url:      "https://api.example.com/data@my-api-key",
			wantURL:  "https://api.example.com/data",
			wantCred: "my-api-key",
		},
		{
			name:     "credential with underscores",
			url:      "https://api.example.com/v2/users@github_token",
			wantURL:  "https://api.example.com/v2/users",
			wantCred: "github_token",
		},
		{
			name:     "URL with @ in basic auth (should not match)",
			url:      "https://user:pass@api.example.com/data",
			wantURL:  "https://user:pass@api.example.com/data",
			wantCred: "",
		},
		{
			name:     "URL with @ in path (should not match — has / after @)",
			url:      "https://api.example.com/@user/repos",
			wantURL:  "https://api.example.com/@user/repos",
			wantCred: "",
		},
		{
			name:     "URL with query params and credential",
			url:      "https://api.example.com/data?format=json@my-cred",
			wantURL:  "https://api.example.com/data?format=json",
			wantCred: "my-cred",
		},
		{
			name:     "credential starts with uppercase",
			url:      "https://api.example.com/data@MyCredential",
			wantURL:  "https://api.example.com/data",
			wantCred: "MyCredential",
		},
		{
			name:     "@ but no valid name after (digit start)",
			url:      "https://api.example.com/data@123",
			wantURL:  "https://api.example.com/data@123",
			wantCred: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.url
			var credentialName string
			if m := credentialSuffixRe.FindStringSubmatchIndex(url); m != nil {
				credentialName = url[m[2]:m[3]]
				url = url[:m[0]]
			}

			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if credentialName != tt.wantCred {
				t.Errorf("credential = %q, want %q", credentialName, tt.wantCred)
			}
		})
	}
}

func TestResolveHTTPSource_NoCredential(t *testing.T) {
	// Test that a basic request without credential still works.
	// We use a known-good public endpoint.
	data, err := resolveHTTPSource("GET:https://httpbin.org/get", nil)
	if err != nil {
		t.Skipf("skipping external HTTP test: %v", err)
	}
	if data == nil {
		t.Error("expected non-nil data")
	}
}

// ── SSRF protection tests ────────────────────────────────────────────

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		// Public IPs — should be allowed
		{"Google DNS", "8.8.8.8", false},
		{"Cloudflare DNS", "1.1.1.1", false},
		{"Public IPv6", "2607:f8b0:4004:800::200e", false},

		// Loopback — must block
		{"Loopback v4", "127.0.0.1", true},
		{"Loopback v4 alt", "127.0.0.2", true},
		{"Loopback v6", "::1", true},

		// RFC 1918 — must block
		{"10.x", "10.0.0.1", true},
		{"10.x high", "10.255.255.255", true},
		{"172.16.x", "172.16.0.1", true},
		{"172.31.x", "172.31.255.255", true},
		{"192.168.x", "192.168.1.1", true},

		// Cloud metadata — must block
		{"AWS metadata", "169.254.169.254", true},
		{"Link-local", "169.254.1.1", true},

		// IPv6 private — must block
		{"IPv6 unique local", "fd00::1", true},
		{"IPv6 link-local", "fe80::1", true},

		// "This" network — must block
		{"Zero network", "0.0.0.1", true},
		{"Unspecified", "0.0.0.0", true},

		// Shared address space (CGNAT) — must block
		{"CGNAT", "100.64.0.1", true},

		// Edge: 172.15 is NOT private, 172.32 is NOT private
		{"172.15 is public", "172.15.255.255", false},
		{"172.32 is public", "172.32.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test IP: %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestValidateHTTPURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string // substring of error, or "" for no error
	}{
		{"public https", "https://api.example.com/data", ""},
		{"public http", "http://api.example.com/data", ""},
		{"ftp scheme blocked", "ftp://evil.com/file", "unsupported URL scheme"},
		{"file scheme blocked", "file:///etc/passwd", "unsupported URL scheme"},
		{"gopher scheme blocked", "gopher://evil.com", "unsupported URL scheme"},
		{"localhost IP", "http://127.0.0.1/admin", "private/internal"},
		{"localhost v6", "http://[::1]/admin", "private/internal"},
		{"private 10.x", "http://10.0.0.1/secret", "private/internal"},
		{"private 172.16", "http://172.16.0.1/secret", "private/internal"},
		{"private 192.168", "http://192.168.1.1/secret", "private/internal"},
		{"metadata endpoint", "http://169.254.169.254/latest/meta-data/", "private/internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHTTPURL(tt.url)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestResolveHTTPSource_SSRFBlocked(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"localhost", "GET:http://127.0.0.1/admin"},
		{"metadata endpoint", "GET:http://169.254.169.254/latest/meta-data/"},
		{"private 10.x", "GET:http://10.0.0.1/internal"},
		{"private 192.168", "GET:http://192.168.1.1/router"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveHTTPSource(tt.spec, nil)
			if err == nil {
				t.Error("expected SSRF error, got nil")
			} else if !strings.Contains(err.Error(), "private/internal") {
				t.Errorf("expected private/internal error, got: %v", err)
			}
		})
	}
}
