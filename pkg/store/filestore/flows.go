package filestore

import (
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/store"
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
