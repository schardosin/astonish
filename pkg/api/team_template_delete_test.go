package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Mock implementations for SandboxTemplateStore and LayerStore
// ---------------------------------------------------------------------------

type mockTemplateStore struct {
	mu        sync.Mutex
	templates map[string]*store.SandboxTemplate // keyed by ID
}

func newMockTemplateStore() *mockTemplateStore {
	return &mockTemplateStore{templates: make(map[string]*store.SandboxTemplate)}
}

func (m *mockTemplateStore) Create(_ context.Context, tpl *store.SandboxTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tpl.ID == "" {
		tpl.ID = tpl.Slug // simplification for tests
	}
	m.templates[tpl.ID] = tpl
	return nil
}

func (m *mockTemplateStore) GetByID(_ context.Context, id string) (*store.SandboxTemplate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.templates[id]
	return t, nil
}

func (m *mockTemplateStore) GetBySlug(_ context.Context, scope store.SandboxTemplateScope, ownerID, slug string) (*store.SandboxTemplate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.templates {
		if t.Scope == scope && t.OwnerID == ownerID && t.Slug == slug {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockTemplateStore) List(_ context.Context, _ store.SandboxTemplateFilter) ([]*store.SandboxTemplate, error) {
	return nil, nil
}

func (m *mockTemplateStore) Update(_ context.Context, tpl *store.SandboxTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.templates[tpl.ID] = tpl
	return nil
}

func (m *mockTemplateStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.templates[id]; !ok {
		return nil // idempotent
	}
	delete(m.templates, id)
	return nil
}

func (m *mockTemplateStore) Resolve(_ context.Context, id string) (*store.ResolvedTemplateChain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.templates[id]
	if !ok {
		return nil, errors.New("not found")
	}
	chain := &store.ResolvedTemplateChain{TemplateID: id}
	if t.TopLayerID != nil {
		chain.LayerIDs = []string{*t.TopLayerID}
	}
	return chain, nil
}

func (m *mockTemplateStore) ListRoots(_ context.Context) ([]*store.SandboxTemplate, error) {
	return nil, nil
}

func (m *mockTemplateStore) has(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.templates[id]
	return ok
}

// mockLayerStore tracks layers and ref_counts in memory.
type mockLayerStore struct {
	mu     sync.Mutex
	layers map[string]*store.SandboxLayer
}

func newMockLayerStore() *mockLayerStore {
	return &mockLayerStore{layers: make(map[string]*store.SandboxLayer)}
}

func (m *mockLayerStore) PutLayer(_ context.Context, layer *store.SandboxLayer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.layers[layer.LayerID]; exists {
		return nil // idempotent
	}
	m.layers[layer.LayerID] = layer
	return nil
}

func (m *mockLayerStore) GetLayer(_ context.Context, layerID string) (*store.SandboxLayer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.layers[layerID]
	return l, nil
}

func (m *mockLayerStore) IncrementRefCount(_ context.Context, layerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.layers[layerID]
	if !ok {
		return errors.New("layer not found")
	}
	l.RefCount++
	return nil
}

func (m *mockLayerStore) DecrementRefCount(_ context.Context, layerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.layers[layerID]
	if !ok {
		return errors.New("layer not found")
	}
	if l.RefCount <= 0 {
		return errors.New("ref_count already at zero")
	}
	l.RefCount--
	return nil
}

func (m *mockLayerStore) ListUnreferenced(_ context.Context, _ time.Duration) ([]*store.SandboxLayer, error) {
	return nil, nil
}

func (m *mockLayerStore) DeleteLayer(_ context.Context, layerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.layers[layerID]
	if !ok {
		return errors.New("layer not found")
	}
	if l.RefCount > 0 {
		return errors.New("ref_count > 0")
	}
	delete(m.layers, layerID)
	return nil
}

func (m *mockLayerStore) refCount(layerID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.layers[layerID]
	if l == nil {
		return -1
	}
	return l.RefCount
}

// ---------------------------------------------------------------------------
// Tests for deleteTeamTemplateStateWith
// ---------------------------------------------------------------------------

func TestDeleteTeamTemplateStateWith_RemovesRowAndDecrementsRef(t *testing.T) {
	ctx := context.Background()
	templates := newMockTemplateStore()
	layers := newMockLayerStore()

	layerID := "510c99e79275ac1b9c9211781fe4c6cffbf9c7242c26a3f966f2fe2b885af9c9"

	// Seed: layer with ref_count=1, template referencing it.
	_ = layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:    layerID,
		CephFSPath: "/mnt/astonish-layers/" + layerID,
		SizeBytes:  26622301,
		RefCount:   1,
	})
	_ = templates.Create(ctx, &store.SandboxTemplate{
		ID:         "tpl-general",
		Slug:       "team-general",
		Scope:      store.SandboxTemplateScopeTeam,
		OwnerID:    "general",
		TopLayerID: &layerID,
		Version:    1,
	})

	// Act
	got := deleteTeamTemplateStateWith(ctx, "general", templates, layers)

	// Assert: returns the decremented layer ID.
	if got != layerID {
		t.Errorf("returned layerID = %q, want %q", got, layerID)
	}

	// Assert: template row gone.
	if templates.has("tpl-general") {
		t.Error("expected template row to be deleted, but it still exists")
	}

	// Assert: layer ref_count decremented to 0.
	if gotRef := layers.refCount(layerID); gotRef != 0 {
		t.Errorf("layer ref_count = %d, want 0", gotRef)
	}
}

