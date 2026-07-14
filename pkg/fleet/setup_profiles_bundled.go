package fleet

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed bundled/setup-profiles/*.yaml
var bundledSetupProfiles embed.FS

var (
	bundledSetupProfilesOnce sync.Once
	bundledSetupProfilesMap  map[string]*SetupProfile
	bundledSetupProfilesErr  error
)

// LoadBundledSetupProfiles returns embedded setup profiles keyed by filename stem.
func LoadBundledSetupProfiles() (map[string]*SetupProfile, error) {
	bundledSetupProfilesOnce.Do(func() {
		entries, err := fs.ReadDir(bundledSetupProfiles, "bundled/setup-profiles")
		if err != nil {
			bundledSetupProfilesErr = fmt.Errorf("reading bundled setup profiles: %w", err)
			return
		}

		profiles := make(map[string]*SetupProfile)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}

			data, err := fs.ReadFile(bundledSetupProfiles, filepath.Join("bundled/setup-profiles", name))
			if err != nil {
				bundledSetupProfilesErr = fmt.Errorf("reading bundled setup profile %s: %w", name, err)
				return
			}

			profile, err := ParseSetupProfileYAML(data)
			if err != nil {
				bundledSetupProfilesErr = fmt.Errorf("parsing bundled setup profile %s: %w", name, err)
				return
			}

			key := strings.TrimSuffix(name, filepath.Ext(name))
			if profile.Key == "" {
				profile.Key = key
			}
			profiles[key] = profile
		}

		bundledSetupProfilesMap = profiles
	})

	return bundledSetupProfilesMap, bundledSetupProfilesErr
}

// IsBundledSetupProfileKey reports whether key is an embedded setup profile.
func IsBundledSetupProfileKey(key string) bool {
	bundled, err := LoadBundledSetupProfiles()
	if err != nil || bundled == nil {
		return false
	}
	_, ok := bundled[key]
	return ok
}

// BundledSetupProfileKeys returns embedded setup profile keys.
func BundledSetupProfileKeys() map[string]struct{} {
	bundled, err := LoadBundledSetupProfiles()
	if err != nil || bundled == nil {
		return map[string]struct{}{}
	}
	keys := make(map[string]struct{}, len(bundled))
	for k := range bundled {
		keys[k] = struct{}{}
	}
	return keys
}

// GetBundledSetupProfile returns one bundled profile by key.
func GetBundledSetupProfile(key string) (*SetupProfile, bool) {
	bundled, err := LoadBundledSetupProfiles()
	if err != nil {
		return nil, false
	}
	p, ok := bundled[key]
	return p, ok
}
