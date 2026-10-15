package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

// memLinkCodeStore is a minimal in-memory store.LinkCodeStore for unit tests.
type memLinkCodeStore struct {
	mu    sync.Mutex
	codes map[string]*store.LinkCode
}

func newMemLinkCodeStore() *memLinkCodeStore {
	return &memLinkCodeStore{codes: make(map[string]*store.LinkCode)}
}

func (m *memLinkCodeStore) Generate(_ context.Context, code, userID, email, channel string) error {
	return m.GenerateWithTTL(context.Background(), code, userID, email, channel, 5*time.Minute)
}

func (m *memLinkCodeStore) GenerateWithTTL(_ context.Context, code, userID, email, channel string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.codes[code] = &store.LinkCode{
		Code:      code,
		UserID:    userID,
		Email:     email,
		Channel:   channel,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	return nil
}

func (m *memLinkCodeStore) Peek(_ context.Context, code string) (*store.LinkCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lc, ok := m.codes[code]
	if !ok || time.Now().After(lc.ExpiresAt) {
		return nil, nil
	}
	return lc, nil
}

func (m *memLinkCodeStore) Consume(_ context.Context, code string) (*store.LinkCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lc, ok := m.codes[code]
	if !ok || time.Now().After(lc.ExpiresAt) {
		return nil, nil
	}
	delete(m.codes, code)
	return lc, nil
}

func (m *memLinkCodeStore) Cleanup(_ context.Context) error { return nil }

type mockLinkCodeBackend struct {
	mockPlatformBackendForChannelInfo
	lcStore store.LinkCodeStore
}

func (m *mockLinkCodeBackend) LinkCodes() store.LinkCodeStore { return m.lcStore }

func TestEnsureLinkCodeStore_LazyInitFromPlatformBackend(t *testing.T) {
	origStore := linkCodeStore
	origBackend := platformBackendInstance
	defer func() {
		linkCodeStoreMu.Lock()
		linkCodeStore = origStore
		linkCodeStoreMu.Unlock()
		platformBackendInstance = origBackend
	}()

	linkCodeStoreMu.Lock()
	linkCodeStore = nil
	linkCodeStoreMu.Unlock()

	platformBackendInstance = &mockLinkCodeBackend{lcStore: newMemLinkCodeStore()}

	got := ensureLinkCodeStore()
	if got == nil {
		t.Fatal("expected ensureLinkCodeStore to lazily initialize from platform backend")
	}
	if GetLinkCodeStore() == nil {
		t.Fatal("expected SetLinkCodeStore to be called by ensureLinkCodeStore")
	}
}

func TestEnsureLinkCodeStore_ReturnsExisting(t *testing.T) {
	origStore := linkCodeStore
	origBackend := platformBackendInstance
	defer func() {
		linkCodeStoreMu.Lock()
		linkCodeStore = origStore
		linkCodeStoreMu.Unlock()
		platformBackendInstance = origBackend
	}()

	existing := NewLinkCodeStore()
	SetLinkCodeStore(existing)

	got := ensureLinkCodeStore()
	if got != existing {
		t.Fatal("expected ensureLinkCodeStore to return the already-registered store")
	}
}

func TestGenerateLinkCode_Uses16ByteRawURLToken(t *testing.T) {
	code := generateLinkCode()
	if len(code) != base64.RawURLEncoding.EncodedLen(16) {
		t.Fatalf("token length = %d, want %d", len(code), base64.RawURLEncoding.EncodedLen(16))
	}
	decoded, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if len(decoded) != 16 {
		t.Fatalf("decoded token length = %d, want 16", len(decoded))
	}
	if strings.ContainsAny(code, "+/=") {
		t.Fatalf("token is not raw URL-safe base64: %q", code)
	}
}

func TestGenerateLinkCode_LookupsAreCaseSensitive(t *testing.T) {
	linkCodes := NewLinkCodeStore()
	code, err := linkCodes.Generate(context.Background(), "user", "user@example.com", "email:user@example.com")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	altered := strings.ToUpper(code)
	if altered == code {
		altered = strings.ToLower(code)
	}
	if linkCodes.Peek(context.Background(), altered) != nil {
		t.Fatal("case-altered code must not resolve")
	}
	if linkCodes.Peek(context.Background(), code) == nil {
		t.Fatal("exact code must resolve")
	}
}

func TestVerifyEmailCode_OwnershipAndChannelCheckedBeforeConsume(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		codeUser string
		channel  string
		want     int
	}{
		{name: "other user", userID: "user-b", codeUser: "user-a", channel: "email:user@example.com", want: http.StatusBadRequest},
		{name: "non-email channel", userID: "user-a", codeUser: "user-a", channel: "telegram", want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linkCodes := NewLinkCodeStore()
			code, err := linkCodes.Generate(context.Background(), tt.codeUser, "user@example.com", tt.channel)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			orig := GetLinkCodeStore()
			defer SetLinkCodeStore(orig)
			SetLinkCodeStore(linkCodes)

			req := httptest.NewRequest(http.MethodPost, "/api/user/channels/verify-email-code", strings.NewReader(`{"code":"`+code+`"}`))
			req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{ID: tt.userID}))
			w := httptest.NewRecorder()
			handleVerifyEmailCode(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.want, w.Body.String())
			}
			if linkCodes.Peek(context.Background(), code) == nil {
				t.Fatal("rejected verification must not consume the code")
			}
		})
	}
}

