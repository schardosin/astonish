package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// GetDistFS returns the embedded dist filesystem
// Supports versioned builds where output is dist/{version}/
func GetDistFS() fs.FS {
	// First, check if dist exists
	entries, err := distFS.ReadDir("dist")
	if err != nil || len(entries) == 0 {
		return nil
	}

	// Check for versioned subfolder (e.g., dist/1.0.0/)
	// If there's exactly one directory entry, it's likely the version folder
	if len(entries) == 1 && entries[0].IsDir() {
		versionDir := entries[0].Name()
		subFS, err := fs.Sub(distFS, "dist/"+versionDir)
		if err != nil {
			return nil
		}
		return subFS
	}

	// Fallback: check for index.html directly in dist (legacy non-versioned build)
	if _, err := distFS.Open("dist/index.html"); err == nil {
		subFS, err := fs.Sub(distFS, "dist")
		if err != nil {
			return nil
		}
		return subFS
	}

	return nil
}
