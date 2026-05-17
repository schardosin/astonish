// Package sandbox — internal aliases for the pkg/sandbox/incus subpackage.
//
// This file is NOT a public API shim. After Phase B.5, every external
// caller imports pkg/sandbox/incus directly for incus-specific symbols.
// The aliases below exist purely to keep pkg/sandbox's own orchestration
// code (lifecycle, template, registry, node, setup) terse — writing
// incus.IncusClient, incus.SessionContainerName, incus.ExecInteractive,
// etc. in every line of every file in this package would be noise without
// value. Instead we alias the ~70 most frequently-used names once, here.
//
// Rules:
//   - Only aliases for symbols used 1+ times somewhere in pkg/sandbox
//     (excluding this file) earn a slot. If the last in-package reference
//     to an alias goes away, the alias goes with it.
//   - Aliases MUST NOT be imported by code outside pkg/sandbox. External
//     callers import pkg/sandbox/incus directly. `grep -n 'sandbox\.X'`
//     in code outside pkg/sandbox for any X below should return zero
//     results; if it doesn't, the guilty file is the bug, not this alias.
//   - When adding a new incus-backed function to pkg/sandbox, prefer
//     referencing incus.X inline once. Promote to an alias here only
//     after a second or third call site appears.

package sandbox

import (
	"os"
	"os/exec"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox/incus"
)

// ---------------------------------------------------------------------------
// Client + platform
// ---------------------------------------------------------------------------

// IncusClient is the concrete Incus daemon client. Internal code refers
// to it unqualified; external code must spell it incus.IncusClient.
type IncusClient = incus.IncusClient

// Platform enum.
type Platform = incus.Platform

const (
	PlatformLinuxNative = incus.PlatformLinuxNative
	PlatformDockerIncus = incus.PlatformDockerIncus
	PlatformUnsupported = incus.PlatformUnsupported
)

// Platform-detection and active-platform helpers.
func Connect(platform Platform) (*IncusClient, error)   { return incus.Connect(platform) }
func DetectPlatform() Platform                          { return incus.DetectPlatform() }
func DetectPlatformReason() (Platform, string)          { return incus.DetectPlatformReason() }
func SetActivePlatform(p Platform)                      { incus.SetActivePlatform(p) }
func GetActivePlatform() Platform                       { return incus.GetActivePlatform() }

// ---------------------------------------------------------------------------
// Container naming
// ---------------------------------------------------------------------------

const (
	SessionPrefix = incus.SessionPrefix
	FleetPrefix   = incus.FleetPrefix
	SnapshotName  = incus.SnapshotName
	BaseTemplate  = incus.BaseTemplate
)

func TemplateName(name string) string             { return incus.TemplateName(name) }
func SessionContainerName(sessionID string) string { return incus.SessionContainerName(sessionID) }
func FleetContainerName(planKey, agentKey, taskSlug string) string {
	return incus.FleetContainerName(planKey, agentKey, taskSlug)
}
func OrgSessionContainerName(orgSlug, teamSlug, sessionID string) string {
	return incus.OrgSessionContainerName(orgSlug, teamSlug, sessionID)
}
func OrgFleetContainerName(orgSlug, planKey, agentKey, taskSlug string) string {
	return incus.OrgFleetContainerName(orgSlug, planKey, agentKey, taskSlug)
}

// Unexported helpers — pkg/sandbox historically spells these
// lowerCamelCase; the incus subpackage exports them UpperCamelCase.
func sanitizeInstanceName(s string) string       { return incus.SanitizeInstanceName(s) }
func safeShortID(id string, maxLen int) string   { return incus.SafeShortID(id, maxLen) }

// ---------------------------------------------------------------------------
// Exec
// ---------------------------------------------------------------------------

type (
	ExecOpts         = incus.ExecOpts
	ContainerProcess = incus.ContainerProcess
)

func ExecInteractive(client *IncusClient, containerName string, command []string, opts ExecOpts) (*ContainerProcess, error) {
	return incus.ExecInteractive(client, containerName, command, opts)
}
func ExecNonInteractive(client *IncusClient, containerName string, command []string, opts ExecOpts) (*ContainerProcess, error) {
	return incus.ExecNonInteractive(client, containerName, command, opts)
}
func ExecSimpleWithEnv(client *IncusClient, containerName string, command []string, env map[string]string) (string, error) {
	return incus.ExecSimpleWithEnv(client, containerName, command, env)
}

// ---------------------------------------------------------------------------
// Org networking
// ---------------------------------------------------------------------------

func OrgNetworkName(orgSlug string) string          { return incus.OrgNetworkName(orgSlug) }
func OrgProfileName(orgSlug string) string          { return incus.OrgProfileName(orgSlug) }
func EnsureOrgNetwork(client *IncusClient, orgSlug string) error {
	return incus.EnsureOrgNetwork(client, orgSlug)
}
func DeleteOrgNetwork(client *IncusClient, orgSlug string) {
	incus.DeleteOrgNetwork(client, orgSlug)
}

// ---------------------------------------------------------------------------
// Overlay + storage pool
// ---------------------------------------------------------------------------

const (
	OverlayImageAlias = incus.OverlayImageAlias
	OverlayBaseDir    = incus.OverlayBaseDir
)

