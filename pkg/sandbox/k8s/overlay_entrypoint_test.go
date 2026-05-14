package k8s

import (
	"strings"
	"testing"
)

// TestEntrypointScript_Defaults locks in the canonical script shape
// emitted when callers pass the zero-value options. This is the script
// that gets baked into astonish-sandbox-base, so regressions in the
// defaults would ship broken images cluster-wide.
func TestEntrypointScript_Defaults(t *testing.T) {
	s := EntrypointScript(EntrypointScriptOptions{})

	checks := []struct {
		name string
		want string
	}{
		{"shebang", "#!/bin/sh"},
		{"set -eu", "\nset -eu\n"},
		{"pipefail probe", "if ( set -o pipefail ) 2>/dev/null; then"},
		{"session-id guard", `: "${ASTONISH_SESSION_ID:?ASTONISH_SESSION_ID must be set}"`},
		{"layer-chain guard", `: "${ASTONISH_LAYER_CHAIN:?ASTONISH_LAYER_CHAIN must be set}"`},
		{"uppers default mount", "UPPERS_DIR='/mnt/astonish-uppers'"},
		{"layers default mount", "LAYERS_DIR='/mnt/astonish-layers'"},
		{"upperdir default", "UPPER_DIR='/var/astonish/upper'"},
		{"workdir default", "WORK_DIR='/var/astonish/work'"},
		{"mountpoint default", "MOUNT_POINT='/sandbox/rootfs'"},
		{"resume tarball path", `RESUME_TAR="$UPPERS_DIR/$ASTONISH_SESSION_ID/upper.tar.zst"`},
		{"resume guard", `if [ -f "$RESUME_TAR" ]; then`},
		{"resume tar cmd", `tar --numeric-owner --xattrs --acls -I zstd -xf "$RESUME_TAR" -C "$UPPER_DIR"`},
		{"awk reversal", `awk -F, -v dir="$LAYERS_DIR"`},
		{"awk body top-first", `for (i = NF; i > 0; i--)`},
		{"mount overlay", "mount -t overlay overlay"},
		{"mount options", `-o "lowerdir=$LOWER,upperdir=$UPPER_DIR,workdir=$WORK_DIR"`},
		{"handoff default", `exec chroot "$MOUNT_POINT" '/usr/local/bin/astonish' 'node'`},
	}
	for _, c := range checks {
		if !strings.Contains(s, c.want) {
			t.Errorf("script missing %s: expected substring %q\n---script---\n%s", c.name, c.want, s)
		}
	}

	// Resume-tar extraction MUST precede the overlay mount, else the
	// upper layer will be restored into an already-mounted overlay and
	// silently land on a different filesystem.
	tarIdx := strings.Index(s, "tar --numeric-owner")
	mountIdx := strings.Index(s, "mount -t overlay")
	if tarIdx < 0 || mountIdx < 0 || tarIdx >= mountIdx {
		t.Fatalf("resume-tar extraction must precede overlay mount; tarIdx=%d mountIdx=%d", tarIdx, mountIdx)
	}

	// Handoff MUST be the last command (chroot replaces PID 1).
	trimmed := strings.TrimRight(s, "\n")
	lastLine := trimmed[strings.LastIndex(trimmed, "\n")+1:]
	if !strings.HasPrefix(lastLine, "exec chroot ") {
		t.Errorf("final line must be exec chroot, got %q", lastLine)
	}
}

