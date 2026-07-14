package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
)

type memorySetupDraftStore struct {
	mu     sync.RWMutex
	drafts map[string]*store.FleetSetupDraft
}

var fallbackSetupDraftStore = &memorySetupDraftStore{drafts: map[string]*store.FleetSetupDraft{}}

func (m *memorySetupDraftStore) Create(_ context.Context, draft *store.FleetSetupDraft) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if draft.ID == "" {
		draft.ID = uuid.NewString()
	}
	if draft.Collected == nil {
		draft.Collected = map[string]any{}
	}
	copied := *draft
	m.drafts[draft.ID] = &copied
	return nil
}

func (m *memorySetupDraftStore) Get(_ context.Context, id string) (*store.FleetSetupDraft, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.drafts[id]
	if !ok {
		return nil, false
	}
	copied := *d
	return &copied, true
}

func (m *memorySetupDraftStore) Update(_ context.Context, draft *store.FleetSetupDraft) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if draft == nil || draft.ID == "" {
		return nil
	}
	copied := *draft
	m.drafts[draft.ID] = &copied
	return nil
}

func (m *memorySetupDraftStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.drafts, id)
	return nil
}

func getSetupDraftStore(svc *store.Services) store.FleetSetupDraftStore {
	if svc != nil && svc.FleetSetupDrafts != nil {
		return svc.FleetSetupDrafts
	}
	return fallbackSetupDraftStore
}

type memorySetupProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*fleet.SetupProfile
}

var fallbackSetupProfileStore = &memorySetupProfileStore{profiles: map[string]*fleet.SetupProfile{}}

func (m *memorySetupProfileStore) GetProfile(_ context.Context, key string) (any, bool) {
	if p, ok := fleet.GetBundledSetupProfile(key); ok {
		return p, true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.profiles[key]
	if !ok {
		return nil, false
	}
	return cloneSetupProfile(p), true
}

func (m *memorySetupProfileStore) ListProfiles(_ context.Context) []store.FleetSetupProfileSummary {
	summaries, _ := fleet.SetupProfileSummaries()
	out := make([]store.FleetSetupProfileSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, store.FleetSetupProfileSummary{
			Key: s.Key, Name: s.Name, Description: s.Description,
			Domain: s.Domain, StepCount: s.StepCount, Source: s.Source,
		})
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	bundled := fleet.BundledSetupProfileKeys()
	for key, p := range m.profiles {
		if _, isBundled := bundled[key]; isBundled {
			continue
		}
		out = append(out, store.FleetSetupProfileSummary{
			Key: key, Name: p.Name, Description: p.Description,
			Domain: p.Domain, StepCount: len(p.Steps), Source: "custom",
		})
	}
	return out
}

func (m *memorySetupProfileStore) Save(_ context.Context, key string, profile any) error {
	if fleet.IsBundledSetupProfileKey(key) {
		return store.ErrBundledSetupProfileImmutable
	}
	p, err := normalizeSetupProfile(profile)
	if err != nil {
		return err
	}
	p.Key = key
	if err := p.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles[key] = cloneSetupProfile(p)
	return nil
}

func (m *memorySetupProfileStore) Delete(_ context.Context, key string) error {
	if fleet.IsBundledSetupProfileKey(key) {
		return store.ErrBundledSetupProfileImmutable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.profiles, key)
	return nil
}

func cloneSetupProfile(p *fleet.SetupProfile) *fleet.SetupProfile {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return p
	}
	var out fleet.SetupProfile
	if err := json.Unmarshal(data, &out); err != nil {
		return p
	}
	return &out
}

func normalizeSetupProfile(profile any) (*fleet.SetupProfile, error) {
	switch v := profile.(type) {
	case *fleet.SetupProfile:
		return v, nil
	case fleet.SetupProfile:
		cfg := v
		return &cfg, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("normalize setup profile: %w", err)
		}
		return fleet.ParseSetupProfileYAML(data)
	}
}

func getSetupProfileStore(svc *store.Services) store.FleetSetupProfileStore {
	if svc != nil && svc.FleetSetupProfiles != nil {
		return svc.FleetSetupProfiles
	}
	return fallbackSetupProfileStore
}