func OverlaySessionDir(sessionID string) string       { return incus.OverlaySessionDir(sessionID) }
func OverlayUpperDir(containerName string) string     { return incus.OverlayUpperDir(containerName) }
func EnsureOverlayBaseDir() error                     { return incus.EnsureOverlayBaseDir() }
func EnsureOverlayImage(client *IncusClient) error    { return incus.EnsureOverlayImage(client) }
func ShiftTemplateRootfs(client *IncusClient, templateName string) error {
	return incus.ShiftTemplateRootfs(client, templateName)
}
func SetupUnprivilegedOverlay(client *IncusClient, containerName, containerRootfs, lowerDir string) error {
	return incus.SetupUnprivilegedOverlay(client, containerName, containerRootfs, lowerDir)
}

func GetPoolSourcePath(client *IncusClient, poolName string) (string, error) {
	return incus.GetPoolSourcePath(client, poolName)
}
func GetPoolForProfile(client *IncusClient) (string, error) {
	return incus.GetPoolForProfile(client)
}
func SnapshotRootfsPath(poolSourcePath, templateName string) string {
	return incus.SnapshotRootfsPath(poolSourcePath, templateName)
}
func ContainerRootfsPath(poolSourcePath, containerName string) string {
	return incus.ContainerRootfsPath(poolSourcePath, containerName)
}

func ResolveLowerLayers(poolPath, templateName string, registry *TemplateRegistry) (string, error) {
	return incus.ResolveLowerLayers(poolPath, templateName, registry)
}

func CreateOverlayContainer(client *IncusClient, containerName, templateName string, registry *TemplateRegistry, limits *config.SandboxLimits) error {
	return incus.CreateOverlayContainer(client, containerName, templateName, registry, limits)
}
func CreateOverlayContainerWithProfiles(client *IncusClient, containerName, templateName string, registry *TemplateRegistry, limits *config.SandboxLimits, profiles []string) error {
	return incus.CreateOverlayContainerWithProfiles(client, containerName, templateName, registry, limits, profiles)
}
func UnmountSessionOverlay(poolSourcePath, containerName string) error {
	return incus.UnmountSessionOverlay(poolSourcePath, containerName)
}
func IsOverlayMounted(poolSourcePath, containerName string) bool {
	return incus.IsOverlayMounted(poolSourcePath, containerName)
}
func RemountDependentOverlays(client *IncusClient, snapshotPath string) error {
	return incus.RemountDependentOverlays(client, snapshotPath)
}

// ---------------------------------------------------------------------------
// Docker / host-exec helpers
// ---------------------------------------------------------------------------

func IsIncusDockerContainerRunning() bool   { return incus.IsIncusDockerContainerRunning() }
func GetDockerContainerVersion() string     { return incus.GetDockerContainerVersion() }
func NeedsUpgrade() bool                    { return incus.NeedsUpgrade() }
func EnsureIncusDockerContainer() error     { return incus.EnsureIncusDockerContainer() }
func ExecInDockerHost(args []string) ([]byte, error) {
	return incus.ExecInDockerHost(args)
}
func ExecInDockerHostInteractive(args []string) *exec.Cmd {
	return incus.ExecInDockerHostInteractive(args)
}

// Unexported sandbox-host op wrappers (preserve historical spelling).
func statOnSandboxHost(path string) error           { return incus.StatOnSandboxHost(path) }
func mkdirAllOnSandboxHost(path string, perm os.FileMode) error {
	return incus.MkdirAllOnSandboxHost(path, perm)
}
func removeAllOnSandboxHost(path string) error      { return incus.RemoveAllOnSandboxHost(path) }
func readFileOnSandboxHost(path string) ([]byte, error) {
	return incus.ReadFileOnSandboxHost(path)
}
func mountOverlayOnSandboxHost(opts, target string) error {
	return incus.MountOverlayOnSandboxHost(opts, target)
}
func umountOnSandboxHost(target string) error { return incus.UmountOnSandboxHost(target) }
func rsyncOnSandboxHost(src, dst string) error { return incus.RsyncOnSandboxHost(src, dst) }
func cpOnSandboxHost(src, dst string) error    { return incus.CpOnSandboxHost(src, dst) }

// ---------------------------------------------------------------------------
// Browser-container install helpers (used by pkg/sandbox/template.go when
// provisioning browser-capable templates).
// ---------------------------------------------------------------------------

// LinuxDistro identifies the Linux distribution of the container's base image.
type LinuxDistro = incus.LinuxDistro

const (
	// DistroUbuntuNoble is Ubuntu 24.04 LTS (noble). Used by Incus containers.
	DistroUbuntuNoble = incus.DistroUbuntuNoble
	// DistroDebianBookworm is Debian 12 (bookworm). Used by K8s sandbox-base.
	DistroDebianBookworm = incus.DistroDebianBookworm
)

func IsContainerCompatibleEngine(engine string) bool {
	return incus.IsContainerCompatibleEngine(engine)
}
func BrowserContainerInstallCommands(engine, arch string, distro LinuxDistro) [][]string {
	return incus.BrowserContainerInstallCommands(engine, arch, distro)
}
