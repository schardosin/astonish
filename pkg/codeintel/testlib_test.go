package codeintel

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/SAP/astonish/pkg/codeintel/internal/treesitter"
)

// resolveTestLibraryPath returns a usable libastonish-treesitter.so path, or "".
func resolveTestLibraryPath() string {
	if path := os.Getenv("ASTONISH_TREESITTER_LIB"); path != "" {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	candidates := []string{
		treesitter.DefaultLibraryPath,
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		pkgDir := filepath.Dir(thisFile)
		candidates = append(candidates,
			filepath.Join(pkgDir, "native", "dist", "libastonish-treesitter.so"),
		)
	}
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return ""
}
