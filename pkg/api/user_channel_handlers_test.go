package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "modernc.org/sqlite"

	"github.com/SAP/astonish/pkg/memory"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
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

func (s *verifySpyUserChannelStore) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	copy := *s.channel
	return &copy, nil
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

func (s *emailPersistenceStore) VerifyAndEnable(_ context.Context, id, userID string) (*store.UserChannel, error) {
	s.verifies++
	if s.verifyErr != nil {
		return nil, s.verifyErr
	}
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	if s.channel.ID != id || s.channel.UserID != userID {
		return nil, errors.New("channel not found")
	}
	s.channel.Verified = true
	s.channel.Enabled = true
	s.channel.VerifiedAt = timePtr(time.Now())
	copy := s.channel
	return &copy, nil
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
			if failure != "success" && (s.channel.Verified || s.channel.Enabled || s.channel.VerifiedAt != nil) {
				t.Error("failed proof completion partially changed the link")
			}
			if s.updates != 0 {
				t.Error("proof completion used a separate mutable-field write")
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

// concurrentEmailStore records handler persistence attempts without assuming
// that proof consumption has already serialized the callers.
type concurrentEmailStore struct {
	store.UserChannelStore
	mu        sync.Mutex
	links     int
	created   *store.UserChannel
	lookupErr error
}

func (s *concurrentEmailStore) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil, s.lookupErr
}

func (s *concurrentEmailStore) Link(_ context.Context, ch *store.UserChannel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links++
	copy := *ch
	s.created = &copy
	return nil
}

func (s *concurrentEmailStore) ListByChannelType(context.Context, string) ([]*store.UserChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created == nil {
		return nil, nil
	}
	copy := *s.created
	return []*store.UserChannel{&copy}, nil
}

func TestVerifyEmailCode_ConcurrentHandlersSinglePersistence(t *testing.T) {
	for _, backend := range []string{"memory", "database-adapter"} {
		t.Run(backend, func(t *testing.T) {
			var codes LinkCodeBackend = NewLinkCodeStore()
			if backend == "database-adapter" {
				codes = NewDBLinkCodeBackend(newMemLinkCodeStore())
			}
			oldCodes := GetLinkCodeStore()
			SetLinkCodeStore(codes)
			t.Cleanup(func() { SetLinkCodeStore(oldCodes) })
			channels := &concurrentEmailStore{}
			oldBackend := platformBackendInstance
			platformBackendInstance = &verifyUserChannelBackend{channels: channels}
			t.Cleanup(func() { platformBackendInstance = oldBackend })
			// Unique principal keeps repeated test runs independent of process-local
			// rate-limit windows. Eight contenders remain below the attempt budget.
			owner := "concurrent-http-" + generateLinkCode()
			code, err := codes.Generate(context.Background(), owner, "proof@example.com", "email:proof@example.com")
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan *httptest.ResponseRecorder, 8)
			for range 8 {
				go func() {
					<-start
					r := httptest.NewRequest(http.MethodPost, "/api/user/channels/verify-email-code", strings.NewReader(`{"code":"`+code+`"}`))
					r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: owner}))
					w := httptest.NewRecorder()
					handleVerifyEmailCode(w, r)
					results <- w
				}()
			}
			close(start)
			successes := 0
			for range 8 {
				w := <-results
				switch w.Code {
				case http.StatusOK:
					successes++
				case http.StatusBadRequest:
				default:
					t.Errorf("unexpected contender status=%d body=%s", w.Code, w.Body.String())
				}
			}
			if successes != 1 || channels.links != 1 {
				t.Fatalf("successes=%d persistence attempts=%d, want one each", successes, channels.links)
			}
			if channels.created == nil || channels.created.UserID != owner || channels.created.ExternalID != "proof@example.com" || !channels.created.Verified || !channels.created.Enabled {
				t.Fatalf("winning request did not persist authorized link: %+v", channels.created)
			}
			if codes.Peek(context.Background(), code) != nil {
				t.Fatal("winning proof remains reusable")
			}
		})
	}
}

