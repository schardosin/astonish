package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/store"
)

// --- effectiveUserID tests ---

func TestEffectiveUserID_PersonalMode(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	got := effectiveUserID(r)
	if got != studioChatUserID {
		t.Errorf("effectiveUserID() = %q, want %q", got, studioChatUserID)
	}
}

func TestEffectiveUserID_PlatformMode(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	ctx := WithPlatformUser(r.Context(), &PlatformUser{
		ID:       "user-123",
		Email:    "test@example.com",
		OrgSlug:  "acme",
		TeamSlug: "engineering",
		Role:     "member",
	})
	r = r.WithContext(ctx)

	got := effectiveUserID(r)
	if got != "user-123" {
		t.Errorf("effectiveUserID() = %q, want %q", got, "user-123")
	}
}

// --- DefaultUserID tests ---

func TestDefaultUserID(t *testing.T) {
	got := DefaultUserID()
	if got != studioChatUserID {
		t.Errorf("DefaultUserID() = %q, want %q", got, studioChatUserID)
	}
}

// --- isPlatformMode tests ---

func TestIsPlatformMode_NoServices(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/test", nil)
	if isPlatformMode(r) {
		t.Error("isPlatformMode() should be false when no Services in context")
	}
}

func TestIsPlatformMode_PersonalMode(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/test", nil)
	svc := &store.Services{Mode: store.ModePersonal}
	ctx := store.WithServices(r.Context(), svc)
	r = r.WithContext(ctx)
	if isPlatformMode(r) {
		t.Error("isPlatformMode() should be false in personal mode")
	}
}

func TestIsPlatformMode_PlatformMode(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/test", nil)
	svc := &store.Services{Mode: store.ModePlatform}
	ctx := store.WithServices(r.Context(), svc)
	r = r.WithContext(ctx)
	if !isPlatformMode(r) {
		t.Error("isPlatformMode() should be true in platform mode")
	}
}

// --- effectiveCredentialStore tests ---

func TestEffectiveCredentialStore_NoStore(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/test", nil)
	// Ensure no singleton is set
	prev := getAPICredentialStore()
	SetAPICredentialStore(nil)
	defer SetAPICredentialStore(prev)

	cs := effectiveCredentialStore(r)
	if cs != nil {
		t.Error("effectiveCredentialStore() should return nil when no store available")
	}
}

func TestEffectiveCredentialStore_FromServices(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/test?scope=team", nil)
	mockCreds := &mockCredentialStore{}
	svc := &store.Services{
		Mode:        store.ModePlatform,
		Credentials: mockCreds,
	}
	ctx := store.WithServices(r.Context(), svc)
	r = r.WithContext(ctx)

	cs := effectiveCredentialStore(r)
	if cs == nil {
		t.Fatal("effectiveCredentialStore() should return non-nil from Services")
	}
	if cs != mockCreds {
		t.Error("effectiveCredentialStore() should return the Services credential store")
	}
}

// --- ChatRunner UserID tests ---

func TestNewChatRunner_UserID(t *testing.T) {
	runner := newChatRunner("session-1", "user-abc", true)
	if runner.UserID != "user-abc" {
		t.Errorf("runner.UserID = %q, want %q", runner.UserID, "user-abc")
	}
	if runner.SessionID != "session-1" {
		t.Errorf("runner.SessionID = %q, want %q", runner.SessionID, "session-1")
	}
	if !runner.IsNew {
		t.Error("runner.IsNew should be true")
	}
	runner.cancel() // cleanup
}

// --- Audit middleware tests ---

// mockAuditStore captures audit entries for testing.
type mockAuditStore struct {
	mu      sync.Mutex
	entries []*store.AuditEntry
}

func (m *mockAuditStore) Log(_ context.Context, entry *store.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditStore) Query(_ context.Context, _ store.AuditFilter) ([]*store.AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries, nil
}

func (m *mockAuditStore) getEntries() []*store.AuditEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*store.AuditEntry, len(m.entries))
	copy(result, m.entries)
	return result
}

