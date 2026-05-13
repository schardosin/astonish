package sandbox

import (
	"os"
	"os/exec"

	"github.com/schardosin/astonish/pkg/sandbox/incus"
)

// IncusClient is re-exported from pkg/sandbox/incus. Keeping the historical
// name keeps ~50 external call sites across cmd/, pkg/api, pkg/daemon,
// pkg/launcher, pkg/fleet, and pkg/tools unchanged.
type IncusClient = incus.IncusClient

// Container naming constants re-exported from pkg/sandbox/incus.
const (
	TemplatePrefix = incus.TemplatePrefix
	SessionPrefix  = incus.SessionPrefix
	FleetPrefix    = incus.FleetPrefix
	SnapshotName   = incus.SnapshotName
	BaseTemplate   = incus.BaseTemplate
)

// Connect creates a new IncusClient connected to the Incus daemon.
func Connect(platform Platform) (*IncusClient, error) { return incus.Connect(platform) }

// TemplateName returns the full Incus container name for a template.
func TemplateName(name string) string { return incus.TemplateName(name) }

// SessionContainerName returns the full Incus container name for a session.
func SessionContainerName(sessionID string) string { return incus.SessionContainerName(sessionID) }

// FleetContainerName returns the full Incus container name for a fleet session.
func FleetContainerName(planKey, agentKey, taskSlug string) string {
	return incus.FleetContainerName(planKey, agentKey, taskSlug)
}

// OrgSessionContainerName returns the container name scoped to an org/team.
func OrgSessionContainerName(orgSlug, teamSlug, sessionID string) string {
	return incus.OrgSessionContainerName(orgSlug, teamSlug, sessionID)
}

// OrgFleetContainerName returns the fleet container name scoped to an org.
func OrgFleetContainerName(orgSlug, planKey, agentKey, taskSlug string) string {
	return incus.OrgFleetContainerName(orgSlug, planKey, agentKey, taskSlug)
}

// SnapshotSource returns the snapshot source string for cloning.
func SnapshotSource(templateName string) string { return incus.SnapshotSource(templateName) }

// sanitizeInstanceName is an unexported wrapper preserving the historical
// name used by pkg/sandbox-internal callers (org_network.go, etc).
func sanitizeInstanceName(s string) string { return incus.SanitizeInstanceName(s) }

// safeShortID is an unexported wrapper preserving the historical name used
// by pkg/sandbox-internal callers (lifecycle.go).
func safeShortID(id string, maxLen int) string { return incus.SafeShortID(id, maxLen) }

// Platform is re-exported from pkg/sandbox/incus.
type Platform = incus.Platform

// Platform enum values re-exported so existing callers compile unchanged.
const (
	PlatformLinuxNative = incus.PlatformLinuxNative
	PlatformDockerIncus = incus.PlatformDockerIncus
	PlatformUnsupported = incus.PlatformUnsupported
)

// DetectPlatform determines the host platform and available container runtime.
func DetectPlatform() Platform { return incus.DetectPlatform() }

// DetectPlatformReason determines the host platform and returns a human-readable
// reason if the platform is unsupported.
func DetectPlatformReason() (Platform, string) { return incus.DetectPlatformReason() }

// IsInsideLXC detects whether the current process is running inside an LXC
// container.
func IsInsideLXC() bool { return incus.IsInsideLXC() }

// SetActivePlatform sets the package-level platform used by remote ops.
func SetActivePlatform(p Platform) { incus.SetActivePlatform(p) }

// GetActivePlatform returns the current active platform.
func GetActivePlatform() Platform { return incus.GetActivePlatform() }

// --- remote-ops helper shims (unexported wrappers so existing in-package
// callers keep working without renaming). The incus package exports the
// helpers publicly; these wrappers preserve the historical unexported
// spelling used in the rest of pkg/sandbox.