func TestDeleteTeamTemplateStateWith_NoopWhenNoTemplate(t *testing.T) {
	ctx := context.Background()
	templates := newMockTemplateStore()
	layers := newMockLayerStore()

	// No template exists — should be a no-op and return empty.
	got := deleteTeamTemplateStateWith(ctx, "nonexistent", templates, layers)
	if got != "" {
		t.Errorf("returned layerID = %q, want empty", got)
	}
}

func TestDeleteTeamTemplateStateWith_IdempotentOnSecondCall(t *testing.T) {
	ctx := context.Background()
	templates := newMockTemplateStore()
	layers := newMockLayerStore()

	layerID := "abc123"
	_ = layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:  layerID,
		RefCount: 1,
	})
	_ = templates.Create(ctx, &store.SandboxTemplate{
		ID:         "tpl-x",
		Slug:       "team-x",
		Scope:      store.SandboxTemplateScopeTeam,
		OwnerID:    "x",
		TopLayerID: &layerID,
		Version:    1,
	})

	// First call cleans up.
	deleteTeamTemplateStateWith(ctx, "x", templates, layers)

	if templates.has("tpl-x") {
		t.Fatal("template should be deleted after first call")
	}
	if got := layers.refCount(layerID); got != 0 {
		t.Fatalf("ref_count = %d after first call, want 0", got)
	}

	// Second call is a no-op (template already gone).
	deleteTeamTemplateStateWith(ctx, "x", templates, layers)

	// ref_count should still be 0, not negative.
	if got := layers.refCount(layerID); got != 0 {
		t.Errorf("ref_count = %d after second call, want 0", got)
	}
}

func TestDeleteTeamTemplateStateWith_SkipsBaseLayerDecrement(t *testing.T) {
	ctx := context.Background()
	templates := newMockTemplateStore()
	layers := newMockLayerStore()

	baseID := "@base"
	_ = templates.Create(ctx, &store.SandboxTemplate{
		ID:         "tpl-base-only",
		Slug:       "team-baseonly",
		Scope:      store.SandboxTemplateScopeTeam,
		OwnerID:    "baseonly",
		TopLayerID: &baseID,
		Version:    1,
	})

	// Act — should not attempt to decrement @base (which isn't in the layer
	// store and would error).
	deleteTeamTemplateStateWith(ctx, "baseonly", templates, layers)

	if templates.has("tpl-base-only") {
		t.Error("template row should be deleted")
	}
}

func TestDeleteTeamTemplateStateWith_NilTopLayerID(t *testing.T) {
	ctx := context.Background()
	templates := newMockTemplateStore()
	layers := newMockLayerStore()

	_ = templates.Create(ctx, &store.SandboxTemplate{
		ID:         "tpl-nil",
		Slug:       "team-nil",
		Scope:      store.SandboxTemplateScopeTeam,
		OwnerID:    "nil",
		TopLayerID: nil,
		Version:    1,
	})

	// Act — should not panic on nil TopLayerID.
	deleteTeamTemplateStateWith(ctx, "nil", templates, layers)

	if templates.has("tpl-nil") {
		t.Error("template row should be deleted")
	}
}

func TestDeleteTeamTemplateStateWith_SharedLayerRefCount(t *testing.T) {
	// Simulates two templates referencing the same content-addressed layer.
	// Deleting one should decrement to 1, not 0.
	ctx := context.Background()
	templates := newMockTemplateStore()
	layers := newMockLayerStore()

	layerID := "shared-sha256"
	_ = layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:  layerID,
		RefCount: 2, // two templates reference this
	})

	_ = templates.Create(ctx, &store.SandboxTemplate{
		ID:         "tpl-team-a",
		Slug:       "team-a",
		Scope:      store.SandboxTemplateScopeTeam,
		OwnerID:    "a",
		TopLayerID: &layerID,
		Version:    1,
	})
	_ = templates.Create(ctx, &store.SandboxTemplate{
		ID:         "tpl-team-b",
		Slug:       "team-b",
		Scope:      store.SandboxTemplateScopeTeam,
		OwnerID:    "b",
		TopLayerID: &layerID,
		Version:    1,
	})

	// Delete team-a.
	deleteTeamTemplateStateWith(ctx, "a", templates, layers)

	// Layer ref_count should be 1 (team-b still references it).
	if got := layers.refCount(layerID); got != 1 {
		t.Errorf("ref_count = %d after deleting team-a, want 1", got)
	}

	// team-b still exists.
	if !templates.has("tpl-team-b") {
		t.Error("team-b template should still exist")
	}
}
