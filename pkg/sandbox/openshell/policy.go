package openshell

import "github.com/schardosin/astonish/pkg/config"

// defaultSandboxPolicy returns the SandboxPolicySpec applied to every sandbox
// created by the Astonish backend.
//
// Key design choices:
//   - Landlock compatibility defaults to "best_effort" so the supervisor
//     degrades gracefully on kernels without Landlock LSM. Configurable via
//     SandboxOpenShellConfig.LandlockCompatibility.
//   - System paths are read-only (executables, libraries, config).
//   - Workspace, temp, and runtime paths are read-write.
//   - /dev/ptmx and /dev/pts are read-write — required for PTY allocation
//     inside sandboxes running on kernel 6.10+ (Landlock ABI v5 restricts
//     LANDLOCK_ACCESS_FS_IOCTL_DEV). The supervisor pre-opens PathFds for
//     these before landlock_restrict_self().
//   - /root is NOT included — the sandboxed process runs as user "sandbox"
//     with home directory at /sandbox.
//   - Network: the supervisor's proxy denies all egress when no
//     NetworkPolicies are set. We ALWAYS populate the policy with resolved
//     presets so that sandboxes have usable internet access.
//   - Operators can extend paths via FilesystemPolicy.ExtraReadOnly/ExtraReadWrite.
func defaultSandboxPolicy(cfg config.SandboxOpenShellConfig) *SandboxPolicySpec {
	// Landlock compatibility mode — default to "best_effort" which degrades
	// gracefully on kernels without Landlock support.
	landlockCompat := cfg.LandlockCompatibility
	if landlockCompat == "" {
		landlockCompat = "best_effort"
	}

	policy := &SandboxPolicySpec{
		Version: 1,
		Landlock: &LandlockSpec{
			Compatibility: landlockCompat,
		},
		Filesystem: &FilesystemSpec{
			IncludeWorkdir: true,
			ReadOnly: []string{
				"/usr",
				"/bin",
				"/sbin",
				"/lib",
				"/lib64",
				"/etc",
				"/opt",
				// Device nodes needed by standard library functions.
				"/dev/null",
				"/dev/urandom",
			},
			ReadWrite: []string{
				"/sandbox",
				"/tmp",
				"/var/tmp",
				"/home",
				"/run",
				// PTY device nodes — required for shell_command's interactive
				// terminal support (password prompts, interactive CLIs).
				// The supervisor pre-opens PathFds for these paths BEFORE
				// calling landlock_restrict_self(), so ioctl on /dev/ptmx
				// is permitted even under Landlock ABI v5 (kernel 6.10+)
				// which restricts LANDLOCK_ACCESS_FS_IOCTL_DEV.
				"/dev/ptmx",
				"/dev/pts",
			},
		},
	}

	// Append operator-supplied extra filesystem paths.
	if len(cfg.FilesystemPolicy.ExtraReadOnly) > 0 {
		policy.Filesystem.ReadOnly = append(policy.Filesystem.ReadOnly, cfg.FilesystemPolicy.ExtraReadOnly...)
	}
	if len(cfg.FilesystemPolicy.ExtraReadWrite) > 0 {
		policy.Filesystem.ReadWrite = append(policy.Filesystem.ReadWrite, cfg.FilesystemPolicy.ExtraReadWrite...)
	}

	// Always populate NetworkPolicies — empty = deny-all in OpenShell.
	// ResolvePresets expands the configured presets + extra endpoints into
	// a concrete list of allowed host:port entries.
	endpoints := ResolvePresets(cfg.NetworkPolicy)
	if len(endpoints) > 0 {
		policy.NetworkPolicies = map[string]*NetworkPolicySpec{
			"egress": {
				Name:      "astonish-egress",
				Endpoints: endpoints,
				Binaries:  []string{"/**"},
			},
		}
	}

	return policy
}
