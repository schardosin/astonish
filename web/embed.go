package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// GetDistFS returns the embedded dist filesystem
// Returns nil if dist directory doesn't exist (not built)
func GetDistFS() fs.FS {
	// Check if dist exists
	entries, err := distFS.ReadDir("dist")
	if err != nil || len(entries) == 0 {
		return nil
	}

	// Return the dist subdirectory as the root
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}

	return subFS
}
