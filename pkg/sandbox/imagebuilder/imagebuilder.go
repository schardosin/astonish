// Package imagebuilder provides Kaniko-based container image building for
// the OpenShell sandbox backend. It accepts user-authored Dockerfile bodies
// (arbitrary instructions after FROM), spawns Kaniko build Jobs in the
// control-plane namespace, and tracks build progress via pod log streaming.
package imagebuilder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
)

// Config bundles the imagebuilder dependencies.
type Config struct {
	// Client is the Kubernetes clientset for creating Jobs and ConfigMaps.
	Client kubernetes.Interface

	// Namespace where build Jobs run (control-plane namespace).
	Namespace string

	// RegistryURL is the OCI registry prefix for pushing images.
	// Example: "docker.io/schardosin", "ghcr.io/org"
	RegistryURL string

	// SecretName is the K8s Secret (dockerconfigjson) with registry creds.
	SecretName string

	// BuildImage is the Kaniko executor image reference.
	// Default: "gcr.io/kaniko-project/executor:v1.23.2"
	BuildImage string
}

// Builder creates and manages Kaniko image build Jobs.
type Builder struct {
	cfg Config
}

// New creates a new Builder with the given configuration.
func New(cfg Config) *Builder {
	if cfg.BuildImage == "" {
		cfg.BuildImage = "gcr.io/kaniko-project/executor:v1.23.2"
	}
	return &Builder{cfg: cfg}
}

// BuildSpec describes what to build.
type BuildSpec struct {
	// Scope identifies the build target: "base" for platform-wide,
	// or a team slug for team-scoped builds.
	Scope string

	// BaseImage is the original application-delivered image (Layer 1).
	// Always used as the FROM base — never the previously-built image.
	BaseImage string

	// PlatformBody is the platform admin's Dockerfile recipe (Layer 2).
	// Included in all builds. May be empty if the admin hasn't configured one.
	PlatformBody string

	// TeamBody is the team-specific Dockerfile recipe (Layer 3).
	// Empty for platform builds, non-empty for team builds.
	TeamBody string
}

// BuildResult is returned after a build Job is created.
type BuildResult struct {
	// Image is the full destination image reference (registry/name:tag).
	Image string

	// JobName is the K8s Job name for tracking/cleanup.
	JobName string

	// ConfigMapName is the ConfigMap holding the Dockerfile.
	ConfigMapName string
}

// BuildStatus represents the current state of a build Job.
type BuildStatus struct {
	Phase   BuildPhase
	Message string
}

// BuildPhase is the high-level state of a build.
type BuildPhase string

const (
	BuildPhaseRunning   BuildPhase = "running"
	BuildPhaseSucceeded BuildPhase = "succeeded"
	BuildPhaseFailed    BuildPhase = "failed"
	BuildPhaseUnknown   BuildPhase = "unknown"
)

// ImageTag computes the deterministic image reference for a given spec.
func (b *Builder) ImageTag(spec BuildSpec) string {
	combined := CombinedBody(spec.PlatformBody, spec.TeamBody)
	return ImageRef(b.cfg.RegistryURL, spec.Scope, combined)
}

// ImageRef computes the full image reference given a registry URL, scope, and
// combined Dockerfile body. The tag is the first 12 chars of SHA256(body).
func ImageRef(registryURL, scope, combinedBody string) string {
	h := sha256.Sum256([]byte(combinedBody))
	tag := hex.EncodeToString(h[:])[:12]
	return fmt.Sprintf("%s/astonish-sandbox-%s:%s", registryURL, sanitizeDNS(scope), tag)
}

// ContentHash returns the deterministic hash for a combined Dockerfile body.
// Used to detect no-op rebuilds (same content → same hash → same image tag).
func ContentHash(combinedBody string) string {
	h := sha256.Sum256([]byte(combinedBody))
	return hex.EncodeToString(h[:])[:12]
}

// CombinedBody merges platform and team Dockerfile bodies into a single string
// for hashing purposes. The hash determines the image tag.
func CombinedBody(platformBody, teamBody string) string {
	return strings.TrimSpace(platformBody) + "\n---\n" + strings.TrimSpace(teamBody)
}

// JobName returns a deterministic Job name for a build.
func JobName(scope, combinedBody string) string {
	h := sha256.Sum256([]byte(combinedBody))
	hash := hex.EncodeToString(h[:])[:8]
	// K8s names must be <= 63 chars, DNS-safe.
	name := fmt.Sprintf("astonish-build-%s-%s", sanitizeDNS(scope), hash)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// ConfigMapName returns the ConfigMap name for a build's Dockerfile.
func ConfigMapName(scope, combinedBody string) string {
	h := sha256.Sum256([]byte(combinedBody))
	hash := hex.EncodeToString(h[:])[:8]
	name := fmt.Sprintf("astonish-build-df-%s-%s", sanitizeDNS(scope), hash)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// sanitizeDNS converts a scope string to a DNS-safe label component.
func sanitizeDNS(s string) string {
	s = strings.ToLower(s)
	var out strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out.WriteRune(c)
		} else {
			out.WriteRune('-')
		}
	}
	result := out.String()
	result = strings.Trim(result, "-")
	if result == "" {
		result = "x"
	}
	return result
}

// BuildTimeout is the maximum time to wait for a Kaniko build to complete.
// Large builds (e.g. gcloud CLI ~500MB) can take 15-20 min, so we allow 30 min.
const BuildTimeout = 30 * time.Minute

// Validate checks that the Builder is properly configured.
func (b *Builder) Validate() error {
	if b.cfg.Client == nil {
		return fmt.Errorf("imagebuilder: kubernetes client is required")
	}
	if b.cfg.Namespace == "" {
		return fmt.Errorf("imagebuilder: namespace is required")
	}
	if b.cfg.RegistryURL == "" {
		return fmt.Errorf("imagebuilder: registry URL is required")
	}
	if b.cfg.SecretName == "" {
		return fmt.Errorf("imagebuilder: registry secret name is required")
	}
	return nil
}

// IsConfigured returns true if the builder has enough configuration to run builds.
func IsConfigured(registryURL, secretName string) bool {
	return registryURL != "" && secretName != ""
}

// ProgressFunc is called with progress messages during a build.
type ProgressFunc func(ctx context.Context, msg string)
