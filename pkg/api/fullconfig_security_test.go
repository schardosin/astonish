package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
)

func fullConfigSecuritySetup(t *testing.T, proxy, cdp string) *config.AppConfig {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	cfg := &config.AppConfig{Browser: config.BrowserAppConfig{Proxy: proxy, RemoteCDPURL: cdp}}
	if err := config.SaveAppConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestFullConfigBrowserSecretResponse(t *testing.T) {
	proxy := "http://proxy-user:proxy-password@proxy.example:8080"
	cdp := "wss://cdp-user:cdp-password@browser.example/devtools?token=cdp-token&custom=custom-secret#fragment-secret"
	cfg := fullConfigSecuritySetup(t, proxy, cdp)
	cfg.Browser.KasmVNCPassword = "vnc-secret"
	if err := config.SaveAppConfig(cfg); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	GetFullConfigHandler(w, httptest.NewRequest(http.MethodGet, "/api/settings/full", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	for _, secret := range []string{"proxy-user", "proxy-password", "cdp-user", "cdp-password", "cdp-token", "custom-secret", "fragment-secret", "vnc-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("response discloses %s", secret)
		}
	}
	var response FullConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Browser.Proxy, "proxy.example:8080") || !strings.Contains(response.Browser.RemoteCDPURL, "browser.example/devtools") {
		t.Error("redaction lost nonsecret destination")
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Browser.Proxy != proxy || cfg.Browser.RemoteCDPURL != cdp || cfg.Browser.KasmVNCPassword != "vnc-secret" {
		t.Error("GET mutated stored runtime credentials")
	}
}

func TestFullConfigBrowserMaskedUpdates(t *testing.T) {
	for _, field := range []string{"proxy", "remote_cdp_url"} {
		t.Run(field, func(t *testing.T) {
			initial := "https://user:password@example.test:8080/path?token=token-secret&token=second-secret&mode=fast#fragment"
			for _, tc := range []struct {
				name, initial, value string
				omit, roundtrip      bool
				status               int
				want                 string
			}{
				{name: "roundtrip", initial: initial, roundtrip: true, status: 200, want: initial},
				{name: "omitted", initial: initial, omit: true, status: 200, want: initial},
				{name: "explicit-clear", initial: initial, value: "", status: 200, want: ""},
				{name: "replacement", initial: initial, value: "https://new:credential@new.example/path", status: 200, want: "https://new:credential@new.example/path"},
				{name: "missing-placeholder", value: "****", status: 400},
				{name: "changed-host", initial: initial, value: "https://****:****@attacker.example:8080/path?token=****&token=****&mode=****#****", status: 400, want: initial},
				{name: "changed-path", initial: initial, value: "https://****:****@example.test:8080/other?token=****&token=****&mode=****#****", status: 400, want: initial},
				{name: "encoded-placeholder", value: "https://example.test?token=%2A%2A%2A%2A", status: 400},
				{name: "changed-scheme", initial: initial, value: "http://****:****@example.test:8080/path?token=****", status: 400, want: initial},
				{name: "changed-port", initial: initial, value: "https://****:****@example.test:9090/path?token=****", status: 400, want: initial},
				{name: "changed-query", initial: initial, value: "https://****:****@example.test:8080/path?other=****", status: 400, want: initial},
			} {
				t.Run(tc.name, func(t *testing.T) {
					proxy, cdp := "", ""
					if field == "proxy" {
						proxy = tc.initial
					} else {
						cdp = tc.initial
					}
					cfg := fullConfigSecuritySetup(t, proxy, cdp)
					cfg.Browser.KasmVNCPort = 6902
					cfg.Browser.KasmVNCPassword = "vnc-secret"
					if err := config.SaveAppConfig(cfg); err != nil {
						t.Fatal(err)
					}
					value := tc.value
					if tc.roundtrip {
						w := httptest.NewRecorder()
						GetFullConfigHandler(w, httptest.NewRequest(http.MethodGet, "/api/settings/full", nil))
						var response map[string]json.RawMessage
						if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
							t.Fatal(err)
						}
						var browser map[string]json.RawMessage
						if err := json.Unmarshal(response["browser"], &browser); err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal(browser[field], &value); err != nil {
							t.Fatal(err)
						}
					}
					browser := map[string]any{"viewport_width": 1200}
					if !tc.omit {
						browser[field] = value
					}
					payload, _ := json.Marshal(map[string]any{"browser": browser})
					path, _ := config.GetConfigPath()
					before, _ := os.ReadFile(path)
					w := httptest.NewRecorder()
					UpdateFullConfigHandler(w, httptest.NewRequest(http.MethodPut, "/api/settings/full", strings.NewReader(string(payload))))
					if w.Code != tc.status {
						t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
					}
					cfg, err := config.LoadAppConfig()
					if err != nil {
						t.Fatal(err)
					}
					if cfg.Browser.KasmVNCPort != 6902 || cfg.Browser.KasmVNCPassword != "vnc-secret" {
						t.Error("URL update lost runtime handoff settings")
					}
					got := cfg.Browser.Proxy
					if field == "remote_cdp_url" {
						got = cfg.Browser.RemoteCDPURL
					}
					if got != tc.want {
						t.Errorf("stored URL = %q, want %q", got, tc.want)
					}
					if tc.status != 200 {
						after, _ := os.ReadFile(path)
						if string(before) != string(after) {
							t.Error("rejected update mutated config")
						}
					}
				})
			}
		})
	}
}

func TestFullConfigBrowserURLRedactionEdges(t *testing.T) {
	for _, raw := range []string{
		"https://user:p%40ss@example.test/path?token=a%2fb&token=a+b&empty=#secret",
		"https://user@example.test/path",
		"https://example.test/path?bad=%zz&token=secret",
		"https://example.test/path?token=secret;other=secret",
		"http://user:secret@%invalid",
		"opaque:secret",
	} {
		t.Run(raw, func(t *testing.T) {
			masked := redactBrowserURL(raw)
			if strings.Contains(masked, "secret") || strings.Contains(masked, "user") || strings.Contains(masked, "p%40ss") {
				t.Fatalf("redaction disclosed credentials: %q", masked)
			}
			restored, err := restoreBrowserURL(&masked, raw)
			if err != nil || restored != raw {
				t.Fatalf("lossless roundtrip failed: %q, %v", restored, err)
			}
		})
	}
	for _, raw := range []string{"", "socks5://localhost:1080", "ws://localhost:9222/devtools/browser/id"} {
		if got := redactBrowserURL(raw); got != raw {
			t.Errorf("nonsecret URL changed: %q", got)
		}
	}
}

func TestFullConfigBrowserPlatformAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		user   *PlatformUser
		status int
	}{
		{"anonymous", nil, http.StatusUnauthorized},
		{"org-owner", &PlatformUser{Role: "owner"}, http.StatusForbidden},
		{"superadmin", &PlatformUser{PlatformRole: "superadmin"}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fullConfigSecuritySetup(t, "http://user:password@proxy.example", "")
			req := httptest.NewRequest(http.MethodPut, "/api/settings/full", strings.NewReader(`{"browser":{"proxy":""}}`))
			ctx := store.WithServices(req.Context(), &store.Services{Mode: store.ModePlatform})
			if tc.user != nil {
				ctx = WithPlatformUser(ctx, tc.user)
			}
			req = req.WithContext(ctx)
			req.RemoteAddr = "127.0.0.1:1234"
			path, _ := config.GetConfigPath()
			before, _ := os.ReadFile(path)
			w := httptest.NewRecorder()
			UpdateFullConfigHandler(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if tc.status != http.StatusOK {
				after, _ := os.ReadFile(path)
				if string(before) != string(after) {
					t.Error("denied update changed config")
				}
			}
		})
	}
}

func TestFullConfigAuthorizationBeforeBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/settings/full", strings.NewReader("not-json"))
	req = req.WithContext(store.WithServices(req.Context(), &store.Services{Mode: store.ModePlatform}))
	w := httptest.NewRecorder()
	UpdateFullConfigHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 before decoding", w.Code)
	}
}

func TestFullConfigEmbeddingMaskedUpdates(t *testing.T) {
	for _, tc := range []struct {
		name, key, body string
		status          int
		want            string
	}{
		{"omitted", "embedding-secret", `{"enabled":true}`, 200, "embedding-secret"},
		{"clear", "embedding-secret", `{"embedding":{"api_key":""}}`, 200, ""},
		{"roundtrip", "embedding-secret", `{"embedding":{"provider":"openai","base_url":"https://api.example","api_key":"****cret"}}`, 200, "embedding-secret"},
		{"missing", "", `{"embedding":{"api_key":"****"}}`, 400, ""},
		{"changed-destination", "embedding-secret", `{"embedding":{"base_url":"https://attacker.example","api_key":"****cret"}}`, 400, "embedding-secret"},
		{"changed-provider-omitted-key", "embedding-secret", `{"embedding":{"provider":"other"}}`, 400, "embedding-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fullConfigSecuritySetup(t, "", "")
			cfg.Memory.Embedding = config.EmbeddingConfig{Provider: "openai", BaseURL: "https://api.example", APIKey: tc.key}
			if err := config.SaveAppConfig(cfg); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			UpdateFullConfigHandler(w, httptest.NewRequest(http.MethodPut, "/api/settings/full", strings.NewReader(`{"memory":`+tc.body+`}`)))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			cfg, err := config.LoadAppConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Memory.Embedding.APIKey != tc.want {
				t.Errorf("stored key = %q, want %q", cfg.Memory.Embedding.APIKey, tc.want)
			}
		})
	}
}
