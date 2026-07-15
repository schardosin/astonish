package baseconfig

import (
	"fmt"
	"strings"

	"github.com/SAP/astonish/pkg/sandbox"
)

// validEngines is the set of allowed browser engine values.
var validEngines = map[string]bool{
	"none":         true,
	"default":      true,
	"cloakbrowser": true,
}

// validArch is the set of allowed architecture values.
var validArch = map[string]bool{
	"amd64": true,
	"arm64": true,
}

// validFingerprintPlatforms is the set of allowed fingerprint OS platforms.
var validFingerprintPlatforms = map[string]bool{
	"linux":   true,
	"macos":   true,
	"windows": true,
}

// Validate checks the BaseConfig for correctness. Returns nil if valid.
func (c *BaseConfig) Validate() error {
	if c.Architecture != "" && !validArch[c.Architecture] {
		return fmt.Errorf("baseconfig: invalid architecture %q (expected amd64 or arm64)", c.Architecture)
	}

	if c.Browser.Engine != "" && !validEngines[c.Browser.Engine] {
		return fmt.Errorf("baseconfig: invalid browser engine %q (expected none, default, or cloakbrowser)", c.Browser.Engine)
	}

	if c.Browser.Engine == "cloakbrowser" {
		if c.Browser.FingerprintPlatform != "" && !validFingerprintPlatforms[c.Browser.FingerprintPlatform] {
			return fmt.Errorf("baseconfig: invalid fingerprint platform %q (expected linux, macos, or windows)", c.Browser.FingerprintPlatform)
		}
	}

	// Validate optional tool IDs against the live catalog.
	catalog := sandbox.OptionalTools()
	catalogMap := make(map[string]bool, len(catalog))
	for _, t := range catalog {
		catalogMap[t.ID] = true
	}
	for _, id := range c.OptionalTools {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !catalogMap[id] {
			known := make([]string, 0, len(catalog))
			for _, t := range catalog {
				known = append(known, t.ID)
			}
			return fmt.Errorf("baseconfig: unknown optional tool %q (known: %s)", id, strings.Join(known, ", "))
		}
	}

	return nil
}
