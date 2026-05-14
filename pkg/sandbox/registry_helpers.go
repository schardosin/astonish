package sandbox

import (
	"encoding/json"
	"path/filepath"
)

// legacySessionsJSONPath returns the path to the pre-cutover sessions.json
// file. Kept as a named helper so tests and the importer agree on the path.
func legacySessionsJSONPath(dataDir string) string {
	return filepath.Join(dataDir, "sessions.json")
}

// jsonUnmarshalPreserveEmpty is a thin wrapper around json.Unmarshal that
// treats an empty byte slice as "no entries" rather than an error. Legacy
// sessions.json occasionally existed as a zero-byte file after a buggy
// write path.
func jsonUnmarshalPreserveEmpty(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
