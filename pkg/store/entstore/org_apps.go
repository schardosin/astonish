package entstore

import (
	"context"
	"encoding/json"
	"fmt"

	orgent "github.com/schardosin/astonish/ent/org"
	"github.com/schardosin/astonish/ent/org/orgapp"
	"github.com/schardosin/astonish/pkg/store"
)

// orgAppStore implements store.AppStore for org-level apps.
type orgAppStore struct {
	client *orgent.Client
}

var _ store.AppStore = (*orgAppStore)(nil)

func (as *orgAppStore) Save(ctx context.Context, app any) (string, error) {
	// Marshal the app definition to a map.
	data, err := json.Marshal(app)
	if err != nil {
		return "", fmt.Errorf("marshal app definition: %w", err)
	}
	var def map[string]any
	if err := json.Unmarshal(data, &def); err != nil {
		return "", fmt.Errorf("unmarshal app definition: %w", err)
	}

	// Extract slug and name from the definition.
	slug, _ := def["slug"].(string)
	if slug == "" {
		slug, _ = def["name"].(string)
	}
	name, _ := def["name"].(string)
	description, _ := def["description"].(string)

	if slug == "" {
		return "", fmt.Errorf("app definition must include a slug")
	}
	if name == "" {
		name = slug
	}

	// Check if app exists.
	existing, err := as.client.OrgApp.Query().
		Where(orgapp.SlugEQ(slug)).
		Only(ctx)
	if err != nil && !orgent.IsNotFound(err) {
		return "", err
	}

	if existing != nil {
		// Update.
		return slug, existing.Update().
			SetName(name).
			SetDescription(description).
			SetDefinition(def).
			Exec(ctx)
	}

	// Create new.
	_, err = as.client.OrgApp.Create().
		SetSlug(slug).
		SetName(name).
		SetDescription(description).
		SetDefinition(def).
		Save(ctx)
	if err != nil {
		return "", err
	}
	return slug, nil
}

func (as *orgAppStore) Load(ctx context.Context, slug string) (any, error) {
	ent, err := as.client.OrgApp.Query().
		Where(orgapp.SlugEQ(slug)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return ent.Definition, nil
}

func (as *orgAppStore) Delete(ctx context.Context, slug string) error {
	_, err := as.client.OrgApp.Delete().
		Where(orgapp.SlugEQ(slug)).
		Exec(ctx)
	return err
}

func (as *orgAppStore) List(ctx context.Context) ([]store.AppListItem, error) {
	ents, err := as.client.OrgApp.Query().
		Order(orgapp.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]store.AppListItem, len(ents))
	for i, e := range ents {
		items[i] = store.AppListItem{
			Name:        e.Name,
			Description: e.Description,
			UpdatedAt:   e.UpdatedAt,
			Scope:       "org",
		}
	}
	return items, nil
}
