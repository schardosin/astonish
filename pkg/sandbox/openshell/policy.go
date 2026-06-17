package openshell

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
func defaultSandboxPolicy() *SandboxPolicySpec {
	return &SandboxPolicySpec{
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
}
