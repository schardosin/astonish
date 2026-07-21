package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
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
	data, err := resolveHTTPSource(nil, "GET:https://httpbin.org/get", nil)
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
			err := validateHTTPURL(tt.url, nil)
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
			_, err := resolveHTTPSource(nil, tt.spec, nil)
			if err == nil {
				t.Error("expected SSRF error, got nil")
			} else if !strings.Contains(err.Error(), "private/internal") {
				t.Errorf("expected private/internal error, got: %v", err)
			}
		})
	}
}

func TestIsHardBlockedIP(t *testing.T) {
	tests := []struct {
		ip   string
		hard bool
	}{
		{"127.0.0.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fe80::1", true},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"8.8.8.8", false},
		{"fd00::1", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid IP %q", tt.ip)
			}
			if got := isHardBlockedIP(ip); got != tt.hard {
				t.Errorf("isHardBlockedIP(%s) = %v, want %v", tt.ip, got, tt.hard)
			}
		})
	}
}

func TestCheckAppHTTPDial(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		decision PolicyDecision
		wantErr  string
	}{
		{"public unknown", "8.8.8.8", PolicyUnknown, ""},
		{"public allow", "8.8.8.8", PolicyAllow, ""},
		{"public deny", "8.8.8.8", PolicyDeny, "denied by network policy"},
		{"private unknown", "10.0.0.1", PolicyUnknown, "private/internal"},
		{"private allow", "10.0.0.1", PolicyAllow, ""},
		{"private deny", "10.0.0.1", PolicyDeny, "denied by network policy"},
		{"loopback allow still blocked", "127.0.0.1", PolicyAllow, "private/internal"},
		{"metadata allow still blocked", "169.254.169.254", PolicyAllow, "private/internal"},
		{"cgnat allow", "100.64.0.1", PolicyAllow, ""},
		{"ula allow", "fd00::1", PolicyAllow, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid IP %q", tt.ip)
			}
			err := checkAppHTTPDial("example.internal", 443, ip, tt.decision, 0)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestCheckAppHTTPDial_UnknownIncludesPolicyHint(t *testing.T) {
	err := checkAppHTTPDial("github.wdf.sap.corp", 443, net.ParseIP("10.239.45.169"), PolicyUnknown, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"private/internal", "network policy=unknown", "0 allow rules loaded"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q in error, got: %s", want, msg)
		}
	}
}

func TestValidateHTTPURL_AllowlistedPrivateIP(t *testing.T) {
	allow := &appHTTPPolicyState{
		ep: &EffectivePolicy{
			Team: []store.NetworkPolicyRule{{
				Host:   "10.0.0.1",
				Port:   80,
				Action: store.NetworkPolicyAllow,
			}},
		},
	}
	if err := validateHTTPURL("http://10.0.0.1/secret", allow); err != nil {
		t.Fatalf("expected allowlisted private IP to pass, got %v", err)
	}
	if err := validateHTTPURL("http://10.0.0.1/secret", nil); err == nil {
		t.Fatal("expected private IP without allow to fail")
	}
	allowAll := &appHTTPPolicyState{
		ep: &EffectivePolicy{
			Team: []store.NetworkPolicyRule{{
				Host:   "*",
				Port:   0,
				Action: store.NetworkPolicyAllow,
			}},
		},
	}
	if err := validateHTTPURL("http://127.0.0.1/admin", allowAll); err == nil {
		t.Fatal("expected loopback to stay blocked even when allowlisted")
	}
	if err := validateHTTPURL("http://169.254.169.254/latest/meta-data/", allowAll); err == nil {
		t.Fatal("expected metadata to stay blocked even when allowlisted")
	}
}

func TestResolveHTTPSource_NetworkPolicyDeny(t *testing.T) {
	orig := appHTTPPolicyLoader
	defer func() { appHTTPPolicyLoader = orig }()
	appHTTPPolicyLoader = func(*http.Request) *appHTTPPolicyState {
		return &appHTTPPolicyState{
			ep: &EffectivePolicy{
				Team: []store.NetworkPolicyRule{{
					Host:   "api.example.com",
					Port:   443,
					Action: store.NetworkPolicyDeny,
				}},
			},
		}
	}

	_, err := resolveHTTPSource(nil, "GET:https://api.example.com/data", nil)
	if err == nil {
		t.Fatal("expected deny error, got nil")
	}
	if !strings.Contains(err.Error(), "denied by network policy") {
		t.Fatalf("expected denied by network policy, got: %v", err)
	}
}

