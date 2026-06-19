//go:build e2e

// Sandbox backend link-time registrations for the E2E test binary.
//
// The E2E harness configures sandbox.backend: k8s in the test config.
// For the k8s backend factory to be available, pkg/sandbox/k8s must be
// imported (its init() registers the factory with sandbox.NewBackend).
//
// This mirrors cmd/astonish/sandbox_backends.go which does the same for
// the production binary.
package e2eboot

import (
	_ "github.com/schardosin/astonish/pkg/sandbox/k8s"
	_ "github.com/schardosin/astonish/pkg/sandbox/mock"
	_ "github.com/schardosin/astonish/pkg/sandbox/openshell"
)
