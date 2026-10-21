package config

import "testing"

// TestStorageConfigIsPlatformMode pins the predicate that gates host-filesystem
// reads for multi-tenant containment. Both postgres and sqlite are platform
// (multi-user) backends; only the file/default backend is trusted single-tenant.
// This is the fix for the scheduler adaptive-report ART guard that previously
// used IsPostgres() alone and missed SQLite platform deployments.
func TestStorageConfigIsPlatformMode(t *testing.T) {
	cases := []struct {
		backend string
		want    bool
	}{
		{"postgres", true},
		{"sqlite", true},
		{"file", false},
		{"", false},
	}
	for _, tc := range cases {
		c := &StorageConfig{Backend: tc.backend}
		if got := c.IsPlatformMode(); got != tc.want {
			t.Errorf("IsPlatformMode(backend=%q) = %v, want %v", tc.backend, got, tc.want)
		}
	}
}