func TestResolveHTTPSource_NetworkPolicyAllowPrivate(t *testing.T) {
	origPolicy := appHTTPPolicyLoader
	origTransport := httpTransportFactory
	defer func() {
		appHTTPPolicyLoader = origPolicy
		httpTransportFactory = origTransport
	}()

	appHTTPPolicyLoader = func(*http.Request) *appHTTPPolicyState {
		return &appHTTPPolicyState{
			ep: &EffectivePolicy{
				Team: []store.NetworkPolicyRule{{
					Host:   "github.wdf.sap.corp",
					Port:   443,
					Action: store.NetworkPolicyAllow,
				}},
			},
		}
	}

	// Capture the policy decision seen by the transport without dialing.
	var seenDecision PolicyDecision
	var seenHost string
	var seenPort uint32
	httpTransportFactory = func(pol *appHTTPPolicyState) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			host := req.URL.Hostname()
			port := urlPort(req.URL)
			seenHost = host
			seenPort = port
			seenDecision = pol.Check(host, port)
			// Soft-private dial would be allowed; simulate success without network I/O.
			if err := checkAppHTTPDial(host, port, net.ParseIP("10.239.45.169"), seenDecision, pol.allowRulesLoaded()); err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})
	}

	data, err := resolveHTTPSource(nil, "POST:https://github.wdf.sap.corp/api/graphql", map[string]any{
		"body": map[string]any{"query": "{ viewer { login } }"},
	})
	if err != nil {
		t.Fatalf("expected allowlisted private host to succeed, got: %v", err)
	}
	if seenHost != "github.wdf.sap.corp" || seenPort != 443 {
		t.Fatalf("unexpected dial target %s:%d", seenHost, seenPort)
	}
	if seenDecision != PolicyAllow {
		t.Fatalf("expected PolicyAllow, got %v", seenDecision)
	}
	_ = data
}

func TestAppHTTPPolicyState_ConfigExtraEndpointsAllow(t *testing.T) {
	pol := &appHTTPPolicyState{
		ep: &EffectivePolicy{},
		configAllow: []config.NetworkEndpointConfig{{
			Host: "github.wdf.sap.corp",
			Port: 443,
		}},
	}
	if got := pol.Check("github.wdf.sap.corp", 443); got != PolicyAllow {
		t.Fatalf("expected PolicyAllow from ExtraEndpoints, got %v", got)
	}
	if got := pol.Check("github.wdf.sap.corp", 80); got != PolicyUnknown {
		t.Fatalf("expected PolicyUnknown for wrong port, got %v", got)
	}
	// Port 0 in config means 443 (OpenShell semantics).
	polZero := &appHTTPPolicyState{
		configAllow: []config.NetworkEndpointConfig{{
			Host: "internal.example.com",
			Port: 0,
		}},
	}
	if got := polZero.Check("internal.example.com", 443); got != PolicyAllow {
		t.Fatalf("expected port 0 to match 443, got %v", got)
	}
}

func TestAppHTTPPolicyState_DBDenyBeatsConfigAllow(t *testing.T) {
	pol := &appHTTPPolicyState{
		ep: &EffectivePolicy{
			Platform: []store.NetworkPolicyRule{{
				Host:   "github.wdf.sap.corp",
				Port:   443,
				Action: store.NetworkPolicyDeny,
			}},
		},
		configAllow: []config.NetworkEndpointConfig{{
			Host: "github.wdf.sap.corp",
			Port: 443,
		}},
	}
	if got := pol.Check("github.wdf.sap.corp", 443); got != PolicyDeny {
		t.Fatalf("expected PolicyDeny, got %v", got)
	}
}

func TestLoadAppHTTPPolicy_FailSoftPlatformListError(t *testing.T) {
	origCfg := appConfigExtraEndpoints
	defer func() { appConfigExtraEndpoints = origCfg }()
	appConfigExtraEndpoints = func() []config.NetworkEndpointConfig { return nil }

	failing := &stubNetworkPolicyStore{err: fmt.Errorf("platform list boom")}
	teamOK := &stubNetworkPolicyStore{rules: []store.NetworkPolicyRule{{
		Host:   "github.wdf.sap.corp",
		Port:   443,
		Action: store.NetworkPolicyAllow,
	}}}

	svc := &store.Services{
		PlatformNetworkPolicies: failing,
		TeamNetworkPolicies:     teamOK,
	}
	r := httptest.NewRequest(http.MethodPost, "/api/apps/data", nil)
	r = r.WithContext(store.WithServices(r.Context(), svc))

	pol := loadAppHTTPPolicy(r)
	if got := pol.Check("github.wdf.sap.corp", 443); got != PolicyAllow {
		t.Fatalf("expected team allow to survive platform List error, got %v (ep=%+v)", got, pol.ep)
	}
}

