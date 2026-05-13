package incus

// ConfigProvider supplies container security/privilege configuration to the
// incus backend. The top-level pkg/sandbox package owns user-facing config
// (SandboxConfig) and registers an implementation via SetConfigProvider
// during SetSandboxConfig. This inversion avoids pkg/sandbox/incus needing
// to import pkg/sandbox (which would be a cycle).
type ConfigProvider interface {
	// IsPrivileged returns whether containers should run in privileged mode.
	IsPrivileged() bool
}

// configProvider is the registered provider. nil means defaults apply.
var configProvider ConfigProvider

// SetConfigProvider registers the ConfigProvider implementation. Called once
// by pkg/sandbox during sandbox configuration.
func SetConfigProvider(p ConfigProvider) {
	configProvider = p
}

// IsPrivileged reports whether containers should run privileged. When no
// provider has been registered, defaults to false (unprivileged).
func IsPrivileged() bool {
	if configProvider == nil {
		return false
	}
	return configProvider.IsPrivileged()
}

// containerSecurityConfig returns the security-related config keys for a container
// based on the current privilege mode and platform.
//
// On native Linux (unprivileged), containers get full hardening:
//   - Syscall intercepts for mknod/setxattr (needed for Docker images)
//   - Default syscall deny list (blocks dangerous syscalls like kexec, module loading)
//   - Compat syscall deny (blocks 32-bit syscall attacks on x86_64)
//   - Guest API disabled (removes /dev/incus from container)
//
// On Docker+Incus (macOS/Windows), syscall hardening is skipped because:
//   - The Docker Desktop VM is the security boundary, not LXC
//   - Seccomp intercepts may not work in nested/emulated environments
//     (e.g., deny_compat fails on aarch64 with "Unsupported architecture")
//   - Containers are still unprivileged (user namespaces active) unless
//     the user explicitly sets sandbox.privileged: true
//
// Note: security.idmap.isolated is intentionally NOT set. All containers must
// share the same idmap range so that overlay lower layers (shared template
// snapshots with pre-shifted UIDs) have correct ownership for all containers.
func containerSecurityConfig() map[string]string {
	if IsPrivileged() {
		return map[string]string{
			"security.privileged": "true",
		}
	}

	// On Docker+Incus, skip syscall hardening — the Docker VM provides
	// isolation and seccomp features may not work in nested environments.
	if activePlatform == PlatformDockerIncus {
		return map[string]string{
			"security.privileged": "false",
		}
	}

	// Native Linux: full hardening
	return map[string]string{
		"security.privileged":                  "false",
		"security.syscalls.intercept.mknod":    "true",
		"security.syscalls.intercept.setxattr": "true",
		"security.syscalls.deny_default":       "true",
		"security.syscalls.deny_compat":        "true",
		"security.guestapi":                    "false",
	}
}

// ContainerSecurityConfig is the exported name for external callers that
// need the computed security map (mostly tests and diagnostics).
func ContainerSecurityConfig() map[string]string {
	return containerSecurityConfig()
}
