package entstore

import (
	"context"
	"encoding/json"
	"fmt"

	personalent "github.com/schardosin/astonish/ent/personal"
	"github.com/schardosin/astonish/ent/personal/app"
	"github.com/schardosin/astonish/ent/personal/appstate"
	"github.com/schardosin/astonish/pkg/store"
)

// personalAppStore implements store.AppStore for personal scope.
type personalAppStore struct {
	client *personalent.Client
}

var _ store.AppStore = (*personalAppStore)(nil)

func (as *personalAppStore) Save(ctx context.Context, appDef any) (string, error) {
	// Marshal to extract fields.
	data, err := json.Marshal(appDef)
	if err != nil {
		return "", fmt.Errorf("marshal app: %w", err)
	}
	var def map[string]any
	if err := json.Unmarshal(data, &def); err != nil {
		return "", fmt.Errorf("unmarshal app: %w", err)
	}

	slug, _ := def["slug"].(string)
	if slug == "" {
		slug, _ = def["name"].(string)
	}
	name, _ := def["name"].(string)
	description, _ := def["description"].(string)
	code, _ := def["code"].(string)
	sessionID, _ := def["session_id"].(string)
	if sessionID == "" {
		sessionID, _ = def["sessionId"].(string)
	}

	if slug == "" {
		return "", fmt.Errorf("app definition must include a slug")
	}
	if name == "" {
		name = slug
	}

	// Check if exists.
	existing, err := as.client.App.Query().
		Where(app.SlugEQ(slug)).
		Only(ctx)
	if err != nil && !personalent.IsNotFound(err) {
		return "", err
	}

	if existing != nil {
		// Update: increment version.
		update := existing.Update().
			SetName(name).
			SetDescription(description).
			SetCode(code).
			SetSessionID(sessionID).
			AddVersion(1)
		return slug, update.Exec(ctx)
	}

	// Create.
	_, err = as.client.App.Create().
		SetSlug(slug).
		SetName(name).
		SetDescription(description).
		SetCode(code).
		SetSessionID(sessionID).
		Save(ctx)
	if err != nil {
		return "", err
	}
	return slug, nil
}

func (as *personalAppStore) Load(ctx context.Context, slug string) (any, error) {
	ent, err := as.client.App.Query().
		Where(app.SlugEQ(slug)).
		Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return map[string]any{
		"slug":        ent.Slug,
		"name":        ent.Name,
		"description": ent.Description,
		"code":        ent.Code,
		"version":     ent.Version,
		"session_id":  ent.SessionID,
		"created_at":  ent.CreatedAt,
		"updated_at":  ent.UpdatedAt,
	}, nil
}

func (as *personalAppStore) Delete(ctx context.Context, slug string) error {
	_, err := as.client.App.Delete().
		Where(app.SlugEQ(slug)).
		Exec(ctx)
	return err
}

func (as *personalAppStore) List(ctx context.Context) ([]store.AppListItem, error) {
	ents, err := as.client.App.Query().
		Order(app.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]store.AppListItem, len(ents))
	for i, e := range ents {
		items[i] = store.AppListItem{
			Name:        e.Name,
			Description: e.Description,
			Version:     e.Version,
			UpdatedAt:   e.UpdatedAt,
			Scope:       "personal",
		}
	}
	return items, nil
}

// ===========================================================================
// personalAppStateStore implements store.AppStateStore for personal scope.
// ===========================================================================

type personalAppStateStore struct {
	client *personalent.Client
}

var _ store.AppStateStore = (*personalAppStateStore)(nil)

func (ss *personalAppStateStore) Get(ctx context.Context, appSlug, key string) (any, error) {
	// Look up app by slug to get its ID.
	a, err := ss.client.App.Query().
		Where(app.SlugEQ(appSlug)).
		Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	state, err := ss.client.AppState.Query().
		Where(
			appstate.AppIDEQ(a.ID),
			appstate.KeyEQ(key),
		).
		Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return state.Value, nil
}

func (ss *personalAppStateStore) Set(ctx context.Context, appSlug, key string, value any) error {
	// Look up app by slug.
	a, err := ss.client.App.Query().
		Where(app.SlugEQ(appSlug)).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("app %q not found: %w", appSlug, err)
	}

	// Convert value to map[string]any for JSON storage.
	valueMap := map[string]any{"v": value}

	// Check if state already exists.
	existing, err := ss.client.AppState.Query().
		Where(
			appstate.AppIDEQ(a.ID),
			appstate.KeyEQ(key),
		).
		Only(ctx)
	if err != nil && !personalent.IsNotFound(err) {
		return err
	}

	if existing != nil {
		return existing.Update().
			SetValue(valueMap).
			Exec(ctx)
	}

	_, err = ss.client.AppState.Create().
		SetAppID(a.ID).
		SetKey(key).
		SetValue(valueMap).
		Save(ctx)
	return err
}

func (ss *personalAppStateStore) Delete(ctx context.Context, appSlug, key string) error {
	a, err := ss.client.App.Query().
		Where(app.SlugEQ(appSlug)).
		Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil
		}
		return err
	}

	_, err = ss.client.AppState.Delete().
		Where(
			appstate.AppIDEQ(a.ID),
			appstate.KeyEQ(key),
		).
		Exec(ctx)
	return err
}

func (ss *personalAppStateStore) List(ctx context.Context, appSlug string) (map[string]any, error) {
	a, err := ss.client.App.Query().
		Where(app.SlugEQ(appSlug)).
		Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	states, err := ss.client.AppState.Query().
		Where(appstate.AppIDEQ(a.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]any, len(states))
	for _, s := range states {
		result[s.Key] = s.Value
	}
	return result, nil
}