func TestResolveHTTPSource_ConfigExtraEndpointsAllowPrivate(t *testing.T) {
	origPolicy := appHTTPPolicyLoader
	origTransport := httpTransportFactory
	defer func() {
		appHTTPPolicyLoader = origPolicy
		httpTransportFactory = origTransport
	}()

	appHTTPPolicyLoader = func(*http.Request) *appHTTPPolicyState {
		return &appHTTPPolicyState{
			ep: &EffectivePolicy{},
			configAllow: []config.NetworkEndpointConfig{{
				Host: "github.wdf.sap.corp",
				Port: 443,
			}},
		}
	}

	httpTransportFactory = func(pol *appHTTPPolicyState) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			host := req.URL.Hostname()
			port := urlPort(req.URL)
			d := pol.Check(host, port)
			if err := checkAppHTTPDial(host, port, net.ParseIP("10.239.45.169"), d, pol.allowRulesLoaded()); err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})
	}

	if _, err := resolveHTTPSource(nil, "POST:https://github.wdf.sap.corp/api/graphql", nil); err != nil {
		t.Fatalf("expected config ExtraEndpoints to allow private host, got: %v", err)
	}
}

type stubNetworkPolicyStore struct {
	rules []store.NetworkPolicyRule
	err   error
}

func (s *stubNetworkPolicyStore) List(context.Context) ([]store.NetworkPolicyRule, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.rules, nil
}
func (s *stubNetworkPolicyStore) Get(context.Context, string) (*store.NetworkPolicyRule, error) {
	return nil, nil
}
func (s *stubNetworkPolicyStore) Save(context.Context, *store.NetworkPolicyRule) error { return nil }
func (s *stubNetworkPolicyStore) Delete(context.Context, string) error                 { return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// ── HTTP body unwrapping tests ───────────────────────────────────────

func TestExtractHTTPBodyAndHeaders(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		args        map[string]any
		wantBody    string            // JSON-encoded expected body, or "" for nil
		wantHeaders map[string]string // expected extracted headers
	}{
		{
			name:   "structured body+headers convention (POST)",
			method: "POST",
			args: map[string]any{
				"headers": map[string]any{
					"AI-Resource-Group": "default",
					"Content-Type":      "application/json",
				},
				"body": map[string]any{
					"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
					"max_tokens": float64(4096),
				},
			},
			wantBody: `{"max_tokens":4096,"messages":[{"content":"hello","role":"user"}]}`,
			wantHeaders: map[string]string{
				"AI-Resource-Group": "default",
				"Content-Type":      "application/json",
			},
		},
		{
			name:   "flat args convention — no body key (POST)",
			method: "POST",
			args: map[string]any{
				"query": "SELECT * FROM tasks",
			},
			wantBody:    `{"query":"SELECT * FROM tasks"}`,
			wantHeaders: map[string]string{},
		},
		{
			name:   "flat args with headers — headers stripped from body (POST)",
			method: "POST",
			args: map[string]any{
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
				"query": "INSERT INTO tasks VALUES (1, 'test')",
			},
			wantBody: `{"query":"INSERT INTO tasks VALUES (1, 'test')"}`,
			wantHeaders: map[string]string{
				"Authorization": "Bearer token",
			},
		},
		{
			name:        "nil args — no body (POST)",
			method:      "POST",
			args:        nil,
			wantBody:    "",
			wantHeaders: map[string]string{},
		},
		{
			name:   "GET method — no body even with args",
			method: "GET",
			args: map[string]any{
				"query": "test",
			},
			wantBody:    "",
			wantHeaders: map[string]string{},
		},
		{
			name:   "GET method with headers — headers extracted, no body",
			method: "GET",
			args: map[string]any{
				"headers": map[string]any{"X-API-Key": "secret"},
				"query":   "test",
			},
			wantBody:    "",
			wantHeaders: map[string]string{"X-API-Key": "secret"},
		},
		{
			name:   "PUT method with structured body",
			method: "PUT",
			args: map[string]any{
				"headers": map[string]any{"X-Custom": "val"},
				"body":    map[string]any{"name": "updated"},
			},
			wantBody:    `{"name":"updated"}`,
			wantHeaders: map[string]string{"X-Custom": "val"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyData, headers := extractHTTPBodyAndHeaders(tt.method, tt.args)

			// Check body
			if tt.wantBody == "" {
				if bodyData != nil {
					t.Errorf("expected nil body, got %v", bodyData)
				}
			} else {
				if bodyData == nil {
					t.Fatal("expected non-nil body, got nil")
				}
				gotJSON, err := json.Marshal(bodyData)
				if err != nil {
					t.Fatalf("failed to marshal body: %v", err)
				}
				if string(gotJSON) != tt.wantBody {
					t.Errorf("body mismatch:\n  want: %s\n  got:  %s", tt.wantBody, string(gotJSON))
				}
				// Verify "headers" key never leaks into body
				if m, ok := bodyData.(map[string]any); ok {
					if _, leaked := m["headers"]; leaked {
						t.Error("headers key leaked into the HTTP body")
					}
				}
			}

			// Check headers
			if len(tt.wantHeaders) == 0 {
				if len(headers) != 0 {
					t.Errorf("expected no headers, got %v", headers)
				}
			} else {
				for k, v := range tt.wantHeaders {
					if got, ok := headers[k]; !ok {
						t.Errorf("missing header %q", k)
					} else if got != v {
						t.Errorf("header %q = %q, want %q", k, got, v)
					}
				}
				if len(headers) != len(tt.wantHeaders) {
					t.Errorf("header count = %d, want %d: %v", len(headers), len(tt.wantHeaders), headers)
				}
			}
		})
	}
}