func TestVerifyEmailCode_LookupFailureDoesNotPersist(t *testing.T) {
	const canary = "synthetic database password=private-value host=internal-db"
	channels := &concurrentEmailStore{lookupErr: errors.New(canary)}
	oldBackend := platformBackendInstance
	platformBackendInstance = &verifyUserChannelBackend{channels: channels}
	t.Cleanup(func() { platformBackendInstance = oldBackend })
	oldCodes := GetLinkCodeStore()
	codes := NewLinkCodeStore()
	SetLinkCodeStore(codes)
	t.Cleanup(func() { SetLinkCodeStore(oldCodes) })
	owner := "lookup-failure-" + generateLinkCode()
	code, err := codes.Generate(context.Background(), owner, "proof@example.com", "email:proof@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for attempt, want := range []int{http.StatusInternalServerError, http.StatusBadRequest} {
		r := httptest.NewRequest(http.MethodPost, "/api/user/channels/verify-email-code", strings.NewReader(`{"code":"`+code+`"}`))
		r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: owner}))
		w := httptest.NewRecorder()
		handleVerifyEmailCode(w, r)
		if w.Code != want {
			t.Errorf("attempt %d: status=%d want=%d", attempt, w.Code, want)
		}
		if strings.Contains(w.Body.String(), canary) || strings.Contains(w.Body.String(), "private-value") {
			t.Error("lookup failure exposed internal storage error")
		}
	}
	if channels.links != 0 || channels.created != nil {
		t.Fatal("lookup failure proceeded to persistence")
	}
}

