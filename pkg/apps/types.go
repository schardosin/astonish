package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// VisualApp represents a saved generative-UI app stored as YAML on disk.
type VisualApp struct {
	Name        string       `json:"name" yaml:"name"`
	Description string       `json:"description" yaml:"description"`
	Code        string       `json:"code" yaml:"code"`
	Version     int          `json:"version" yaml:"version"`
	DataSources []DataSource `json:"dataSources,omitempty" yaml:"data_sources,omitempty"`
	CreatedAt   time.Time    `json:"createdAt" yaml:"created_at"`
	UpdatedAt   time.Time    `json:"updatedAt" yaml:"updated_at"`
	SessionID   string       `json:"sessionId,omitempty" yaml:"session_id,omitempty"`
}

// DataSource defines a data feed for a running app (Phase 4).
type DataSource struct {
	ID       string         `json:"id" yaml:"id"`
	Type     string         `json:"type" yaml:"type"`     // "mcp_tool", "http_api", "static"
	Config   map[string]any `json:"config" yaml:"config"`
	Interval string         `json:"interval,omitempty" yaml:"interval,omitempty"`
}

// AppListItem is the lightweight representation returned by ListApps.
type AppListItem struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AppsDir returns the directory where apps are stored (~/.config/astonish/apps/).
func AppsDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config dir: %w", err)
	}
	return filepath.Join(configDir, "astonish", "apps"), nil
}

// slugify converts a title to a filesystem-safe name.
var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = nonAlphanumRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "untitled_app"
	}
	return s
}

// SaveApp writes (or overwrites) an app YAML file to the apps directory.
func SaveApp(app *VisualApp) (string, error) {
	dir, err := AppsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create apps dir: %w", err)
	}

	app.UpdatedAt = time.Now()
	if app.CreatedAt.IsZero() {
		app.CreatedAt = app.UpdatedAt
	}

	data, err := yaml.Marshal(app)
	if err != nil {
		return "", fmt.Errorf("cannot marshal app: %w", err)
	}

	filename := Slugify(app.Name) + ".yaml"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("cannot write app file: %w", err)
	}
	return path, nil
}

// LoadApp reads an app from disk by its slugified name.
func LoadApp(name string) (*VisualApp, error) {
	dir, err := AppsDir()
	if err != nil {
		return nil, err
	}

	filename := Slugify(name) + ".yaml"
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		return nil, fmt.Errorf("cannot read app %q: %w", name, err)
	}

	var app VisualApp
	if err := yaml.Unmarshal(data, &app); err != nil {
		return nil, fmt.Errorf("cannot parse app %q: %w", name, err)
	}
	return &app, nil
}

// DeleteApp removes an app YAML file from disk.
func DeleteApp(name string) error {
	dir, err := AppsDir()
	if err != nil {
		return err
	}

	filename := Slugify(name) + ".yaml"
	path := filepath.Join(dir, filename)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("cannot delete app %q: %w", name, err)
	}
	return nil
}

// ListApps returns all saved apps sorted by most recently updated first.
func ListApps() ([]AppListItem, error) {
	dir, err := AppsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []AppListItem{}, nil
		}
		return nil, fmt.Errorf("cannot read apps dir: %w", err)
	}

	var items []AppListItem
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var app VisualApp
		if err := yaml.Unmarshal(data, &app); err != nil {
			continue
		}

		items = append(items, AppListItem{
			Name:        app.Name,
			Description: app.Description,
			Version:     app.Version,
			UpdatedAt:   app.UpdatedAt,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})

	return items, nil
}
