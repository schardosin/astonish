package codeintel

type ScopeGraph struct {
	File string       `json:"file"`
	Defs []Definition `json:"defs"`
	Refs []Reference  `json:"refs"`
	// Imports is reserved for a future import-extraction query pass; nothing
	// populates it yet and tools must not rely on it.
	Imports []Import `json:"imports,omitempty"`
}

type Definition struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Signature string `json:"signature"`
}

type Reference struct {
	Name        string `json:"name"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	ContextLine string `json:"context_line"`
}

// Import describes a file-level import. Not populated until import-capture
// queries are added; kept for forward-compatible JSON cache shape.
type Import struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
	Line  int    `json:"line"`
}