// TestEntrypointScript_OptionOverrides exercises every EntrypointScriptOptions
// field independently so breakage in the defaults-vs-overrides plumbing is
// localised by the failing subtest name.
func TestEntrypointScript_OptionOverrides(t *testing.T) {
	cases := []struct {
		name  string
		opts  EntrypointScriptOptions
		wants []string
		avoid []string
	}{
		{
			name:  "uppers mount override",
			opts:  EntrypointScriptOptions{UppersMount: "/custom/uppers"},
			wants: []string{"UPPERS_DIR='/custom/uppers'"},
			avoid: []string{"UPPERS_DIR='/mnt/astonish-uppers'"},
		},
		{
			name:  "layers mount override",
			opts:  EntrypointScriptOptions{LayersMount: "/custom/layers"},
			wants: []string{"LAYERS_DIR='/custom/layers'"},
			avoid: []string{"LAYERS_DIR='/mnt/astonish-layers'"},
		},
		{
			name:  "upperdir override",
			opts:  EntrypointScriptOptions{UpperDir: "/tmp/up"},
			wants: []string{"UPPER_DIR='/tmp/up'"},
			avoid: []string{"UPPER_DIR='/var/astonish/upper'"},
		},
		{
			name:  "workdir override",
			opts:  EntrypointScriptOptions{WorkDir: "/tmp/wk"},
			wants: []string{"WORK_DIR='/tmp/wk'"},
			avoid: []string{"WORK_DIR='/var/astonish/work'"},
		},
		{
			name:  "mountpoint override",
			opts:  EntrypointScriptOptions{MountPoint: "/mnt/root"},
			wants: []string{"MOUNT_POINT='/mnt/root'"},
			avoid: []string{"MOUNT_POINT='/sandbox/rootfs'"},
		},
		{
			name:  "handoff binary override",
			opts:  EntrypointScriptOptions{Handoff: "/bin/busybox"},
			wants: []string{`exec chroot "$MOUNT_POINT" '/bin/busybox' 'node'`},
			avoid: []string{`'/usr/local/bin/astonish'`},
		},
		{
			name: "handoff args override — multi-arg",
			opts: EntrypointScriptOptions{HandoffArgs: []string{"node", "--flag", "value"}},
			wants: []string{
				`exec chroot "$MOUNT_POINT" '/usr/local/bin/astonish' 'node' '--flag' 'value'`,
			},
		},
		{
			name: "handoff args override — args requiring quoting",
			opts: EntrypointScriptOptions{HandoffArgs: []string{"it's fine"}},
			wants: []string{
				`'it'"'"'s fine'`,
			},
		},
		{
			name: "all overrides combined",
			opts: EntrypointScriptOptions{
				UppersMount: "/u",
				LayersMount: "/l",
				UpperDir:    "/ud",
				WorkDir:     "/wd",
				MountPoint:  "/mp",
				Handoff:     "/h",
				HandoffArgs: []string{"x", "y"},
			},
			wants: []string{
				"UPPERS_DIR='/u'",
				"LAYERS_DIR='/l'",
				"UPPER_DIR='/ud'",
				"WORK_DIR='/wd'",
				"MOUNT_POINT='/mp'",
				`exec chroot "$MOUNT_POINT" '/h' 'x' 'y'`,
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := EntrypointScript(tc.opts)
			for _, w := range tc.wants {
				if !strings.Contains(s, w) {
					t.Errorf("expected substring %q in script\n---script---\n%s", w, s)
				}
			}
			for _, a := range tc.avoid {
				if strings.Contains(s, a) {
					t.Errorf("unexpected substring %q in script (default not overridden)", a)
				}
			}
		})
	}
}