func execOnSandboxHost(args []string) ([]byte, error)     { return incus.ExecOnSandboxHost(args) }
func statOnSandboxHost(path string) error                 { return incus.StatOnSandboxHost(path) }
func mkdirAllOnSandboxHost(path string, perm os.FileMode) error {
	return incus.MkdirAllOnSandboxHost(path, perm)
}
func removeAllOnSandboxHost(path string) error        { return incus.RemoveAllOnSandboxHost(path) }
func readFileOnSandboxHost(path string) ([]byte, error) { return incus.ReadFileOnSandboxHost(path) }
func readDirOnSandboxHost(path string) ([]string, error) {
	return incus.ReadDirOnSandboxHost(path)
}
func mountOverlayOnSandboxHost(opts, target string) error {
	return incus.MountOverlayOnSandboxHost(opts, target)
}
func umountOnSandboxHost(target string) error      { return incus.UmountOnSandboxHost(target) }
func readMountsOnSandboxHost() ([]byte, error)     { return incus.ReadMountsOnSandboxHost() }
func isOverlayMountedOnSandboxHost(rootfs string) bool {
	return incus.IsOverlayMountedOnSandboxHost(rootfs)
}
func rsyncOnSandboxHost(src, dst string) error { return incus.RsyncOnSandboxHost(src, dst) }
func cpOnSandboxHost(src, dst string) error    { return incus.CpOnSandboxHost(src, dst) }

// --- docker.go re-exports ---
//
// Constants and functions from pkg/sandbox/docker.go are referenced by
// external packages (cmd, pkg/api). Re-export them here to preserve API.

const (
	DockerContainerName  = incus.DockerContainerName
	DockerVolumeName     = incus.DockerVolumeName
	DockerImageRepo      = incus.DockerImageRepo
	DockerVersionLabel   = incus.DockerVersionLabel
	DockerIncusPort      = incus.DockerIncusPort
	DockerClientCertPath = incus.DockerClientCertPath
	DockerClientKeyPath  = incus.DockerClientKeyPath
)

// DockerImageTag returns the full image reference for the current astonish version.
func DockerImageTag() string { return incus.DockerImageTag() }

// IsIncusDockerContainerRunning reports whether the Incus Docker container is running.
func IsIncusDockerContainerRunning() bool { return incus.IsIncusDockerContainerRunning() }

// IsIncusDockerContainerExists reports whether the Incus Docker container exists.
func IsIncusDockerContainerExists() bool { return incus.IsIncusDockerContainerExists() }

// GetDockerContainerVersion returns the astonish version label on the container.
func GetDockerContainerVersion() string { return incus.GetDockerContainerVersion() }

// NeedsUpgrade reports whether the Docker container needs an upgrade.
func NeedsUpgrade() bool { return incus.NeedsUpgrade() }

// EnsureIncusDockerContainer creates or reuses the Incus Docker container.
func EnsureIncusDockerContainer() error { return incus.EnsureIncusDockerContainer() }

// UpgradeIncusDockerContainer recreates the container to match the current version.
func UpgradeIncusDockerContainer() error { return incus.UpgradeIncusDockerContainer() }

// StopIncusDockerContainer stops the Incus Docker container.
func StopIncusDockerContainer() error { return incus.StopIncusDockerContainer() }

// ExecInDockerHost runs a command inside the Docker container hosting Incus.
func ExecInDockerHost(args []string) ([]byte, error) { return incus.ExecInDockerHost(args) }

// ExecInDockerHostInteractive returns an *exec.Cmd wired for interactive
// execution inside the Docker container hosting Incus.
func ExecInDockerHostInteractive(args []string) *exec.Cmd {
	return incus.ExecInDockerHostInteractive(args)
}

// ReadDockerClientCert reads the client certificate and key from the Docker
// container that hosts Incus.
func ReadDockerClientCert() (certPEM, keyPEM string, err error) {
	return incus.ReadDockerClientCert()
}