// TestUserChannelRoutes_SQLitePersistenceFailure uses the production route
// registrar, JWT middleware, Ent channel/code stores, and an actual HTTP server.
// Tokens are issued by the test (login is not exercised). Abort triggers affect
// only this test's temporary SQLite file; no platform/bootstrap is contacted.
func TestUserChannelRoutes_SQLitePersistenceFailure(t *testing.T) {
	for _, failure := range []string{"insert", "verify", "enable"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			dsn := "file:" + filepath.Join(dir, "platform.db")
			_, es, err := entstore.NewPlatformServices(ctx, entstore.Config{DSN: dsn, DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = es.Close() })
			owner := uuid.NewString()
			if err := es.Users().Create(ctx, &store.User{ID: owner, Email: "owner@example.test", DisplayName: "Owner", Status: "active"}); err != nil {
				t.Fatal(err)
			}
			channels := es.UserChannels()
			if failure != "insert" {
				if err := channels.Link(ctx, &store.UserChannel{UserID: owner, ChannelType: "email", ExternalID: "proof@example.test", DisplayName: "Original"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := channels.ListByUser(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			// RegisterRoutes sees the real backend but does not invoke the global
			// SetPlatformBackend setter (which also wires sandbox bootstrap state).
			oldBackend, oldHealth, oldCodes := platformBackendInstance, healthBackend, GetLinkCodeStore()
			platformBackendInstance = es
			t.Cleanup(func() { platformBackendInstance = oldBackend; healthBackend = oldHealth; SetLinkCodeStore(oldCodes) })
			codes := NewDBLinkCodeBackend(es.LinkCodes())
			SetLinkCodeStore(codes)
			code, err := codes.Generate(ctx, owner, "proof@example.test", "email:proof@example.test")
			if err != nil {
				t.Fatal(err)
			}
			pa := testPlatformAuth(t)
			token, err := pa.jwt.IssueAccessToken(owner, "owner@example.test", "Owner", "test-org", "test-team", "admin", "")
			if err != nil {
				t.Fatal(err)
			}
			router := mux.NewRouter()
			RegisterRoutes(router, nil, nil, nil)
			server := httptest.NewServer(PlatformAuthMiddleware(pa, router))
			t.Cleanup(server.Close)
			client := &http.Client{Timeout: 10 * time.Second}
			t.Cleanup(client.CloseIdleConnections)
			request := func(auth string, want int, wantError string) {
				t.Helper()
				body, _ := json.Marshal(map[string]string{"code": code})
				r, err := http.NewRequest(http.MethodPost, server.URL+"/api/user/channels/verify-email-code", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Content-Type", "application/json")
				if auth != "" {
					r.Header.Set("Authorization", "Bearer "+auth)
				}
				resp, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				data, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != want {
					t.Fatalf("HTTP %d, want %d: %s", resp.StatusCode, want, data)
				}
				if len(data) > 4096 || strings.Contains(string(data), "private-db-canary") {
					t.Errorf("unbounded or internal persistence error: %s", data)
				}
				if wantError != "" {
					var result map[string]any
					if err := json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
					if result["error"] != wantError || result["channel"] != nil {
						t.Errorf("unexpected error response: %s", data)
					}
				}
			}
			request("", http.StatusUnauthorized, "")
			if codes.Peek(ctx, code) == nil {
				t.Fatal("unauthenticated request consumed proof")
			}
			db, err := sql.Open("sqlite", dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			event, wantError := "INSERT", "failed to link email"
			if failure == "verify" {
				event, wantError = "UPDATE OF verified", "failed to verify email"
			}
			if failure == "enable" {
				event, wantError = "UPDATE OF enabled", "failed to verify email"
			}
			if _, err := db.ExecContext(ctx, "CREATE TRIGGER reject_channel_write BEFORE "+event+" ON user_channels BEGIN SELECT RAISE(ABORT, 'private-db-canary'); END"); err != nil {
				t.Fatal(err)
			}
			request(token, http.StatusInternalServerError, wantError)
			after, err := channels.ListByUser(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				beforeJSON, _ := json.Marshal(before)
				afterJSON, _ := json.Marshal(after)
				t.Errorf("failed %s write changed persisted channel state: before=%s after=%s", failure, beforeJSON, afterJSON)
			}
			if codes.Peek(ctx, code) != nil {
				t.Error("failed persistence left proof reusable")
			}
			if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_channel_write"); err != nil {
				t.Fatal(err)
			}
			request(token, http.StatusBadRequest, "")
			replayed, err := channels.ListByUser(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, replayed) {
				t.Error("replay changed persisted channel state")
			}
			// A fresh proof works after removing the controlled failure, proving
			// the preceding 500 reached storage rather than an unwired route.
			code, err = codes.Generate(ctx, owner, "proof@example.test", "email:proof@example.test")
			if err != nil {
				t.Fatal(err)
			}
			request(token, http.StatusOK, "")
			linked, err := channels.ListByUser(ctx, owner)
			if err != nil || len(linked) != 1 {
				t.Fatalf("recovery links=%d: %v", len(linked), err)
			}
			if !linked[0].Verified || !linked[0].Enabled || linked[0].VerifiedAt == nil {
				t.Fatalf("recovery not persisted: %+v", linked[0])
			}
		})
	}
}

func TestUserChannelProofStore_SQLiteOwnerAndMutableFields(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	_, es, err := entstore.NewPlatformServices(ctx, entstore.Config{DSN: "file:" + filepath.Join(dir, "platform.db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = es.Close() })
	owner := uuid.NewString()
	if err := es.Users().Create(ctx, &store.User{ID: owner, Email: "owner@example.test", DisplayName: "Owner", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	channels := es.UserChannels()
	proofs, ok := channels.(store.UserChannelProofStore)
	if !ok {
		t.Fatal("Ent store must support atomic proof completion")
	}
	link := &store.UserChannel{UserID: owner, ChannelType: "email", ExternalID: "proof@example.test", DisplayName: "Original"}
	if err := channels.Link(ctx, link); err != nil {
		t.Fatal(err)
	}
	before, err := channels.GetByID(ctx, link.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, id, user string }{
		{"wrong-owner", link.ID, uuid.NewString()},
		{"missing-link", uuid.NewString(), owner},
		{"invalid-link", "invalid", owner},
		{"invalid-owner", link.ID, "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if updated, err := proofs.VerifyAndEnable(ctx, tc.id, tc.user); err == nil || updated != nil {
				t.Fatalf("invalid proof write returned row=%+v err=%v", updated, err)
			}
			after, err := channels.GetByID(ctx, link.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("invalid proof write changed link: %+v err=%v", after, err)
			}
		})
	}
	updated, err := proofs.VerifyAndEnable(ctx, link.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := channels.GetByID(ctx, link.ID)
	if err != nil || !reflect.DeepEqual(updated, persisted) || !persisted.Verified || !persisted.Enabled || persisted.VerifiedAt == nil {
		t.Fatalf("proof result differs from persisted state: %+v / %+v err=%v", updated, persisted, err)
	}
	// Ordinary updates must not change proof fields, even if supplied by a caller.
	mutable := *persisted
	mutable.Enabled = false
	mutable.DisplayName = "Changed"
	mutable.Verified = false
	mutable.VerifiedAt = nil
	if err := channels.Update(ctx, &mutable); err != nil {
		t.Fatal(err)
	}
	after, err := channels.GetByID(ctx, link.ID)
	if err != nil || after.Enabled || after.DisplayName != "Changed" || !after.Verified || !reflect.DeepEqual(after.VerifiedAt, persisted.VerifiedAt) {
		t.Fatalf("mutable update changed proof semantics: %+v err=%v", after, err)
	}
}

// staleOwnerLookupStore models a lookup snapshot taken before ownership changed.
// Proof completion still delegates to the actual persistence implementation.
type staleOwnerLookupStore struct {
	store.UserChannelStore
	proofs                   store.UserChannelProofStore
	snapshot                 store.UserChannel
	proofCalls, legacyWrites int
	proofID, proofOwner      string
}

func (s *staleOwnerLookupStore) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	copy := s.snapshot
	return &copy, nil
}

func (s *staleOwnerLookupStore) VerifyAndEnable(ctx context.Context, id, owner string) (*store.UserChannel, error) {
	s.proofCalls++
	s.proofID, s.proofOwner = id, owner
	return s.proofs.VerifyAndEnable(ctx, id, owner)
}

func (s *staleOwnerLookupStore) Verify(ctx context.Context, id string) error {
	s.legacyWrites++
	return s.UserChannelStore.Verify(ctx, id)
}

func (s *staleOwnerLookupStore) Update(ctx context.Context, ch *store.UserChannel) error {
	s.legacyWrites++
	return s.UserChannelStore.Update(ctx, ch)
}

func TestVerifyEmailCode_StaleOwnerLookupDoesNotPersist(t *testing.T) {
	for _, backend := range []string{"mock", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			staleOwner, currentOwner := uuid.NewString(), uuid.NewString()
			link := store.UserChannel{ID: uuid.NewString(), UserID: currentOwner, ChannelType: "email", ExternalID: "proof@example.test", DisplayName: "Original"}
			var channels store.UserChannelStore
			var codes LinkCodeBackend = NewLinkCodeStore()
			var readPersisted func() *store.UserChannel
			if backend == "sqlite" {
				dir := t.TempDir()
				_, es, err := entstore.NewPlatformServices(ctx, entstore.Config{DSN: "file:" + filepath.Join(dir, "platform.db"), DataDir: dir})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = es.Close() })
				for i, owner := range []string{staleOwner, currentOwner} {
					if err := es.Users().Create(ctx, &store.User{ID: owner, Email: []string{"stale@example.test", "current@example.test"}[i], DisplayName: "Owner", Status: "active"}); err != nil {
						t.Fatal(err)
					}
				}
				channels = es.UserChannels()
				if err := channels.Link(ctx, &link); err != nil {
					t.Fatal(err)
				}
				codes = NewDBLinkCodeBackend(es.LinkCodes())
				readPersisted = func() *store.UserChannel {
					t.Helper()
					row, err := channels.GetByID(ctx, link.ID)
					if err != nil || row == nil {
						t.Fatalf("read persisted channel: row=%+v err=%v", row, err)
					}
					return row
				}
			} else {
				s := &emailPersistenceStore{channel: link}
				channels = s
				readPersisted = func() *store.UserChannel { copy := s.channel; return &copy }
			}
			proofs, ok := channels.(store.UserChannelProofStore)
			if !ok {
				t.Fatal("fixture must exercise a proof-capable store")
			}
			before := readPersisted()
			wrapped := &staleOwnerLookupStore{UserChannelStore: channels, proofs: proofs, snapshot: *before}
			wrapped.snapshot.UserID = staleOwner
			installEmailSecurityFixture(t, codes, wrapped)
			code, err := codes.Generate(ctx, staleOwner, link.ExternalID, "email:"+link.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			w := emailSecurityRequest(staleOwner, code)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("stale owner status=%d body=%s", w.Code, w.Body.String())
			}
			var failure map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if len(failure) != 1 || failure["error"] != "failed to verify email" {
				t.Fatalf("write rejection must return only a bounded error: %s", w.Body.String())
			}
			if wrapped.proofCalls != 1 || wrapped.proofID != link.ID || wrapped.proofOwner != staleOwner || wrapped.legacyWrites != 0 {
				t.Fatalf("proof did not use authenticated owner at write: %+v", wrapped)
			}
			if after := readPersisted(); !reflect.DeepEqual(before, after) {
				t.Fatalf("stale-owner proof changed persisted row: before=%+v after=%+v", before, after)
			}
			if codes.Peek(ctx, code) != nil {
				t.Fatal("rejected write retained reusable proof")
			}
			if w := emailSecurityRequest(staleOwner, code); w.Code != http.StatusBadRequest {
				t.Fatalf("replay status=%d body=%s", w.Code, w.Body.String())
			}
			if wrapped.proofCalls != 1 || !reflect.DeepEqual(before, readPersisted()) {
				t.Fatal("replay attempted persistence or changed the row")
			}

			// The real current owner can still complete a fresh proof. Validate
			// the returned row against storage, including the proof timestamp.
			wrapped.snapshot = *before
			code, err = codes.Generate(ctx, currentOwner, link.ExternalID, "email:"+link.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			w = emailSecurityRequest(currentOwner, code)
			if w.Code != http.StatusOK {
				t.Fatalf("current owner status=%d body=%s", w.Code, w.Body.String())
			}
			var result struct {
				Channel *store.UserChannel `json:"channel"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			after := readPersisted()
			if !after.Verified || !after.Enabled || after.VerifiedAt == nil || after.UserID != currentOwner {
				t.Fatalf("current-owner proof was not persisted: %+v", after)
			}
			// JSON comparison ignores time.Time's in-process monotonic metadata.
			persistedJSON, _ := json.Marshal(after)
			responseJSON, _ := json.Marshal(result.Channel)
			if !bytes.Equal(persistedJSON, responseJSON) || wrapped.proofCalls != 2 || wrapped.legacyWrites != 0 || codes.Peek(ctx, code) != nil {
				t.Fatalf("success response/persistence mismatch: persisted=%s response=%s", persistedJSON, responseJSON)
			}
		})
	}
}

func TestVerifyEmailCode_MissingAtomicStoreFailsClosed(t *testing.T) {
	owner := "unsupported-" + generateLinkCode()
	link := &store.UserChannel{ID: "link", UserID: owner, ExternalID: "a@example.test"}
	channels := &verifySpyUserChannelStore{channel: link}
	codes := NewLinkCodeStore()
	installEmailSecurityFixture(t, codes, channels)
	code, err := codes.Generate(context.Background(), owner, link.ExternalID, "email:"+link.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if w := emailSecurityRequest(owner, code); w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if channels.verifyCalls != 0 || link.Verified || link.Enabled || codes.Peek(context.Background(), code) != nil {
		t.Fatal("unsupported store changed link or retained consumed proof")
	}
}

// This route-level integration test deliberately lives in the ordinary API
// suite: it uses real SQLite and tenant/JWT middleware without e2eboot.Bootstrap,
// whose normal E2E path requires a provider and sweeps sandbox infrastructure.
func TestMemoryRoutes_SQLiteConsolidationPersistenceFailure(t *testing.T) {
	for _, failure := range []string{"insert", "update"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			svc, es, err := entstore.NewPlatformServices(ctx, entstore.Config{DSN: "file:" + filepath.Join(dir, "platform.db"), DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = es.Close() })
			owner := uuid.NewString()
			if err := es.ProvisionOrg(ctx, uuid.NewString(), "persistence"); err != nil {
				t.Fatal(err)
			}
			org, err := es.ForOrg("persistence")
			if err != nil {
				t.Fatal(err)
			}
			if err := org.ProvisionTeam(ctx, "test-team"); err != nil {
				t.Fatal(err)
			}
			if err := org.ProvisionPersonalSchema(ctx, owner); err != nil {
				t.Fatal(err)
			}
			memories := map[string]store.MemoryStore{
				"personal": org.ForUser(owner).Memories(),
				"team":     org.ForTeam("test-team").Memories(),
				"org":      org.OrgMemories(),
			}
			var sources, duplicates []string
			for scope, ms := range memories {
				if err := ms.Add(ctx, store.MemoryEntry{Content: "Original " + scope + " source must survive failed consolidation", Category: "route-persistence", CreatedBy: owner}); err != nil {
					t.Fatal(err)
				}
				rows, err := ms.List(ctx, "", 100, 0)
				if err != nil || len(rows) != 1 {
					t.Fatalf("seed %s: %v", scope, err)
				}
				sources = append(sources, rows[0].ID)
			}
			card := memory.ScenarioCard{CanonicalKey: "route-persistence", Identity: memory.ScenarioIdentity{System: "route-test", Service: "configuration", ResourceType: "service", Operation: "restart"}, Scope: "team", Title: "Route persistence", RecommendedRecipe: []string{"Read the configuration file and validate its contents before restarting the service."}, Status: memory.ScenarioCardStatusVerified, SourceMemoryIDs: sources}
			if failure == "update" {
				for _, scope := range []string{"team", "personal"} {
					old := card
					old.Scope = scope
					old.RecommendedRecipe = append(append([]string(nil), card.RecommendedRecipe...), "Keep the original audit record.")
					if err := memories[scope].Add(ctx, store.MemoryEntry{Content: memory.RenderScenarioCard(old), Category: memory.ScenarioCardCategory, CreatedBy: owner}); err != nil {
						t.Fatal(err)
					}
					rows, err := memories[scope].List(ctx, memory.ScenarioCardCategory, 100, 0)
					if err != nil || len(rows) != 1 {
						t.Fatalf("seed card: %v", err)
					}
					if scope == "personal" {
						duplicates = append(duplicates, rows[0].ID)
					}
				}
			}
			snapshot := func() map[string]map[string]store.MemorySearchResult {
				t.Helper()
				result := make(map[string]map[string]store.MemorySearchResult)
				for scope, ms := range memories {
					rows, err := ms.List(ctx, "", 100, 0)
					if err != nil {
						t.Fatal(err)
					}
					result[scope] = make(map[string]store.MemorySearchResult)
					for _, row := range rows {
						result[scope][row.ID] = row
					}
				}
				return result
			}
			before := snapshot()
			if failure == "update" && preferredPreservedScenarioCardID(ctx, memories["team"], duplicates, card) == "" {
				t.Fatal("fixture must select an existing destination for UPDATE failure injection")
			}
			oldHealth := healthBackend
			t.Cleanup(func() { healthBackend = oldHealth })
			router := mux.NewRouter()
			RegisterRoutes(router, svc, nil, entstore.TenantMiddleware(es))
			pa := testPlatformAuth(t)
			token, err := pa.jwt.IssueAccessToken(owner, "owner@example.test", "Owner", "persistence", "test-team", "admin", "")
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(PlatformAuthMiddleware(pa, router))
			t.Cleanup(server.Close)
			client := &http.Client{Timeout: 10 * time.Second}
			t.Cleanup(client.CloseIdleConnections)
			request := func(auth string, want int) map[string]any {
				t.Helper()
				body, err := json.Marshal(MemoryConsolidationApplyRequest{Card: card, TargetScope: "team", DuplicateCardIDs: duplicates})
				if err != nil {
					t.Fatal(err)
				}
				r, err := http.NewRequest(http.MethodPost, server.URL+"/api/memories/consolidate/apply", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Content-Type", "application/json")
				if auth != "" {
					r.Header.Set("Authorization", "Bearer "+auth)
				}
				resp, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				data, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != want {
					t.Fatalf("HTTP %d, want %d: %s", resp.StatusCode, want, data)
				}
				if len(data) > 4096 || strings.Contains(string(data), "private-db-canary") {
					t.Fatalf("unbounded/internal error: %s", data)
				}
				var result map[string]any
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			request("", http.StatusUnauthorized)
			if !reflect.DeepEqual(before, snapshot()) {
				t.Fatal("unauthenticated request changed memories")
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "orgs", "persistence", "teams", "test-team.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			event := "INSERT"
			if failure == "update" {
				event = "UPDATE"
			}
			if _, err := db.ExecContext(ctx, "CREATE TRIGGER reject_memory_write BEFORE "+event+" ON memories BEGIN SELECT RAISE(ABORT, 'private-db-canary'); END"); err != nil {
				t.Fatal(err)
			}
			result := request(token, http.StatusInternalServerError)
			if result["error"] != "failed to save scenario card" || result["applied"] == true {
				t.Errorf("unexpected failure response: %+v", result)
			}
			if !reflect.DeepEqual(before, snapshot()) {
				t.Error("failed destination write changed a source, duplicate, or destination in personal/team/org storage")
			}
			if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_memory_write"); err != nil {
				t.Fatal(err)
			}
			result = request(token, http.StatusOK)
			if result["applied"] != true || result["deleted_sources"] != float64(len(sources)) {
				t.Fatalf("recovery did not apply consolidation: %+v", result)
			}
			after := snapshot()
			if len(after["team"]) != 1 || len(after["personal"]) != 0 || len(after["org"]) != 0 {
				t.Fatalf("unexpected recovery state: %+v", after)
			}
			for _, row := range after["team"] {
				if failure == "update" {
					if _, existed := before["team"][row.ID]; !existed {
						t.Fatal("recovery inserted a new destination instead of updating the original")
					}
				}
				if row.Category != memory.ScenarioCardCategory || !strings.Contains(row.Snippet, card.RecommendedRecipe[0]) {
					t.Fatalf("destination not persisted: %+v", row)
				}
			}
		})
	}
}
