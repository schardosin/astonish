package openshell

import "github.com/schardosin/astonish/pkg/config"

// defaultSandboxPolicy returns the SandboxPolicySpec applied to every sandbox
// created by the Astonish backend.
//
// Key design choices:
//   - Landlock compatibility is "best_effort" so the supervisor degrades
//     gracefully on kernels that lack Landlock LSM support.
//   - System paths are read-only (executables, libraries, config).
//   - Workspace, temp, and runtime paths are read-write.
//   - /root is NOT included — the sandboxed process runs as user "sandbox"
//     with home directory at /sandbox.
//   - Network: the supervisor's proxy denies all egress when no
//     NetworkPolicies are set. We ALWAYS populate the policy with resolved
//     presets so that sandboxes have usable internet access.
func defaultSandboxPolicy(netCfg config.NetworkPolicyConfig) *SandboxPolicySpec {
	policy := &SandboxPolicySpec{
		Version: 1,
		Landlock: &LandlockSpec{
			Compatibility: "best_effort",
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
			},
			ReadWrite: []string{
				"/sandbox",
				"/tmp",
				"/var/tmp",
				"/home",
				"/run",
			},
		},
	}

	// Always populate NetworkPolicies — empty = deny-all in OpenShell.
	// ResolvePresets expands the configured presets + extra endpoints into
	// a concrete list of allowed host:port entries.
	endpoints := ResolvePresets(netCfg)
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
