package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

type mockPersonalSettingsStore struct {
	mu       sync.Mutex
	settings *store.PersonalSettings
}

func (m *mockPersonalSettingsStore) Get(_ context.Context) (*store.PersonalSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings == nil {
		return &store.PersonalSettings{}, nil
	}
	cp := *m.settings
	return &cp, nil
}

func (m *mockPersonalSettingsStore) Save(_ context.Context, s *store.PersonalSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.settings = &cp
	return nil
}

func newUserSettingsRequest(t *testing.T, method, body string, ps store.PersonalSettingsStore) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var buf *bytes.Buffer
	if body == "" {
		buf = bytes.NewBufferString("")
	} else {
		buf = bytes.NewBufferString(body)
	}
	r := httptest.NewRequest(method, "/api/user-settings/default-model", buf)
	ctx := store.WithServices(r.Context(), &store.Services{PersonalSettings: ps})
	r = r.WithContext(ctx)
	return httptest.NewRecorder(), r
}

func TestUserSettingsDefaultModel_GetEmpty(t *testing.T) {
	ps := &mockPersonalSettingsStore{}
	w, r := newUserSettingsRequest(t, http.MethodGet, "", ps)
	GetUserDefaultModelHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got userDefaultModelPayload
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.DefaultProvider != "" || got.DefaultModel != "" {
		t.Fatalf("expected empty payload, got %+v", got)
	}
}

func TestUserSettingsDefaultModel_PatchAndGet(t *testing.T) {
	ps := &mockPersonalSettingsStore{}

	patchBody := `{"defaultProvider":"anthropic","defaultModel":"claude-4"}`
	w, r := newUserSettingsRequest(t, http.MethodPatch, patchBody, ps)
	PatchUserDefaultModelHandler(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w2, r2 := newUserSettingsRequest(t, http.MethodGet, "", ps)
	GetUserDefaultModelHandler(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var got userDefaultModelPayload
	if err := json.NewDecoder(w2.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.DefaultProvider != "anthropic" || got.DefaultModel != "claude-4" {
		t.Fatalf("expected {anthropic, claude-4}, got %+v", got)
	}

	clearBody := `{"defaultProvider":"","defaultModel":""}`
	w3, r3 := newUserSettingsRequest(t, http.MethodPatch, clearBody, ps)
	PatchUserDefaultModelHandler(w3, r3)
	if w3.Code != http.StatusOK {
		t.Fatalf("clear PATCH: expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	w4, r4 := newUserSettingsRequest(t, http.MethodGet, "", ps)
	GetUserDefaultModelHandler(w4, r4)
	var cleared userDefaultModelPayload
	if err := json.NewDecoder(w4.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cleared.DefaultProvider != "" || cleared.DefaultModel != "" {
		t.Fatalf("expected cleared payload, got %+v", cleared)
	}
}

func TestUserSettingsDefaultModel_PatchMalformed(t *testing.T) {
	ps := &mockPersonalSettingsStore{}
	w, r := newUserSettingsRequest(t, http.MethodPatch, `{"defaultProvider":`, ps)
	PatchUserDefaultModelHandler(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
