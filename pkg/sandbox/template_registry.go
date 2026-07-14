package sandbox

import "github.com/schardosin/astonish/pkg/sandbox/tmplmeta"

// TemplateMeta is re-exported from pkg/sandbox/tmplmeta.
type TemplateMeta = tmplmeta.TemplateMeta

// BootstrapFileMeta is re-exported from pkg/sandbox/tmplmeta.
type BootstrapFileMeta = tmplmeta.BootstrapFileMeta

// TemplateRegistry is re-exported from pkg/sandbox/tmplmeta.
type TemplateRegistry = tmplmeta.TemplateRegistry

// NewTemplateRegistry creates a new registry backed by a JSON file.
func NewTemplateRegistry() (*TemplateRegistry, error) {
	return tmplmeta.NewTemplateRegistry()
}

// NewInMemoryRegistry creates an in-memory template registry for tests.
func NewInMemoryRegistry(dir string) *TemplateRegistry {
	return tmplmeta.NewInMemoryRegistry(dir)
}

// sandboxDataDir returns the directory for sandbox data files.
func sandboxDataDir() (string, error) {
	return tmplmeta.SandboxDataDir()
}