// TestEntrypointScript_ApplyDefaults verifies the defaulting layer in
// isolation so tests of EntrypointScript don't have to re-check every
// sentinel for every subtest.
func TestEntrypointScript_ApplyDefaults(t *testing.T) {
	var o EntrypointScriptOptions
	o.applyDefaults()
	if o.UppersMount != mountUppers {
		t.Errorf("UppersMount default = %q, want %q", o.UppersMount, mountUppers)
	}
	if o.LayersMount != mountLayers {
		t.Errorf("LayersMount default = %q, want %q", o.LayersMount, mountLayers)
	}
	if o.UpperDir != mountUpper {
		t.Errorf("UpperDir default = %q, want %q", o.UpperDir, mountUpper)
	}
	if o.WorkDir != mountWork {
		t.Errorf("WorkDir default = %q, want %q", o.WorkDir, mountWork)
	}
	if o.MountPoint != "/sandbox/rootfs" {
		t.Errorf("MountPoint default = %q, want /sandbox/rootfs", o.MountPoint)
	}
	if o.Handoff != "/usr/local/bin/astonish" {
		t.Errorf("Handoff default = %q, want /usr/local/bin/astonish", o.Handoff)
	}
	if len(o.HandoffArgs) != 1 || o.HandoffArgs[0] != "node" {
		t.Errorf("HandoffArgs default = %v, want [node]", o.HandoffArgs)
	}
	if o.HostBinaryPath != "/usr/local/bin/astonish-host" {
		t.Errorf("HostBinaryPath default = %q, want /usr/local/bin/astonish-host", o.HostBinaryPath)
	}

	// Idempotency: re-applying defaults must not duplicate args or
	// mutate already-set values.
	custom := EntrypointScriptOptions{
		UppersMount: "/x",
		HandoffArgs: []string{"a"},
	}
	custom.applyDefaults()
	custom.applyDefaults()
	if custom.UppersMount != "/x" {
		t.Errorf("applyDefaults mutated explicit UppersMount: %q", custom.UppersMount)
	}
	if len(custom.HandoffArgs) != 1 || custom.HandoffArgs[0] != "a" {
		t.Errorf("applyDefaults mutated explicit HandoffArgs: %v", custom.HandoffArgs)
	}
}

// TestEntrypointScript_QuotingInjection guards against shell injection
// via option values: every path field flows through shellQuote, which
// uses the POSIX single-quote dance ('…'"'"'…') to escape embedded
// single quotes. A regression here would enable a malicious caller to
// break out of the single-quoted assignment and execute arbitrary
// commands at PID 1 inside the sandbox pod.
func TestEntrypointScript_QuotingInjection(t *testing.T) {
	s := EntrypointScript(EntrypointScriptOptions{
		UppersMount: "/tricky'path",
		Handoff:     "/bin/it's",
	})
	if !strings.Contains(s, `UPPERS_DIR='/tricky'"'"'path'`) {
		t.Errorf("UppersMount single-quote not escaped; got:\n%s", s)
	}
	if !strings.Contains(s, `'/bin/it'"'"'s'`) {
		t.Errorf("Handoff single-quote not escaped; got:\n%s", s)
	}
	// An injection attempt must not materialise as bare shell.
	if strings.Contains(s, "UPPERS_DIR=/tricky'path") {
		t.Errorf("single-quote escaped incorrectly, producing injectable content:\n%s", s)
	}
}

