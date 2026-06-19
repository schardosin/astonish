package openshell

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------
//
// In the OpenShell model, "templates" correspond to custom sandbox images
// (the --from flag in `openshell sandbox create`). The OpenShell gateway
// manages sandbox images; Astonish does not build overlay layers itself.
//
// These methods satisfy the sandbox.Backend interface. BuildTemplate and
// SaveSessionAsTemplate are not supported in the OpenShell backend because
// the gateway owns workspace lifecycle through volumeClaimTemplates.
// Use custom sandbox images instead.

// BuildTemplate is not supported in the OpenShell backend.
// Use custom sandbox images (--from) via the OpenShell gateway instead.
func (b *OpenShellBackend) BuildTemplate(_ context.Context, _ sandbox.TemplateBuildSpec) (*sandbox.TemplateArtifact, error) {
	return nil, fmt.Errorf("sandbox/openshell: BuildTemplate is not supported; " +
		"use custom sandbox images via the OpenShell gateway instead")
}

// SaveSessionAsTemplate is not supported in the OpenShell backend.
func (b *OpenShellBackend) SaveSessionAsTemplate(_ context.Context, _ string) (*sandbox.TemplateArtifact, error) {
	return nil, fmt.Errorf("sandbox/openshell: SaveSessionAsTemplate is not supported; " +
		"use custom sandbox images via the OpenShell gateway instead")
}

// RefreshTemplate is not supported in the OpenShell backend.
func (b *OpenShellBackend) RefreshTemplate(_ context.Context, _ string) (*sandbox.TemplateArtifact, error) {
	return nil, fmt.Errorf("sandbox/openshell: RefreshTemplate is not supported; " +
		"use custom sandbox images via the OpenShell gateway instead")
}

// DeleteTemplate is a no-op in the OpenShell backend. Image lifecycle is
// managed externally via container registries.
func (b *OpenShellBackend) DeleteTemplate(_ context.Context, _ string, _ bool) error {
	return nil
}
