package codeintel

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
)

//go:embed queries/*.scm
var queryFS embed.FS

type languageSpec struct {
	Name      string
	QueryFile string
}

func specForPath(path string) (languageSpec, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return languageSpec{Name: "go", QueryFile: "queries/go.scm"}, true
	case ".ts":
		return languageSpec{Name: "typescript", QueryFile: "queries/typescript.scm"}, true
	case ".tsx":
		return languageSpec{Name: "tsx", QueryFile: "queries/typescript.scm"}, true
	case ".js", ".jsx", ".mjs", ".cjs":
		return languageSpec{Name: "javascript", QueryFile: "queries/javascript.scm"}, true
	case ".py":
		return languageSpec{Name: "python", QueryFile: "queries/python.scm"}, true
	default:
		return languageSpec{}, false
	}
}

func loadQuery(spec languageSpec) ([]byte, error) {
	query, err := queryFS.ReadFile(spec.QueryFile)
	if err != nil {
		return nil, fmt.Errorf("load %s query: %w", spec.Name, err)
	}
	return query, nil
}