// TestEntrypointScript_HostBinaryBindMount exercises the Phase E
// addition: bind-mounting a base-image astonish binary over the
// overlay's /usr/local/bin/astonish. Three behaviours are pinned:
//
//  1. Default path: HostBinaryPath defaults to /usr/local/bin/astonish-host
//     and the bind-mount block appears between overlay mount and handoff.
//  2. Explicit override: a non-standard HostBinaryPath flows through
//     shellQuote and overrides the default.
//  3. Sentinel "-": bind-mount block is OMITTED entirely, matching
//     pre-Phase-E shape for backward-compat tests.
func TestEntrypointScript_HostBinaryBindMount(t *testing.T) {
	t.Run("default enables bind-mount", func(t *testing.T) {
		s := EntrypointScript(EntrypointScriptOptions{})
		if !strings.Contains(s, "HOST_BIN='/usr/local/bin/astonish-host'") {
			t.Errorf("default HOST_BIN assignment missing; got:\n%s", s)
		}
		if !strings.Contains(s, `mount --bind "$HOST_BIN" "$OVERLAY_BIN"`) {
			t.Errorf("bind-mount command missing; got:\n%s", s)
		}

		// Ordering: bind-mount must come AFTER overlay mount and
		// BEFORE handoff. Otherwise the bind destination wouldn't
		// exist yet (no overlay), or PID 1 would handoff without
		// the trusted binary in place.
		mountIdx := strings.Index(s, "mount -t overlay overlay")
		bindIdx := strings.Index(s, `mount --bind "$HOST_BIN"`)
		handoffIdx := strings.Index(s, "exec chroot ")
		if mountIdx < 0 || bindIdx < 0 || handoffIdx < 0 {
			t.Fatalf("missing markers: overlay=%d bind=%d handoff=%d", mountIdx, bindIdx, handoffIdx)
		}
		if !(mountIdx < bindIdx && bindIdx < handoffIdx) {
			t.Errorf("ordering wrong: overlay=%d bind=%d handoff=%d; want overlay < bind < handoff",
				mountIdx, bindIdx, handoffIdx)
		}
	})

	t.Run("custom host binary path", func(t *testing.T) {
		s := EntrypointScript(EntrypointScriptOptions{
			HostBinaryPath: "/opt/astonish/bin/astonish",
		})
		if !strings.Contains(s, "HOST_BIN='/opt/astonish/bin/astonish'") {
			t.Errorf("custom HOST_BIN missing; got:\n%s", s)
		}
		// Default path must NOT leak when overridden.
		if strings.Contains(s, "HOST_BIN='/usr/local/bin/astonish-host'") {
			t.Errorf("default HOST_BIN leaked despite override; got:\n%s", s)
		}
	})

	t.Run("sentinel suppresses bind-mount", func(t *testing.T) {
		s := EntrypointScript(EntrypointScriptOptions{
			HostBinaryPath: "-",
		})
		if strings.Contains(s, "HOST_BIN=") {
			t.Errorf("HOST_BIN assignment should be absent with sentinel; got:\n%s", s)
		}
		if strings.Contains(s, "mount --bind") {
			t.Errorf("bind-mount should be absent with sentinel; got:\n%s", s)
		}
		// Sanity: the rest of the script still emits correctly.
		if !strings.Contains(s, "mount -t overlay overlay") {
			t.Errorf("overlay mount missing with sentinel; got:\n%s", s)
		}
		if !strings.Contains(s, `exec chroot "$MOUNT_POINT"`) {
			t.Errorf("handoff missing with sentinel; got:\n%s", s)
		}
	})

	t.Run("fallback when host binary missing", func(t *testing.T) {
		// The generated script must tolerate the base-image binary
		// being absent (e.g. a custom sandbox image that doesn't bake
		// astonish in). The else branch logs to stderr and leaves
		// the overlay's binary intact so the handoff still works if
		// @base shipped its own.
		s := EntrypointScript(EntrypointScriptOptions{})
		if !strings.Contains(s, `if [ -x "$HOST_BIN" ]; then`) {
			t.Errorf("missing executability guard; got:\n%s", s)
		}
		if !strings.Contains(s, `"skipping bind-mount`) {
			t.Errorf("missing fallback warning; got:\n%s", s)
		}
	})
}

// TestEntrypointScript_OrderingInvariants pins the section order so
// refactors don't silently reshuffle steps that have causal dependencies
// (resume must precede mount; mount must precede handoff).
func TestEntrypointScript_OrderingInvariants(t *testing.T) {
	s := EntrypointScript(EntrypointScriptOptions{})

	markers := []string{
		"set -eu",
		`: "${ASTONISH_SESSION_ID`,
		`: "${ASTONISH_LAYER_CHAIN`,
		"UPPERS_DIR=",
		"LAYERS_DIR=",
		"RESUME_TAR=",
		"tar --numeric-owner",
		`awk -F, -v dir="$LAYERS_DIR"`,
		"mount -t overlay overlay",
		"exec chroot",
	}
	prev := -1
	for _, m := range markers {
		idx := strings.Index(s, m)
		if idx < 0 {
			t.Fatalf("marker %q missing from script", m)
		}
		if idx <= prev {
			t.Errorf("marker %q appears at index %d, must be after previous marker at %d", m, idx, prev)
		}
		prev = idx
	}
}