func TestAuditMiddleware_PersonalMode_Noop(t *testing.T) {
	audit := &mockAuditStore{}
	svc := &store.Services{
		Mode:  store.ModePersonal,
		Audit: audit,
	}

	handler := AuditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/api/sessions", nil)
	ctx := store.WithServices(r.Context(), svc)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if len(audit.getEntries()) != 0 {
		t.Error("audit middleware should not log in personal mode")
	}
}

func TestAuditMiddleware_PlatformMode_Logs(t *testing.T) {
	audit := &mockAuditStore{}
	svc := &store.Services{
		Mode:  store.ModePlatform,
		Audit: audit,
	}

	innerCalled := false
	handler := AuditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	ctx := store.WithServices(r.Context(), svc)
	ctx = WithPlatformUser(ctx, &PlatformUser{
		ID:       "user-456",
		Email:    "admin@acme.com",
		OrgSlug:  "acme",
		TeamSlug: "ops",
	})
	r = r.WithContext(ctx)

	// Need to use mux router for CurrentRoute to work
	router := mux.NewRouter()
	router.Handle("/api/sessions", handler).Methods("GET")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	if !innerCalled {
		t.Fatal("inner handler was not called")
	}

	// Audit log is written asynchronously — wait briefly.
	time.Sleep(100 * time.Millisecond)

	entries := audit.getEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	e := entries[0]
	if e.UserID != "user-456" {
		t.Errorf("audit UserID = %q, want %q", e.UserID, "user-456")
	}
	if e.TeamID != "ops" {
		t.Errorf("audit TeamID = %q, want %q", e.TeamID, "ops")
	}
	if e.Action != "GET" {
		t.Errorf("audit Action = %q, want %q", e.Action, "GET")
	}
	if e.Resource != "GET /api/sessions" {
		t.Errorf("audit Resource = %q, want %q", e.Resource, "GET /api/sessions")
	}
	if e.IPAddress != "10.0.0.1" {
		t.Errorf("audit IPAddress = %q, want %q", e.IPAddress, "10.0.0.1")
	}

	detail, ok := e.Detail.(map[string]any)
	if !ok {
		t.Fatal("audit Detail should be map[string]any")
	}
	if detail["status"] != http.StatusOK {
		t.Errorf("audit Detail.status = %v, want %d", detail["status"], http.StatusOK)
	}
}

func TestAuditMiddleware_XForwardedFor(t *testing.T) {
	audit := &mockAuditStore{}
	svc := &store.Services{
		Mode:  store.ModePlatform,
		Audit: audit,
	}

	handler := AuditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	r := httptest.NewRequest("POST", "/api/chat", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")
	r.RemoteAddr = "127.0.0.1:9999"
	ctx := store.WithServices(r.Context(), svc)
	r = r.WithContext(ctx)

	router := mux.NewRouter()
	router.Handle("/api/chat", handler).Methods("POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	time.Sleep(100 * time.Millisecond)

	entries := audit.getEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].IPAddress != "203.0.113.50" {
		t.Errorf("audit IPAddress = %q, want %q (first X-Forwarded-For)", entries[0].IPAddress, "203.0.113.50")
	}
}

// --- Mock credential store for testing ---

type mockCredentialStore struct{}

func (m *mockCredentialStore) Get(_ string) *store.Credential         { return nil }
func (m *mockCredentialStore) Set(_ string, _ *store.Credential) error { return nil }
func (m *mockCredentialStore) Remove(_ string) error                   { return nil }
func (m *mockCredentialStore) List() map[string]store.CredentialType   { return nil }
func (m *mockCredentialStore) Count() int                              { return 0 }
func (m *mockCredentialStore) Resolve(_ string) (string, string, error) {
	return "", "", nil
}
func (m *mockCredentialStore) SetSecret(_, _ string) error          { return nil }
func (m *mockCredentialStore) SetSecretBatch(_ map[string]string) error { return nil }
func (m *mockCredentialStore) GetSecret(_ string) string            { return "" }
func (m *mockCredentialStore) RemoveSecret(_ string) error          { return nil }
func (m *mockCredentialStore) HasSecrets() bool                     { return false }
func (m *mockCredentialStore) SecretCount() int                     { return 0 }
func (m *mockCredentialStore) ListSecrets() []string                { return nil }
func (m *mockCredentialStore) Reload() error                        { return nil }
