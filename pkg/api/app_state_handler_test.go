package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestAppsDir creates a temporary directory structure that mirrors
// ~/.config/astonish/apps/ and redirects os.UserConfigDir() to it
// via XDG_CONFIG_HOME.
func setupTestAppsDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	appsDir := filepath.Join(tmpDir, "astonish", "apps")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return appsDir
}

func TestCloseAndDeleteAppDB(t *testing.T) {
	appsDir := setupTestAppsDir(t)

	// Create fake .db, .db-wal, .db-shm files
	slug := "my_app"
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		path := filepath.Join(appsDir, slug+suffix)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", suffix, err)
		}
	}

	// Verify files exist
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		path := filepath.Join(appsDir, slug+suffix)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist before cleanup", path)
		}
	}

	// Run cleanup
	if err := CloseAndDeleteAppDB("My App"); err != nil {
		t.Fatalf("CloseAndDeleteAppDB: %v", err)
	}

	// Verify all files are gone
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		path := filepath.Join(appsDir, slug+suffix)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", path)
		}
	}
}

func TestCloseAndDeleteAppDB_NoFiles(t *testing.T) {
	setupTestAppsDir(t)

	// Should not error when no .db files exist
	if err := CloseAndDeleteAppDB("nonexistent"); err != nil {
		t.Errorf("CloseAndDeleteAppDB for nonexistent app should not error, got: %v", err)
	}
}

func TestCleanupOrphanAppDBs(t *testing.T) {
	appsDir := setupTestAppsDir(t)

	// Create an orphaned .db (no matching .yaml, old mtime)
	orphanDB := filepath.Join(appsDir, "orphan_app.db")
	orphanWAL := filepath.Join(appsDir, "orphan_app.db-wal")
	if err := os.WriteFile(orphanDB, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanWAL, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set mtime to 10 days ago
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(orphanDB, oldTime, oldTime)
	os.Chtimes(orphanWAL, oldTime, oldTime)

	// Create a non-orphaned .db (has matching .yaml)
	savedDB := filepath.Join(appsDir, "saved_app.db")
	savedYAML := filepath.Join(appsDir, "saved_app.yaml")
	if err := os.WriteFile(savedDB, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savedYAML, []byte("name: Saved App"), 0644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(savedDB, oldTime, oldTime)

	// Create a recent orphaned .db (no matching .yaml, but recent)
	recentDB := filepath.Join(appsDir, "recent_app.db")
	if err := os.WriteFile(recentDB, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	// mtime is now — should NOT be cleaned up

	// Run cleanup with 7-day threshold
	cleaned := CleanupOrphanAppDBs(7 * 24 * time.Hour)

	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}

	// Orphaned old .db should be gone
	if _, err := os.Stat(orphanDB); !os.IsNotExist(err) {
		t.Error("orphan_app.db should have been deleted")
	}
	if _, err := os.Stat(orphanWAL); !os.IsNotExist(err) {
		t.Error("orphan_app.db-wal should have been deleted")
	}

	// Saved app .db should still exist
	if _, err := os.Stat(savedDB); err != nil {
		t.Error("saved_app.db should still exist (has matching .yaml)")
	}

	// Recent orphan .db should still exist
	if _, err := os.Stat(recentDB); err != nil {
		t.Error("recent_app.db should still exist (too recent)")
	}
}

func TestCleanupOrphanAppDBs_EmptyDir(t *testing.T) {
	setupTestAppsDir(t)

	// Should return 0 with no files
	cleaned := CleanupOrphanAppDBs(7 * 24 * time.Hour)
	if cleaned != 0 {
		t.Errorf("expected 0 cleaned, got %d", cleaned)
	}
}
