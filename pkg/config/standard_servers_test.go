package config

import "testing"

// TestIsStandardServerInstalled_DBGetter verifies that when a DB-backed
// SecretGetter is registered (as in platform mode), IsStandardServerInstalled
// correctly resolves the API key from the getter.
func TestIsStandardServerInstalled_DBGetter(t *testing.T) {
	// Simulate a DB-backed getter that returns a key for "tavily"
	dbSecrets := map[string]string{
		"web_servers.tavily.api_key": "tvly-abc123",
	}
	SetInstalledSecretGetter(func(key string) string {
		return dbSecrets[key]
	})
	defer SetInstalledSecretGetter(nil)

	if !IsStandardServerInstalled("tavily") {
		t.Error("expected tavily to be installed when DB getter returns a key")
	}
}

// TestIsStandardServerInstalled_DBGetter_Missing verifies that when the
// DB-backed getter does NOT have the key, the server shows as not installed.
// This is the regression test for the bug where a stale LoadAppConfig()
// fallback would return true even after removal from the DB.
func TestIsStandardServerInstalled_DBGetter_Missing(t *testing.T) {
	// Simulate a DB-backed getter that has NO key for "tavily"
	SetInstalledSecretGetter(func(key string) string {
		return "" // not found
	})
	defer SetInstalledSecretGetter(nil)

	if IsStandardServerInstalled("tavily") {
		t.Error("expected tavily to NOT be installed when DB getter returns empty")
	}
}

// TestIsStandardServerInstalled_NoGetter verifies that with no getter
// registered at all, the server shows as not installed (no panic, no fallback).
func TestIsStandardServerInstalled_NoGetter(t *testing.T) {
	SetInstalledSecretGetter(nil)

	if IsStandardServerInstalled("tavily") {
		t.Error("expected tavily to NOT be installed when no getter is registered")
	}
}

// TestIsStandardServerInstalled_KeylessAlwaysTrue verifies that servers
// with no required environment variables (keyless) always report as installed.
func TestIsStandardServerInstalled_KeylessAlwaysTrue(t *testing.T) {
	SetInstalledSecretGetter(nil) // no getter at all

	// Find a keyless standard server from the registry
	servers := GetStandardServers()
	var keylessID string
	for _, srv := range servers {
		if len(srv.EnvVars) == 0 {
			keylessID = srv.ID
			break
		}
	}

	if keylessID == "" {
		t.Skip("no keyless standard servers found in registry")
	}

	if !IsStandardServerInstalled(keylessID) {
		t.Errorf("expected keyless server %q to always be installed", keylessID)
	}
}

// TestIsStandardServerInstalled_PlatformMode_FileStoreNotUsed is the
// key regression test: after the fix, even if a file-based getter would
// return empty, but the DB-backed getter returns the key, it works.
// Conversely, if ONLY the file-based getter has the key and the DB getter
// is registered (overwriting the file getter), the DB getter is used.
func TestIsStandardServerInstalled_PlatformMode_FileStoreNotUsed(t *testing.T) {
	// Simulate: the DB getter has the key (the normal install path writes to DB)
	SetInstalledSecretGetter(func(key string) string {
		if key == "web_servers.tavily.api_key" {
			return "tvly-from-db"
		}
		return ""
	})
	defer SetInstalledSecretGetter(nil)

	if !IsStandardServerInstalled("tavily") {
		t.Error("expected tavily installed when DB getter has the key")
	}

	// Now simulate: getter returns empty (simulates the bug where chat_factory
	// overwrote the DB getter with a file-based getter that doesn't have the key)
	SetInstalledSecretGetter(func(key string) string {
		return "" // file-based store doesn't have it
	})

	if IsStandardServerInstalled("tavily") {
		t.Error("expected tavily NOT installed when getter (overwritten to file-based) returns empty — this was the bug")
	}
}
