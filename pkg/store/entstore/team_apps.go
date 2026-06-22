package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	teament "github.com/schardosin/astonish/ent/team"
	"github.com/schardosin/astonish/ent/team/app"
	"github.com/schardosin/astonish/ent/team/appstate"
	"github.com/schardosin/astonish/pkg/store"

	"github.com/google/uuid"
)

// teamAppStore implements store.AppStore using the Ent team client.
type teamAppStore struct {
	client *teament.Client
}

var _ store.AppStore = (*teamAppStore)(nil)

func (s *teamAppStore) Save(ctx context.Context, appDef any) (string, error) {
	m, ok := appDef.(map[string]any)
	if !ok {
		return "", fmt.Errorf("entstore: AppStore.Save: expected map[string]any, got %T", appDef)
	}

	name, _ := m["name"].(string)
	if name == "" {
		return "", fmt.Errorf("entstore: AppStore.Save: missing 'name' field")
	}
	description, _ := m["description"].(string)
	slug := slugify(name)

	code, err := json.Marshal(appDef)
	if err != nil {
		return "", fmt.Errorf("entstore: AppStore.Save: marshal code: %w", err)
	}

	version := 1
	if v, ok := m["version"].(float64); ok {
		version = int(v)
	}

	// Try update first.
	n, err := s.client.App.Update().
		Where(app.SlugEQ(slug)).
		SetName(name).
		SetDescription(description).
		SetCode(string(code)).
		SetVersion(version).
		Save(ctx)
	if err != nil {
		return "", fmt.Errorf("entstore: AppStore.Save: update: %w", err)
	}
	if n == 0 {
		// Create new.
		_, err = s.client.App.Create().
			SetSlug(slug).
			SetName(name).
			SetDescription(description).
			SetCode(string(code)).
			SetVersion(version).
			Save(ctx)
		if err != nil {
			return "", fmt.Errorf("entstore: AppStore.Save: create: %w", err)
		}
	}
	return slug, nil
}

func (s *teamAppStore) Load(ctx context.Context, slug string) (any, error) {
	ent, err := s.client.App.Query().
		Where(app.SlugEQ(slug)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("app %q not found", slug)
		}
		return nil, fmt.Errorf("entstore: AppStore.Load: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(ent.Code), &result); err != nil {
		return nil, fmt.Errorf("entstore: AppStore.Load: unmarshal code: %w", err)
	}
	return result, nil
}

func (s *teamAppStore) Delete(ctx context.Context, slug string) error {
	_, err := s.client.App.Delete().
		Where(app.SlugEQ(slug)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: AppStore.Delete: %w", err)
	}
	return nil
}

func (s *teamAppStore) List(ctx context.Context) ([]store.AppListItem, error) {
	apps, err := s.client.App.Query().
		Order(app.ByName()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: AppStore.List: %w", err)
	}

	items := make([]store.AppListItem, len(apps))
	for i, a := range apps {
		items[i] = store.AppListItem{
			Slug:        a.Slug,
			Name:        a.Name,
			Description: a.Description,
			Version:     a.Version,
			UpdatedAt:   a.UpdatedAt,
		}
	}
	return items, nil
}

// teamAppStateStore implements store.AppStateStore using the Ent team client.
type teamAppStateStore struct {
	client *teament.Client
}

var _ store.AppStateStore = (*teamAppStateStore)(nil)

func (s *teamAppStateStore) Get(ctx context.Context, appSlug, key string) (any, error) {
	appID, err := s.resolveAppID(ctx, appSlug)
	if err != nil {
		return nil, err
	}

	// Use a zero UUID for team-level state (no specific user).
	state, err := s.client.AppState.Query().
		Where(
			appstate.AppIDEQ(appID),
			appstate.KeyEQ(key),
		).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entstore: AppStateStore.Get: %w", err)
	}
	// The value is stored as map[string]any with a "v" wrapper.
	if v, ok := state.Value["v"]; ok {
		return v, nil
	}
	return state.Value, nil
}

func (s *teamAppStateStore) Set(ctx context.Context, appSlug, key string, value any) error {
	appID, err := s.resolveAppID(ctx, appSlug)
	if err != nil {
		return err
	}

	wrapped := map[string]any{"v": value}
	userID := uuid.Nil // team-level state

	// Try update first.
	n, err := s.client.AppState.Update().
		Where(
			appstate.AppIDEQ(appID),
			appstate.UserIDEQ(userID),
			appstate.KeyEQ(key),
		).
		SetValue(wrapped).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: AppStateStore.Set: update: %w", err)
	}
	if n == 0 {
		_, err = s.client.AppState.Create().
			SetAppID(appID).
			SetUserID(userID).
			SetKey(key).
			SetValue(wrapped).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: AppStateStore.Set: create: %w", err)
		}
	}
	return nil
}

func (s *teamAppStateStore) Delete(ctx context.Context, appSlug, key string) error {
	appID, err := s.resolveAppID(ctx, appSlug)
	if err != nil {
		return err
	}

	_, err = s.client.AppState.Delete().
		Where(
			appstate.AppIDEQ(appID),
			appstate.KeyEQ(key),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: AppStateStore.Delete: %w", err)
	}
	return nil
}

func (s *teamAppStateStore) List(ctx context.Context, appSlug string) (map[string]any, error) {
	appID, err := s.resolveAppID(ctx, appSlug)
	if err != nil {
		return nil, err
	}

	states, err := s.client.AppState.Query().
		Where(appstate.AppIDEQ(appID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: AppStateStore.List: %w", err)
	}

	result := make(map[string]any, len(states))
	for _, st := range states {
		if v, ok := st.Value["v"]; ok {
			result[st.Key] = v
		} else {
			result[st.Key] = st.Value
		}
	}
	return result, nil
}

func (s *teamAppStateStore) resolveAppID(ctx context.Context, appSlug string) (uuid.UUID, error) {
	a, err := s.client.App.Query().
		Where(app.SlugEQ(appSlug)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return uuid.Nil, fmt.Errorf("app %q not found", appSlug)
		}
		return uuid.Nil, fmt.Errorf("entstore: resolveAppID: %w", err)
	}
	return a.ID, nil
}

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric/dash characters.
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
