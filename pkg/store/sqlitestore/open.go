package sqlitestore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver
)

// openDB opens (or creates) a SQLite database at the given path with
// production-ready PRAGMA settings. The parent directory is created if it
// does not exist.
func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", path, err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	// Apply performance and safety pragmas.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",        // Concurrent reads, serialized writes
		"PRAGMA busy_timeout=5000",       // Wait up to 5s for write lock
		"PRAGMA foreign_keys=ON",         // Enforce FK constraints
		"PRAGMA synchronous=NORMAL",      // Safe with WAL, better perf than FULL
		"PRAGMA cache_size=-8000",        // 8 MB page cache
		"PRAGMA temp_store=MEMORY",       // Temp tables in memory
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q on %s: %w", p, path, err)
		}
	}

	// SQLite serializes writes internally. A single open connection avoids
	// SQLITE_BUSY at the Go level; WAL mode allows concurrent readers.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Persistent connection

	return db, nil
}