// ── Retry-on-401 with token invalidation tests ──────────────────────

// disableSSRF overrides the SSRF checks for testing with localhost test servers.
// Returns a cleanup function that restores the original values.
func disableSSRF() func() {
	origTransport := httpTransportFactory
	origValidator := httpURLValidator
	httpTransportFactory = func(_ *appHTTPPolicyState) http.RoundTripper { return http.DefaultTransport }
	httpURLValidator = func(_ string, _ *appHTTPPolicyState) error { return nil }
	return func() {
		httpTransportFactory = origTransport
		httpURLValidator = origValidator
	}
}

// oauthRetryCredentialStore is a test credential store that simulates an OAuth
// credential returning a stale token on first call and a fresh token after
// InvalidateToken is called.
type oauthRetryCredentialStore struct {
	resolveCount     atomic.Int32
	invalidateCount  atomic.Int32
	staleToken       string
	freshToken       string
}

func (s *oauthRetryCredentialStore) Get(_ context.Context, _ string) *store.Credential { return nil }
func (s *oauthRetryCredentialStore) Set(_ context.Context, _ string, _ *store.Credential) error {
	return nil
}
func (s *oauthRetryCredentialStore) Remove(_ context.Context, _ string) error { return nil }
func (s *oauthRetryCredentialStore) List(_ context.Context) map[string]store.CredentialType {
	return nil
}
func (s *oauthRetryCredentialStore) Count(_ context.Context) int { return 0 }
func (s *oauthRetryCredentialStore) Resolve(_ context.Context, _ string) (string, string, error) {
	n := s.resolveCount.Add(1)
	if n == 1 {
		return "Authorization", "Bearer " + s.staleToken, nil
	}
	return "Authorization", "Bearer " + s.freshToken, nil
}
func (s *oauthRetryCredentialStore) InvalidateToken(_ context.Context, _ string) {
	s.invalidateCount.Add(1)
}
func (s *oauthRetryCredentialStore) SetSecret(_ context.Context, _, _ string) error { return nil }
func (s *oauthRetryCredentialStore) SetSecretBatch(_ context.Context, _ map[string]string) error {
	return nil
}
func (s *oauthRetryCredentialStore) GetSecret(_ context.Context, _ string) string  { return "" }
func (s *oauthRetryCredentialStore) RemoveSecret(_ context.Context, _ string) error { return nil }
func (s *oauthRetryCredentialStore) HasSecrets(_ context.Context) bool              { return false }
func (s *oauthRetryCredentialStore) SecretCount(_ context.Context) int              { return 0 }
func (s *oauthRetryCredentialStore) ListSecrets(_ context.Context) []string         { return nil }
func (s *oauthRetryCredentialStore) Reload(_ context.Context) error                 { return nil }

