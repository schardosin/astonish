package api

import (
	"fmt"
	"testing"
)

func TestLooksLikeNetworkDenial(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want bool
	}{
		// --- Strong indicators (trigger regardless of exit code) ---
		{
			name: "exit 0 + CONNECT tunnel failed (real-world proxy denial)",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "* CONNECT tunnel: HTTP/1.1 negotiated\r\n* Establish HTTP proxy tunnel to identity-3.qa-de-1.cloud.sap:443\r\n> CONNECT identity-3.qa-de-1.cloud.sap:443 HTTP/1.1\r\r\n< HTTP/1.1 403 Forbidden\r\r\n< Content-Type: application/json\r\r\n< Content-Length: 101\r\r\n* CONNECT tunnel failed, response 403\r\ncurl: (56) CONNECT tunnel failed, response 403\r\n",
			},
			want: true,
		},
		{
			name: "exit 0 + egress denied",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "Egress denied for host packages.sap.com:443\nFalling back...",
			},
			want: true,
		},
		{
			name: "exit 0 + egress blocked",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "{\"error\":\"egress blocked\",\"host\":\"api.example.com\",\"port\":443}",
			},
			want: true,
		},
		{
			name: "exit 0 + policy violation",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "Error: policy violation for outbound connection\nRetrying...\nDone.",
			},
			want: true,
		},
		{
			name: "exit 56 + CONNECT tunnel failed",
			resp: map[string]any{
				"exit_code": float64(56),
				"stdout":    "curl: (56) CONNECT tunnel failed, response 403",
			},
			want: true,
		},

		// --- Weak indicators (require non-zero exit code) ---
		{
			name: "exit 0 + connection refused is NOT a denial (weak indicator)",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "Warning: connection refused, retrying\nSuccess on retry.",
			},
			want: false,
		},
		{
			name: "non-zero exit + connection refused",
			resp: map[string]any{
				"exit_code": float64(7),
				"stdout":    "curl: (7) Failed to connect to api.example.com port 443: Connection refused",
			},
			want: true,
		},
		{
			name: "non-zero exit + 403 forbidden",
			resp: map[string]any{
				"exit_code": float64(22),
				"stdout":    "HTTP/1.1 403 Forbidden\nContent-Length: 101",
			},
			want: true,
		},
		{
			name: "non-zero exit + could not resolve host",
			resp: map[string]any{
				"exit_code": float64(6),
				"stdout":    "curl: (6) Could not resolve host: packages.sap.com",
			},
			want: true,
		},
		{
			name: "non-zero exit + unrelated error",
			resp: map[string]any{
				"exit_code": float64(1),
				"stdout":    "file not found: /etc/config.yaml",
			},
			want: false,
		},
		{
			name: "non-zero exit + empty stdout",
			resp: map[string]any{
				"exit_code": float64(1),
				"stdout":    "",
			},
			want: false,
		},
		{
			name: "int exit_code (non-zero) + network error",
			resp: map[string]any{
				"exit_code": 1,
				"stdout":    "Failed to connect to api.sap.com:443",
			},
			want: true,
		},

		// --- Edge cases ---
		{
			name: "no exit_code field + connection refused",
			resp: map[string]any{
				"stdout": "connection refused",
			},
			want: false,
		},
		{
			name: "nil resp",
			resp: nil,
			want: false,
		},
		{
			name: "exit 0 + normal 403 from a web server (not proxy)",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "HTTP/1.1 403 Forbidden\n{\"error\":\"unauthorized\"}",
			},
			want: false, // weak indicator "403 forbidden" requires non-zero exit
		},
		{
			name: "exit 0 + multi-line script with proxy content-length 101",
			resp: map[string]any{
				"exit_code": float64(0),
				"stdout":    "Payload written. Authenticating...\r\n< HTTP/1.1 403 Forbidden\r\r\n< Content-Type: application/json\r\r\n< Content-Length: 101\r\r\n* CONNECT tunnel failed, response 403\r\ncurl: (56) CONNECT tunnel failed, response 403\r\n--- Response file size ---\r\nFile not found\r\n",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeNetworkDenial(tt.resp)
			if got != tt.want {
				t.Errorf("looksLikeNetworkDenial() = %v, want %v\nresp: %v", got, tt.want, tt.resp)
			}
		})
	}
}

func TestExtractDenialsFromOutput(t *testing.T) {
	tests := []struct {
		name      string
		stdout    string
		wantHosts []string // expected host:port pairs
	}{
		{
			name:      "real-world CONNECT tunnel denial",
			stdout:    "* Establish HTTP proxy tunnel to identity-3.qa-de-1.cloud.sap:443\r\n> CONNECT identity-3.qa-de-1.cloud.sap:443 HTTP/1.1\r\r\n< HTTP/1.1 403 Forbidden\r\r\n* CONNECT tunnel failed, response 403\r\n",
			wantHosts: []string{"identity-3.qa-de-1.cloud.sap:443"},
		},
		{
			name:      "multiple CONNECT denials deduplicated",
			stdout:    "> CONNECT api.sap.com:443 HTTP/1.1\r\n< 403\r\n> CONNECT api.sap.com:443 HTTP/1.1\r\n< 403\r\n",
			wantHosts: []string{"api.sap.com:443"},
		},
		{
			name:      "could not resolve host",
			stdout:    "curl: (6) Could not resolve host: packages.internal.sap.com\n",
			wantHosts: []string{"packages.internal.sap.com:443"},
		},
		{
			name:      "multiple different hosts",
			stdout:    "> CONNECT identity.cloud.sap:443 HTTP/1.1\r\n< 403\r\n> CONNECT compute.cloud.sap:8774 HTTP/1.1\r\n< 403\r\n",
			wantHosts: []string{"identity.cloud.sap:443", "compute.cloud.sap:8774"},
		},
		{
			name:      "no denials in normal output",
			stdout:    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n{\"servers\": []}",
			wantHosts: nil,
		},
		{
			name:      "empty stdout",
			stdout:    "",
			wantHosts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			denials := extractDenialsFromOutput(tt.stdout)

			if len(tt.wantHosts) == 0 {
				if len(denials) != 0 {
					t.Errorf("expected no denials, got %d: %v", len(denials), denials)
				}
				return
			}

			if len(denials) != len(tt.wantHosts) {
				t.Fatalf("expected %d denials, got %d: %v", len(tt.wantHosts), len(denials), denials)
			}

			for i, want := range tt.wantHosts {
				got := denials[i]["host"].(string) + ":" + fmt.Sprint(denials[i]["port"])
				if got != want {
					t.Errorf("denial[%d] = %q, want %q", i, got, want)
				}
				// Verify broader_pattern is present for multi-label hosts
				if bp, ok := denials[i]["broader_pattern"].(string); ok && bp != "" {
					t.Logf("denial[%d] broader_pattern = %q", i, bp)
				}
			}
		})
	}
}
