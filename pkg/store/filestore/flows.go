package filestore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

// FlowStoreWrapper wraps the existing flowstore.Store behind the
// store.FlowStore interface. Since the existing flowstore.Store is
// stateless (re-created on every call), this wrapper creates a new
// instance for each operation.
type FlowStoreWrapper struct{}

// NewFlowStore creates a FlowStore backed by the existing file-based flow store.
func NewFlowStore() store.FlowStore {
	return &FlowStoreWrapper{}
}

func (w *FlowStoreWrapper) ListAllFlows() []store.FlowSummary {
	fs, err := flowstore.NewStore()
	if err != nil {
		return nil
	}
	flows := fs.ListAllFlows()
	result := make([]store.FlowSummary, len(flows))
	for i, f := range flows {
		result[i] = store.FlowSummary{
			Name:        f.Name,
			Description: f.Description,
			Tags:        f.Tags,
			TapName:     f.TapName,
			Installed:   f.Installed,
			LocalPath:   f.LocalPath,
		}
	}
	return result
}

func (w *FlowStoreWrapper) ListFlowsByType(types []string) []store.FlowSummary {
	if len(types) == 0 {
		return nil
	}
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}

	// Scan the same directories that drill discovery uses.
	var dirs []string
	if sysDir, err := config.GetAgentsDir(); err == nil {
		dirs = append(dirs, sysDir)
	}
	if info, err := os.Stat("agents"); err == nil && info.IsDir() {
		dirs = append(dirs, "agents")
	}
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		dirs = append(dirs, flowsDir)
	}

	seen := make(map[string]bool)
	var result []store.FlowSummary

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
				continue
			}
			name := entry.Name()[:len(entry.Name())-5]
			if seen[name] {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var parsed struct {
				Type        string   `yaml:"type"`
				Description string   `yaml:"description"`
				Suite       string   `yaml:"suite"`
				Tags        []string `yaml:"tags"`
			}
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				continue
			}

			if !typeSet[parsed.Type] {
				continue
			}

			seen[name] = true
			result = append(result, store.FlowSummary{
				Name:        name,
				Description: parsed.Description,
				Type:        parsed.Type,
				Suite:       parsed.Suite,
				Tags:        parsed.Tags,
				LocalPath:   path,
				Installed:   true,
			})
		}
	}
	return result
}

func (w *FlowStoreWrapper) GetFlow(name string) (string, error) {
	// Try system agents directory
	if sysDir, err := config.GetAgentsDir(); err == nil {
		path := filepath.Join(sysDir, name+".yaml")
		if data, err := os.ReadFile(path); err == nil {
			return string(data), nil
		}
	}

	// Try local agents directory
	path := filepath.Join("agents", name+".yaml")
	if data, err := os.ReadFile(path); err == nil {
		return string(data), nil
	}

	// Try user flows directory
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		path := filepath.Join(flowsDir, name+".yaml")
		if data, err := os.ReadFile(path); err == nil {
			return string(data), nil
		}
	}

	// Try installed store flows
	if fs, err := flowstore.NewStore(); err == nil {
		if flowPath, ok := fs.GetInstalledFlowPath(flowstore.OfficialStoreName, name); ok {
			if data, err := os.ReadFile(flowPath); err == nil {
				return string(data), nil
			}
		}
	}

	return "", fmt.Errorf("flow %q not found", name)
}

func (w *FlowStoreWrapper) SaveFlow(name string, yamlContent string) error {
	flowsDir, err := flowstore.GetFlowsDir()
	if err != nil {
		return fmt.Errorf("failed to get flows directory: %w", err)
	}

	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create flows directory: %w", err)
	}

	path := filepath.Join(flowsDir, name+".yaml")
	return os.WriteFile(path, []byte(yamlContent), 0644)
}

func (w *FlowStoreWrapper) DeleteFlow(name string) error {
	// Search for the flow in all locations
	locations := []string{}

	if sysDir, err := config.GetAgentsDir(); err == nil {
		locations = append(locations, filepath.Join(sysDir, name+".yaml"))
	}
	locations = append(locations, filepath.Join("agents", name+".yaml"))
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		locations = append(locations, filepath.Join(flowsDir, name+".yaml"))
	}

	for _, path := range locations {
		if _, err := os.Stat(path); err == nil {
			return os.Remove(path)
		}
	}

	return fmt.Errorf("flow %q not found", name)
}

func (w *FlowStoreWrapper) GetTaps() []store.FlowTap {
	fs, err := flowstore.NewStore()
	if err != nil {
		return nil
	}
	taps := fs.GetTaps()
	result := make([]store.FlowTap, len(taps))
	for i, t := range taps {
		result[i] = store.FlowTap{
			Name:   t.Name,
			URL:    t.URL,
			Branch: t.Branch,
		}
	}
	return result
}

func (w *FlowStoreWrapper) AddTap(urlOrShorthand string, alias string) (string, error) {
	fs, err := flowstore.NewStore()
	if err != nil {
		return "", err
	}
	return fs.AddTap(urlOrShorthand, alias)
}

func (w *FlowStoreWrapper) RemoveTap(name string) error {
	fs, err := flowstore.NewStore()
	if err != nil {
		return err
	}
	return fs.RemoveTap(name)
}

func (w *FlowStoreWrapper) GetStoreDir() string {
	fs, err := flowstore.NewStore()
	if err != nil {
		return ""
	}
	return fs.GetStoreDir()
}

// Compile-time check.
var _ store.FlowStore = (*FlowStoreWrapper)(nil)
