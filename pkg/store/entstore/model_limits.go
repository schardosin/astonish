package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	platforment "github.com/SAP/astonish/ent/platform"
	"github.com/SAP/astonish/pkg/store"
)

const modelLimitsKey = "model_limits"

// modelLimitsStore implements store.ModelLimitsStore using platform_settings.
type modelLimitsStore struct {
	client *platforment.Client
	mu     sync.Mutex
}

// ModelLimits returns the platform-scoped model limits store.
func (s *Store) ModelLimits() store.ModelLimitsStore {
	return &modelLimitsStore{client: s.platformClient}
}

func (m *modelLimitsStore) Get(ctx context.Context, provider, model string) (*store.ModelLimitEntry, error) {
	all, err := m.loadAll(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := all[store.ModelLimitsKey(provider, model)]
	if !ok {
		return nil, nil
	}
	cp := entry
	return &cp, nil
}

func (m *modelLimitsStore) UpsertMaxOutput(ctx context.Context, provider, model string, max int, source string) error {
	if max <= 0 {
		return fmt.Errorf("max output tokens must be positive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	all, err := m.loadAll(ctx)
	if err != nil {
		return err
	}
	key := store.ModelLimitsKey(provider, model)
	if existing, ok := all[key]; ok && existing.MaxOutputTokens > 0 && existing.MaxOutputTokens <= max {
		// Already at or below the learned cap — do not raise.
		return nil
	}
	entry := all[key]
	entry.MaxOutputTokens = max
	if source != "" {
		entry.Source = source
	} else {
		entry.Source = "learned_400"
	}
	entry.UpdatedAt = time.Now().UTC()
	all[key] = entry
	return m.saveAll(ctx, all)
}

func (m *modelLimitsStore) UpsertSupportsTools(ctx context.Context, provider, model string, supports bool, source string) error {
	// We only learn the negative from provider errors.
	if supports {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	all, err := m.loadAll(ctx)
	if err != nil {
		return err
	}
	key := store.ModelLimitsKey(provider, model)
	if existing, ok := all[key]; ok && existing.SupportsTools != nil && !*existing.SupportsTools {
		// Already learned false — keep it.
		return nil
	}
	entry := all[key]
	f := false
	entry.SupportsTools = &f
	if source != "" {
		entry.Source = source
	} else {
		entry.Source = "learned_400"
	}
	entry.UpdatedAt = time.Now().UTC()
	all[key] = entry
	return m.saveAll(ctx, all)
}

func (m *modelLimitsStore) loadAll(ctx context.Context) (map[string]store.ModelLimitEntry, error) {
	row, err := m.client.PlatformSetting.Get(ctx, modelLimitsKey)
	if err != nil {
		if platforment.IsNotFound(err) {
			return map[string]store.ModelLimitEntry{}, nil
		}
		return nil, fmt.Errorf("get model_limits: %w", err)
	}
	data, err := json.Marshal(row.Value)
	if err != nil {
		return nil, fmt.Errorf("marshal model_limits value: %w", err)
	}
	var all map[string]store.ModelLimitEntry
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("unmarshal model_limits: %w", err)
	}
	if all == nil {
		all = map[string]store.ModelLimitEntry{}
	}
	return all, nil
}

func (m *modelLimitsStore) saveAll(ctx context.Context, all map[string]store.ModelLimitEntry) error {
	data, err := json.Marshal(all)
	if err != nil {
		return fmt.Errorf("marshal model_limits: %w", err)
	}
	var valueMap map[string]any
	if err := json.Unmarshal(data, &valueMap); err != nil {
		return fmt.Errorf("unmarshal model_limits to map: %w", err)
	}

	row, err := m.client.PlatformSetting.Get(ctx, modelLimitsKey)
	if err != nil {
		if platforment.IsNotFound(err) {
			_, err = m.client.PlatformSetting.Create().
				SetID(modelLimitsKey).
				SetValue(valueMap).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("create model_limits: %w", err)
			}
			return nil
		}
		return fmt.Errorf("get model_limits for upsert: %w", err)
	}
	_, err = row.Update().SetValue(valueMap).Save(ctx)
	if err != nil {
		return fmt.Errorf("update model_limits: %w", err)
	}
	return nil
}

var _ store.ModelLimitsStore = (*modelLimitsStore)(nil)
