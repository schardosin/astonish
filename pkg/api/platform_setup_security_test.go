package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/config"
)

type setupUnreadBody struct{ reads int }

func (b *setupUnreadBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*setupUnreadBody) Close() error               { return nil }

func TestPlatformSetupRequiresExplicitBootstrapAuthority(t *testing.T) {
	for _, handler := range []struct {
		name string
		fn   http.HandlerFunc
	}{
		{"postgres", PlatformInitHandler}, {"sqlite", SQLitePlatformInitHandler},
	} {
		t.Run(handler.name, func(t *testing.T) {
			for _, tc := range []struct{ name, configured, supplied, remote string }{
				{"unset", "", "", "192.0.2.1:1234"},
				{"loopback-not-authority", "", "", "127.0.0.1:1234"},
				{"ipv6-loopback-not-authority", "", "", "[::1]:1234"},
				{"missing", "unit-bootstrap-secret", "", "127.0.0.1:1234"},
				{"wrong", "unit-bootstrap-secret", "wrong", "192.0.2.1:1234"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					cleanup := setupTestConfigDir(t, &config.AppConfig{})
					defer cleanup()
					t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", tc.configured)
					body := &setupUnreadBody{}
					r := httptest.NewRequest(http.MethodPost, "/api/platform/init", body)
					r.RemoteAddr = tc.remote
					r.Header.Set("X-Astonish-Bootstrap-Token", tc.supplied)
					r.Header.Set("X-Forwarded-For", "127.0.0.1")
					w := httptest.NewRecorder()
					handler.fn(w, r)
					if w.Code != http.StatusForbidden || body.reads != 0 {
						t.Fatalf("unauthorized setup: status=%d body reads=%d response=%s", w.Code, body.reads, w.Body.String())
					}
					after, err := config.LoadAppConfig()
					if err != nil || after.Storage.Backend != "" || after.Storage.Auth.JWTSecret != "" {
						t.Fatalf("unauthorized setup changed configuration: err=%v", err)
					}
				})
			}
		})
	}
}

func TestPlatformSetupExplicitAuthorityReachesValidation(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{})
	defer cleanup()
	t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "unit-bootstrap-secret")
	for _, handler := range []http.HandlerFunc{PlatformInitHandler, SQLitePlatformInitHandler} {
		r := httptest.NewRequest(http.MethodPost, "/api/platform/init", strings.NewReader(`invalid`))
		r.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("authorized validation status=%d: %s", w.Code, w.Body.String())
		}
	}
}

// setupBlockingBody marks entry after the configured-backend check. The test
// persists configuration while the first handler is inside the critical section,
// without creating any database or contacting PostgreSQL.
type setupBlockingBody struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *setupBlockingBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}
func (*setupBlockingBody) Close() error { return nil }

func TestPlatformSetupSerializesBothEntryPointsAndRechecksConfig(t *testing.T) {
	handlers := []struct {
		name string
		fn   http.HandlerFunc
	}{
		{"postgres", PlatformInitHandler}, {"sqlite", SQLitePlatformInitHandler},
	}
	for _, first := range handlers {
		for _, second := range handlers {
			t.Run(first.name+"-then-"+second.name, func(t *testing.T) {
				cleanup := setupTestConfigDir(t, &config.AppConfig{})
				defer cleanup()
				t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "unit-bootstrap-secret")
				blocking := &setupBlockingBody{entered: make(chan struct{}), release: make(chan struct{})}
				var release sync.Once
				unblock := func() { release.Do(func() { close(blocking.release) }) }
				defer unblock()
				r := httptest.NewRequest(http.MethodPost, "/api/platform/init", blocking)
				r.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
				firstDone := make(chan int, 1)
				go func() { w := httptest.NewRecorder(); first.fn(w, r); firstDone <- w.Code }()
				select {
				case <-blocking.entered:
				case <-time.After(3 * time.Second):
					t.Fatal("first setup never reached body")
				}
				// Directly assert that the shared lock spans body decoding and therefore
				// both the earlier config check and the later bootstrap/persistence work.
				if platformInitMu.TryLock() {
					platformInitMu.Unlock()
					t.Error("setup released lock before completing")
				}
				unread := &setupUnreadBody{}
				next := httptest.NewRequest(http.MethodPost, "/api/platform/init", unread)
				next.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
				secondDone := make(chan int, 1)
				go func() { w := httptest.NewRecorder(); second.fn(w, next); secondDone <- w.Code }()
				cfg := &config.AppConfig{}
				cfg.Storage.Backend = first.name
				cfg.Storage.Auth.JWTSecret = "persisted-unit-jwt"
				if err := config.SaveAppConfig(cfg); err != nil {
					t.Fatal(err)
				}
				unblock()
				if status := <-firstDone; status != http.StatusBadRequest {
					t.Errorf("first status=%d", status)
				}
				if status := <-secondDone; status != http.StatusConflict {
					t.Errorf("second status=%d", status)
				}
				if unread.reads != 0 {
					t.Error("second setup did not reject before reading body")
				}
				after, err := config.LoadAppConfig()
				if err != nil || after.Storage.Backend != first.name || after.Storage.Auth.JWTSecret != "persisted-unit-jwt" {
					t.Fatalf("saved config changed: %v", err)
				}
			})
		}
	}
}
