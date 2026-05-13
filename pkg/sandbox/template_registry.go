package sandbox

import "github.com/schardosin/astonish/pkg/sandbox/tmplmeta"

// TemplateMeta is re-exported from pkg/sandbox/tmplmeta.
type TemplateMeta = tmplmeta.TemplateMeta

// TemplateRegistry is re-exported from pkg/sandbox/tmplmeta.
type TemplateRegistry = tmplmeta.TemplateRegistry

// NewTemplateRegistry creates a new registry backed by a JSON file.
func NewTemplateRegistry() (*TemplateRegistry, error) {
	return tmplmeta.NewTemplateRegistry()
}

// sandboxDataDir returns the directory for sandbox data files.
func sandboxDataDir() (string, error) {
	return tmplmeta.SandboxDataDir()
}
