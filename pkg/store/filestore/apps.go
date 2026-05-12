package filestore

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/store"
)

// AppStoreWrapper wraps the existing apps package-level functions behind the
// store.AppStore interface.
type AppStoreWrapper struct{}

// NewAppStore creates an AppStore backed by the existing file-based app storage.
func NewAppStore() store.AppStore {
	return &AppStoreWrapper{}
}

func (w *AppStoreWrapper) Save(_ context.Context, app any) (string, error) {
	va, ok := app.(*apps.VisualApp)
	if !ok {
		return "", fmt.Errorf("expected *apps.VisualApp, got %T", app)
	}
	return apps.SaveApp(va)
}

func (w *AppStoreWrapper) Load(_ context.Context, slug string) (any, error) {
	return apps.LoadApp(slug)
}

func (w *AppStoreWrapper) Delete(_ context.Context, slug string) error {
	return apps.DeleteApp(slug)
}

func (w *AppStoreWrapper) List(_ context.Context) ([]store.AppListItem, error) {
	items, err := apps.ListApps()
	if err != nil {
		return nil, err
	}
	result := make([]store.AppListItem, len(items))
	for i, item := range items {
		result[i] = store.AppListItem{
			Name:        item.Name,
			Description: item.Description,
			Version:     item.Version,
			UpdatedAt:   item.UpdatedAt,
		}
	}
	return result, nil
}

// Compile-time check.
var _ store.AppStore = (*AppStoreWrapper)(nil)