func TestResolveHTTPSource_RetryOn401(t *testing.T) {
	cleanup := disableSSRF()
	defer cleanup()

	// Set up a test server that returns 401 on the first request (stale token)
	// and 200 on the second request (fresh token).
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		auth := r.Header.Get("Authorization")

		if n == 1 {
			// First request comes with stale token — reject it
			if auth != "Bearer stale-token" {
				t.Errorf("first request: expected stale token, got %q", auth)
			}
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"token expired"}`))
			return
		}

		// Second request should have fresh token
		if auth != "Bearer fresh-token" {
			t.Errorf("second request: expected fresh token, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","data":"hello"}`))
	}))
	defer ts.Close()

	// Create a mock credential store that returns stale then fresh tokens
	credStore := &oauthRetryCredentialStore{
		staleToken: "stale-token",
		freshToken: "fresh-token",
	}

	// We need to build a fake http.Request with the credential store accessible.
	// The resolveHTTPSource function calls effectiveCredentialStore(r), so we need
	// to set up the context properly. Instead, we'll test by directly overriding
	// the credential resolution. Since resolveHTTPSource uses effectiveCredentialStore,
	// we need a slightly different approach.
	//
	// For this test, we'll set the personal-mode credential store via the API singleton.
	// But since resolveHTTPSource directly calls effectiveCredentialStore(r) which for
	// platform mode reads from the request context, let's use a request with Services.
	r := httptest.NewRequest("GET", "/", nil)
	ctx := store.WithServices(r.Context(), &store.Services{
		Mode:        store.ModePlatform,
		Credentials: credStore,
	})
	r = r.WithContext(ctx)

	spec := "GET:" + ts.URL + "/data@my-oauth-cred"
	result, err := resolveHTTPSource(r, spec, nil)
	if err != nil {
		t.Fatalf("resolveHTTPSource failed: %v", err)
	}

	// Verify the result is from the successful retry
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T: %v", result, result)
	}
	if m["status"] != "ok" {
		t.Errorf("unexpected result: %v", m)
	}

	// Verify the credential store interactions
	if got := credStore.resolveCount.Load(); got != 2 {
		t.Errorf("expected 2 Resolve calls, got %d", got)
	}
	if got := credStore.invalidateCount.Load(); got != 1 {
		t.Errorf("expected 1 InvalidateToken call, got %d", got)
	}
	if got := requestCount.Load(); got != 2 {
		t.Errorf("expected 2 HTTP requests to backend, got %d", got)
	}
}

func TestResolveHTTPSource_NoRetryOnNonCredential401(t *testing.T) {
	cleanup := disableSSRF()
	defer cleanup()

	// When no credential is used, a 401 should NOT trigger a retry.
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()

	spec := "GET:" + ts.URL + "/data"
	_, err := resolveHTTPSource(nil, spec, nil)
	if err == nil {
		t.Fatal("expected error from 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got: %v", err)
	}
	if got := requestCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 HTTP request (no retry), got %d", got)
	}
}

func TestResolveHTTPSource_RetryStill401(t *testing.T) {
	cleanup := disableSSRF()
	defer cleanup()

	// If the retry also returns 401, we should get an error (no infinite retry).
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"still unauthorized"}`))
	}))
	defer ts.Close()

	credStore := &oauthRetryCredentialStore{
		staleToken: "bad-token",
		freshToken: "also-bad-token",
	}

	r := httptest.NewRequest("GET", "/", nil)
	ctx := store.WithServices(r.Context(), &store.Services{
		Mode:        store.ModePlatform,
		Credentials: credStore,
	})
	r = r.WithContext(ctx)

	spec := "GET:" + ts.URL + "/data@my-oauth-cred"
	_, err := resolveHTTPSource(r, spec, nil)
	if err == nil {
		t.Fatal("expected error from persistent 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got: %v", err)
	}
	// Should have tried exactly twice (initial + one retry)
	if got := requestCount.Load(); got != 2 {
		t.Errorf("expected 2 HTTP requests (initial + retry), got %d", got)
	}
}

func TestResolveHTTPSource_NoRetryOn403(t *testing.T) {
	cleanup := disableSSRF()
	defer cleanup()

	// 403 Forbidden should NOT trigger a token retry (only 401 does).
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer ts.Close()

	credStore := &oauthRetryCredentialStore{
		staleToken: "valid-token",
		freshToken: "valid-token",
	}

	r := httptest.NewRequest("GET", "/", nil)
	ctx := store.WithServices(r.Context(), &store.Services{
		Mode:        store.ModePlatform,
		Credentials: credStore,
	})
	r = r.WithContext(ctx)

	spec := "GET:" + ts.URL + "/data@my-oauth-cred"
	_, err := resolveHTTPSource(r, spec, nil)
	if err == nil {
		t.Fatal("expected error from 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
	// Only one request — no retry on 403
	if got := requestCount.Load(); got != 1 {
		t.Errorf("expected 1 HTTP request (no retry on 403), got %d", got)
	}
	// InvalidateToken should NOT have been called
	if got := credStore.invalidateCount.Load(); got != 0 {
		t.Errorf("expected 0 InvalidateToken calls on 403, got %d", got)
	}
}