func TestVerifyEmailCode_ConsumesAfterOwnershipValidation(t *testing.T) {
	linkCodes := NewLinkCodeStore()
	code, err := linkCodes.Generate(context.Background(), "user-a", "user@example.com", "email:user@example.com")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	orig := GetLinkCodeStore()
	defer SetLinkCodeStore(orig)
	SetLinkCodeStore(linkCodes)

	// No platform backend is configured, so validation succeeds and the later
	// persistence check fails; this still proves the code was consumed only then.
	req := httptest.NewRequest(http.MethodPost, "/api/user/channels/verify-email-code", strings.NewReader(`{"code":"`+code+`"}`))
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{ID: "user-a"}))
	w := httptest.NewRecorder()
	handleVerifyEmailCode(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if linkCodes.Peek(context.Background(), code) != nil {
		t.Fatal("validated email code must be consumed")
	}
}

func TestGenerateLinkCode_RequiresPreselectedIdentity(t *testing.T) {
	old := GetLinkCodeStore()
	defer SetLinkCodeStore(old)
	codes := NewLinkCodeStore()
	SetLinkCodeStore(codes)
	for _, tt := range []struct {
		body, binding string
		status        int
	}{
		{`{"channel_type":"telegram"}`, "", http.StatusBadRequest},
		{`{"channel_type":"slack","expected_identity":{"user_id":"U123"}}`, "", http.StatusBadRequest},
		{`{"channel_type":"telegram","expected_identity":{"user_id":"12345"}}`, "link:v1:telegram:12345", http.StatusOK},
		{`{"channel_type":"slack","expected_identity":{"workspace_id":"T123","user_id":"U123"}}`, "link:v1:slack:T123:U123", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/user/channels/link-code", strings.NewReader(tt.body))
		req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{ID: "owner", Email: "owner@example.com"}))
		w := httptest.NewRecorder()
		handleGenerateLinkCode(w, req)
		if w.Code != tt.status {
			t.Fatalf("%s: status=%d body=%s", tt.body, w.Code, w.Body.String())
		}
		if tt.status == http.StatusOK {
			var response struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			pending := codes.Peek(req.Context(), response.Code)
			if pending == nil || pending.Channel != tt.binding || pending.UserID != "owner" {
				t.Fatalf("bad persisted binding: %+v", pending)
			}
		}
	}
}

type verifyUserChannelBackend struct {
	mockPlatformBackendForChannelInfo
	channels store.UserChannelStore
}

func (m *verifyUserChannelBackend) UserChannels() store.UserChannelStore { return m.channels }

type verifySpyUserChannelStore struct {
	store.UserChannelStore
	channel     *store.UserChannel
	verifyCalls int
}

func (s *verifySpyUserChannelStore) Verify(_ context.Context, id string) error {
	s.verifyCalls++
	if s.channel.ID == id {
		s.channel.Verified = true
	}
	return nil
}

func TestVerifyUserChannel_DirectVerificationRejected(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/user/channels/link-id/verify", nil)
		w := httptest.NewRecorder()

		handleVerifyUserChannel(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("legacy unverified record remains unverified", func(t *testing.T) {
		channel := &store.UserChannel{ID: "link-id", UserID: "user-a", Verified: false, Enabled: false}
		channels := &verifySpyUserChannelStore{channel: channel}
		origBackend := platformBackendInstance
		platformBackendInstance = &verifyUserChannelBackend{channels: channels}
		defer func() { platformBackendInstance = origBackend }()

		req := httptest.NewRequest(http.MethodPost, "/api/user/channels/link-id/verify", nil)
		req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{ID: "user-a"}))
		w := httptest.NewRecorder()

		handleVerifyUserChannel(w, req)

		if w.Code != http.StatusGone {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusGone, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "link-code flow") {
			t.Fatalf("response should direct user to proof-based linking: %s", w.Body.String())
		}
		if channels.verifyCalls != 0 || channel.Verified || channel.Enabled {
			t.Fatalf("legacy record was changed without proof: verify calls=%d, channel=%+v", channels.verifyCalls, channel)
		}
	})
}

func TestVerifyEmailCode_DirectLinkRejected(t *testing.T) {
	w := httptest.NewRecorder()
	handleLinkUserChannel(w, httptest.NewRequest(http.MethodPost, "/api/user/channels", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
