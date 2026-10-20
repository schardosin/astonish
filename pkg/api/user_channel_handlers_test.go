package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
		{`{"channel_type":"telegram","expected_identity":{"user_id":"12345"}}`, "link:v1:telegram:12345", http.StatusOK},
		{`{"channel_type":"slack","expected_identity":{"user_id":"U123"}}`, "link:v1:slack:U123", http.StatusOK},
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

type emailPersistenceStore struct {
	store.UserChannelStore
	channel              store.UserChannel
	verifyErr, updateErr error
	verifies, updates    int
}

func (s *emailPersistenceStore) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	ch := s.channel
	return &ch, nil
}
func (s *emailPersistenceStore) Verify(context.Context, string) error {
	s.verifies++
	if s.verifyErr != nil {
		return s.verifyErr
	}
	s.channel.Verified = true
	return nil
}
func (s *emailPersistenceStore) Update(_ context.Context, ch *store.UserChannel) error {
	s.updates++
	if s.updateErr != nil {
		return s.updateErr
	}
	s.channel = *ch
	return nil
}

func TestVerifyEmailCode_PersistenceAndReplay(t *testing.T) {
	for _, failure := range []string{"verify", "update", "success"} {
		t.Run(failure, func(t *testing.T) {
			owner := "persistence-" + failure
			s := &emailPersistenceStore{channel: store.UserChannel{ID: "link", UserID: owner, ExternalID: "a@example.com"}}
			if failure == "verify" {
				s.verifyErr = errors.New("write failed")
			}
			if failure == "update" {
				s.updateErr = errors.New("write failed")
			}
			oldBackend := platformBackendInstance
			platformBackendInstance = &verifyUserChannelBackend{channels: s}
			defer func() { platformBackendInstance = oldBackend }()
			oldCodes := GetLinkCodeStore()
			codes := NewLinkCodeStore()
			SetLinkCodeStore(codes)
			defer SetLinkCodeStore(oldCodes)
			code, err := codes.Generate(context.Background(), owner, "a@example.com", "email:a@example.com")
			if err != nil {
				t.Fatal(err)
			}
			request := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"code":"`+code+`"}`))
				r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: owner}))
				w := httptest.NewRecorder()
				handleVerifyEmailCode(w, r)
				return w
			}
			w := request()
			want := http.StatusInternalServerError
			if failure == "success" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Errorf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if failure == "verify" && (s.updates != 0 || s.channel.Enabled) {
				t.Error("enabled after failed verification")
			}
			if failure == "success" && (!s.channel.Verified || !s.channel.Enabled) {
				t.Error("success not persisted")
			}
			if w = request(); w.Code != http.StatusBadRequest {
				t.Fatalf("replay status=%d", w.Code)
			}
			if s.verifies != 1 {
				t.Fatalf("verify calls=%d", s.verifies)
			}
		})
	}
}

func TestVerifyEmailCode_ExpiredAndConcurrentConsume(t *testing.T) {
	old := GetLinkCodeStore()
	defer SetLinkCodeStore(old)
	backing := newMemLinkCodeStore()
	codes := NewDBLinkCodeBackend(backing)
	SetLinkCodeStore(codes)
	if err := backing.GenerateWithTTL(context.Background(), "expired", "expired-owner", "a@example.com", "email:a@example.com", -time.Second); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"code":"expired"}`))
	r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: "expired-owner"}))
	w := httptest.NewRecorder()
	handleVerifyEmailCode(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expired status=%d", w.Code)
	}
	code, err := codes.Generate(context.Background(), "concurrent-owner", "a@example.com", "email:a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	winners := make(chan bool, 20)
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); winners <- codes.Consume(context.Background(), code) != nil }()
	}
	wg.Wait()
	close(winners)
	count := 0
	for won := range winners {
		if won {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("consume winners=%d", count)
	}
}

type emailCreationStore struct {
	store.UserChannelStore
	linkErr error
	links   int
	created *store.UserChannel
}

func (s *emailCreationStore) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	return nil, nil
}

func (s *emailCreationStore) Link(_ context.Context, ch *store.UserChannel) error {
	s.links++
	if s.linkErr != nil {
		return s.linkErr
	}
	copy := *ch
	s.created = &copy
	return nil
}

func (s *emailCreationStore) ListByChannelType(context.Context, string) ([]*store.UserChannel, error) {
	if s.created == nil {
		return nil, nil
	}
	return []*store.UserChannel{s.created}, nil
}

func TestVerifyEmailCode_NewLinkPersistenceAndReplay(t *testing.T) {
	for _, failure := range []bool{true, false} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			owner := "new-link-" + name
			const internalError = "database password=synthetic-secret internal-host:5432"
			channels := &emailCreationStore{}
			if failure {
				channels.linkErr = errors.New(internalError)
			}
			oldBackend := platformBackendInstance
			platformBackendInstance = &verifyUserChannelBackend{channels: channels}
			t.Cleanup(func() { platformBackendInstance = oldBackend })
			oldCodes := GetLinkCodeStore()
			codes := NewLinkCodeStore()
			SetLinkCodeStore(codes)
			t.Cleanup(func() { SetLinkCodeStore(oldCodes) })
			code, err := codes.Generate(context.Background(), owner, "new@example.com", "email:new@example.com")
			if err != nil {
				t.Fatal(err)
			}
			request := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/api/user/channels/verify-email-code", strings.NewReader(`{"code":"`+code+`"}`))
				r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: owner}))
				w := httptest.NewRecorder()
				handleVerifyEmailCode(w, r)
				return w
			}
			w := request()
			want := http.StatusOK
			if failure {
				want = http.StatusInternalServerError
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if failure {
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error != "failed to link email" {
					t.Errorf("expected bounded persistence error, got %q", body.Error)
				}
				if channels.created != nil {
					t.Fatal("failed write persisted a link")
				}
			} else if channels.created == nil || channels.created.UserID != owner || channels.created.ExternalID != "new@example.com" || !channels.created.Verified || !channels.created.Enabled || channels.created.VerifiedAt == nil {
				t.Fatalf("successful link was not persisted correctly: %+v", channels.created)
			}
			if codes.Peek(context.Background(), code) != nil {
				t.Error("creation attempt left proof reusable")
			}
			if replay := request(); replay.Code != http.StatusBadRequest {
				t.Errorf("replay status=%d want=%d", replay.Code, http.StatusBadRequest)
			}
			if channels.links != 1 {
				t.Errorf("link writes=%d want=1", channels.links)
			}
		})
	}
}

func TestVerifyEmailCode_DirectLinkRejected(t *testing.T) {
	w := httptest.NewRecorder()
	handleLinkUserChannel(w, httptest.NewRequest(http.MethodPost, "/api/user/channels", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
