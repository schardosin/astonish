// Package sandbox — Backend contract test for *IncusBackend.
//
// The reusable contract helper lives in backend_contract.go (a regular
// source file) so other backend packages can import it; this file wires
// it up for the Incus implementation.

package sandbox

import "testing"

// TestIncusBackendContract runs the contract suite against *IncusBackend.
// The factory returns a zero-value backend (with empty registries) which is
// sufficient for the non-daemon-touching contract checks above. Methods
// that would otherwise hit the daemon are guarded by the cancelled-context
// short-circuit, so no real Incus is required.
func TestIncusBackendContract(t *testing.T) {
	RunBackendContract(t, func(t *testing.T) (Backend, string) {
		return &IncusBackend{
			sessions:  newTestRegistry(t),
			templates: &TemplateRegistry{},
		}, ""
	})
}
