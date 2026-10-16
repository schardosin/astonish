package slack

import (
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

type deniedOAuthTransport struct{}

func (deniedOAuthTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected OAuth exchange")
}

func TestOAuthHTTPHandlersDisabled(t *testing.T) {
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: deniedOAuthTransport{}}
	t.Cleanup(func() { http.DefaultClient = old })
	h := NewOAuthHandler(&OAuthConfig{ClientID: "unused"}, nil, New(&Config{}, log.Default()))
	for name, handler := range map[string]http.Handler{"install": h.InstallHandler(), "callback": h.CallbackHandler()} {
		t.Run(name, func(t *testing.T) {
			for _, query := range []string{"", "?state=123&code=untrusted", "?error=access_denied"} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://example.com/slack/"+name+query, nil))
				if w.Code != http.StatusNotFound {
					t.Errorf("status = %d, want 404", w.Code)
				}
				if w.Header().Get("Location") != "" || w.Header().Get("Set-Cookie") != "" {
					t.Error("disabled OAuth handler must not initiate authorization")
				}
				if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Error("disabled OAuth handler must not render HTML")
				}
			}
		})
	}
}
