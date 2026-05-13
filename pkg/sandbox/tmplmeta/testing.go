package tmplmeta

import "path/filepath"

// NewInMemoryRegistry returns a TemplateRegistry with an empty in-memory map
// and a file path rooted at the provided directory. Intended for tests that
// need to construct a registry without loading from or saving to a real user
// data directory.
//
// If dir is empty, saving the registry will fail; callers that only Get/Add
// without persisting can pass "".
func NewInMemoryRegistry(dir string) *TemplateRegistry {
	fp := ""
	if dir != "" {
		fp = filepath.Join(dir, "templates.json")
	}
	return &TemplateRegistry{
		templates: make(map[string]*TemplateMeta),
		filePath:  fp,
	}
}

// SeedForTest adds a TemplateMeta directly to the in-memory map without
// reloading from disk or persisting. Intended for tests only.
func (r *TemplateRegistry) SeedForTest(meta *TemplateMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[meta.Name] = meta
}
