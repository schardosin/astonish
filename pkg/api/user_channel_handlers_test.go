package api

import (
	"context"
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
