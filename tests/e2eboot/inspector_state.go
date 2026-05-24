// Package e2eboot — inspector_state.go is intentionally NOT build-tagged so it
// can be imported by both the e2e test code (which is `+build e2e`) AND by the
// standalone `tools/e2e-inspector` binary which has no build tag.
//
// This file defines the on-disk shape of the inspector state file and the
// fixed configuration that both sides must agree on.
package e2eboot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// InspectorStateFile is the path written by the inspector binary on startup
// and read by the e2e test harness when ASTONISH_E2E_KEEP_ALIVE=1.
const InspectorStateFile = "/tmp/astonish-e2e-inspect.json"

// InspectorSecretFile holds a per-run shared secret used to gate any future
// admin endpoints on the inspector. Currently unused but reserved.
const InspectorSecretFile = "/tmp/astonish-e2e-inspect.secret" //nolint:gosec // not a credential

// InspectorSuffix is the fixed Postgres-suffix used by the inspector.
// Platform DB:  astonish_<suffix>_platform
// Per-test orgs are <orgSlug>-<perTestSuffix> and live in DBs
// astonish_<suffix>_<sanitized-org-slug>.
const InspectorSuffix = "e2einspect"

// InspectorPort is the fixed TCP port the inspector StudioServer binds on.
const InspectorPort = 9394

// InspectorDefaultEmail / InspectorDefaultPassword are the credentials of
// the bootstrap user (org "default", team "general"). The user logs into
// the Studio UI with these.
const (
	InspectorDefaultEmail    = "e2e@test.local"
	InspectorDefaultPassword = "E2ETest2024!"
)

// InspectorJWTSecret is the JWT signing secret used by the inspector. It must
// be the same value used by tests when they mint per-test JWTs (in seed.go),
// otherwise the inspector StudioServer will reject those tokens.
const InspectorJWTSecret = "e2e-test-jwt-secret-that-is-at-least-32-chars-long!!"

// SeededUserPassword is the password used for ALL seeded users
// (Alice/Bob/Carol/Dave/Eve) created by Seed(). Tests still authenticate via
// minted JWTs; this password exists so a developer can log into the
// kept-alive inspector UI as Alice (etc.) and see the world from her
// perspective — including her chat sessions. Defined here (not in
// layout.go) so the standalone tools/e2e-inspector binary can reference it.
const SeededUserPassword = "E2ETestSeed2024!"

// InspectorState is what gets written to InspectorStateFile.
type InspectorState struct {
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	Hostname   string `json:"hostname"`
	BaseURL    string `json:"base_url"`     // http://localhost:9394
	Suffix     string `json:"suffix"`       // e2einspect
	BaseDSN    string `json:"base_dsn"`     // ASTONISH_TEST_DSN at startup
	StartedAt  string `json:"started_at"`   // RFC3339
	UserEmail  string `json:"user_email"`
	UserPasswd string `json:"user_passwd"`
}

// ReadInspectorState reads InspectorStateFile. Returns os.ErrNotExist if the
// file does not exist (i.e. no inspector is running).
func ReadInspectorState() (*InspectorState, error) {
	data, err := os.ReadFile(InspectorStateFile)
	if err != nil {
		return nil, err
	}
	var s InspectorState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", InspectorStateFile, err)
	}
	return &s, nil
}

// WriteInspectorState atomically writes the state to InspectorStateFile.
func WriteInspectorState(s *InspectorState) error {
	tmp := InspectorStateFile + ".tmp"
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, InspectorStateFile)
}

// RetainSessions returns true when the test run is part of a shared-inspect
// session (env ASTONISH_E2E_KEEP_ALIVE=1). When true, hygienic per-test
// session DELETEs should be skipped so the inspector UI can browse the
// data the tests created. Asserted DELETEs (where the DELETE response
// itself is the subject of an assertion, e.g. "verify deletion works" or
// "verify another user cannot delete") MUST still run regardless of this
// flag.
func RetainSessions() bool {
	return os.Getenv("ASTONISH_E2E_KEEP_ALIVE") == "1"
}

// IsInspectorRunning probes whether an inspector process is alive at the
// recorded PID. Returns (state, true) if alive, (state, false) otherwise.
func IsInspectorRunning() (*InspectorState, bool) {
	s, err := ReadInspectorState()
	if err != nil {
		return nil, false
	}
	if s.PID == 0 {
		return s, false
	}
	proc, err := os.FindProcess(s.PID)
	if err != nil {
		return s, false
	}
	// On Unix, signal 0 probes whether the process exists without affecting it.
	if err := proc.Signal(syscallSignal0); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return s, false
		}
		// Many "process not found" errors come back as ESRCH; treat any
		// failure as "not running".
		return s, false
	}
	return s, true
}
