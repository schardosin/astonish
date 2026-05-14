// Package k8s — sandbox-pod entrypoint script generator.
//
// In the K8s+Sysbox backend, the sandbox container runs a single
// PID-1 entrypoint whose job is:
//
//  1. Streaming the persisted upper layer back onto the local emptyDir
//     when resuming a previously-evicted session
//     (/mnt/astonish-uppers/<session-id>/upper.tar.zst).
//  2. Composing the overlay chain encoded in $ASTONISH_LAYER_CHAIN as
//     lowerdirs (top-most first, per overlayfs syntax), with the
//     emptyDir at /var/astonish/upper as upperdir and /var/astonish/work
//     as workdir, mounted at /sandbox/rootfs.
//  3. Handing off to the workload via chroot into the composed rootfs.
//
// Rationale for an in-main-container entrypoint (rather than an init
// container) is documented in docs/architecture/sandbox-backends.md
// §5.3 step 3: Sysbox grants CAP_SYS_ADMIN inside the user namespace
// so mount(2) works without hostPath / Bidirectional propagation.
//
// This file owns the **source of truth** for the entrypoint script.
// The script is copied into the astonish-sandbox-base image at build
// time (typically `go run ./cmd/astonish-sandbox-entrypoint-script > 
// /tmp/entrypoint.sh && COPY /tmp/entrypoint.sh /usr/local/bin/` in the
// Dockerfile, or an equivalent build step). Keeping it in Go lets us
// unit-test its shape without a CI shell harness and lets Astonish
// evolve the runtime contract (env var names, mount paths, resume
// semantics) in lockstep with the backend that produces those inputs.
//
// Design invariants enforced by the script:
//
//   - `set -euo pipefail` — fail fast; unset variables are errors.
//   - Resume-tar extraction MUST happen before the overlay mount so
//     the emptyDir at /var/astonish/upper is populated first.
//   - Resume-tar extraction is SKIPPED if the tarball is absent (fresh
//     session path). Absence is not an error.
//   - Layer-chain composition reverses the comma-separated
//     $ASTONISH_LAYER_CHAIN so the top-most (last element) becomes the
//     first lowerdir — overlayfs expects the layer closest to upper
//     first. This mirrors buildFleetPodManifest's / buildPodManifest's
//     conventional oldest-first slice order.
//   - The mount is FORMED with `mount -t overlay` (not fuse-overlayfs)
//     since Sysbox enables kernel overlayfs.
//   - The handoff is `exec chroot` so PID 1 semantics transfer to the
//     workload (signal propagation, reaping).
//
// Reference: docs/architecture/sandbox-backends.md §5.3 step 3; §5.14
// for the resume-tar layout.

package k8s

import "strings"

// EntrypointScriptOptions tunes the emitted script for non-default
// deployments. The zero value produces the canonical script used by
// the astonish-sandbox-base image.
//
// Fields are all optional; empty means "use the spec's default".
type EntrypointScriptOptions struct {
	// UppersMount is the in-container mount path of the uppers PVC.
	// Default: /mnt/astonish-uppers (mountUppers).
	UppersMount string

	// LayersMount is the in-container mount path of the layers PVC.
	// Default: /mnt/astonish-layers (mountLayers).
	LayersMount string

	// UpperDir is the emptyDir path that overlayfs uses as its
	// upperdir. Default: /var/astonish/upper (mountUpper).
	UpperDir string

	// WorkDir is the emptyDir path that overlayfs uses as its
	// workdir. Default: /var/astonish/work (mountWork).
	WorkDir string

	// MountPoint is where the composed overlay is mounted. Default:
	// /sandbox/rootfs.
	MountPoint string

	// Handoff is the absolute path of the command to exec after the
	// overlay is in place (PID 1 hands off via `exec chroot`).
	// Default: /usr/local/bin/astonish (the daemon's node binary;
	// §5.3 step 3).
	Handoff string

	// HandoffArgs are the arguments passed to Handoff.
	// Default: []string{"node"}.
	HandoffArgs []string
}

func (o *EntrypointScriptOptions) applyDefaults() {
	if o.UppersMount == "" {
		o.UppersMount = mountUppers
	}
	if o.LayersMount == "" {
		o.LayersMount = mountLayers
	}
	if o.UpperDir == "" {
		o.UpperDir = mountUpper
	}
	if o.WorkDir == "" {
		o.WorkDir = mountWork
	}
	if o.MountPoint == "" {
		o.MountPoint = "/sandbox/rootfs"
	}
	if o.Handoff == "" {
		o.Handoff = "/usr/local/bin/astonish"
	}
	if len(o.HandoffArgs) == 0 {
		o.HandoffArgs = []string{"node"}
	}
}

// EntrypointScript returns the POSIX-shell source of the sandbox-pod
// entrypoint, parameterised by opts. The returned script is
// self-contained (no external files required) and must be written to
// the image at /usr/local/bin/astonish-sandbox-entrypoint with
// executable mode.
//
// Env vars consumed at runtime:
//
//   - ASTONISH_SESSION_ID (required): key used to locate the persisted
//     upper tarball on the uppers PVC when resuming.
//   - ASTONISH_LAYER_CHAIN (required): comma-separated list of layer
//     IDs, oldest → newest. The script reverses this for overlayfs.
//
// Failure modes:
//
//   - Missing env var → script exits non-zero (via `set -u`).
//   - Empty layer chain → script exits non-zero with a clear error
//     message; a session with no layers is a configuration bug.
//   - Mount failure → script exits non-zero; the pod transitions to
//     Failed and the kubelet surfaces the stderr in logs.
func EntrypointScript(opts EntrypointScriptOptions) string {
	opts.applyDefaults()

	var b strings.Builder
	b.WriteString(`#!/bin/sh
# astonish-sandbox-entrypoint — PID-1 overlay composer.
#
# DO NOT EDIT BY HAND. Generated by pkg/sandbox/k8s.EntrypointScript.
# See docs/architecture/sandbox-backends.md §5.3 step 3 for the
# design rationale.

set -eu
# pipefail is not POSIX but is supported by bash/ash/busybox/zsh. In
# dash (Debian /bin/sh), "set -o pipefail" aborts the whole shell when
# pipefail is unknown — even with a trailing "|| true" — so we wrap it
# in a subshell to isolate the failure, then re-apply it to the parent
# shell only if the probe succeeded. Best-effort; absence is tolerated.
if ( set -o pipefail ) 2>/dev/null; then
  set -o pipefail
fi

: "${ASTONISH_SESSION_ID:?ASTONISH_SESSION_ID must be set}"
: "${ASTONISH_LAYER_CHAIN:?ASTONISH_LAYER_CHAIN must be set}"

`)
	// Emit path vars up top so the script reads top-down and the
	// later commands are obvious.
	b.WriteString("UPPERS_DIR=")
	writeSingleQuoted(&b, opts.UppersMount)
	b.WriteByte('\n')
	b.WriteString("LAYERS_DIR=")
	writeSingleQuoted(&b, opts.LayersMount)
	b.WriteByte('\n')
	b.WriteString("UPPER_DIR=")
	writeSingleQuoted(&b, opts.UpperDir)
	b.WriteByte('\n')
	b.WriteString("WORK_DIR=")
	writeSingleQuoted(&b, opts.WorkDir)
	b.WriteByte('\n')
	b.WriteString("MOUNT_POINT=")
	writeSingleQuoted(&b, opts.MountPoint)
	b.WriteString("\n\n")

	// Resume path.
	b.WriteString(`# --- 1. Resume from persisted upper (if present) -----------------------
# When a previously-evicted session is restarted, Astonish persists its
# upper layer to $UPPERS_DIR/<session>/upper.tar.zst. Stream it back
# onto the local emptyDir before the overlay mount.
RESUME_TAR="$UPPERS_DIR/$ASTONISH_SESSION_ID/upper.tar.zst"
if [ -f "$RESUME_TAR" ]; then
  echo "astonish-entrypoint: resuming upper from $RESUME_TAR" 1>&2
  mkdir -p "$UPPER_DIR"
  tar --numeric-owner --xattrs --acls -I zstd -xf "$RESUME_TAR" -C "$UPPER_DIR"
fi

# Always ensure the overlay dirs exist; on fresh sessions they're
# already present (emptyDir mounts) but this is cheap and idempotent.
mkdir -p "$UPPER_DIR" "$WORK_DIR" "$MOUNT_POINT"

# --- 2. Compose overlay from layer chain -------------------------------
# $ASTONISH_LAYER_CHAIN is oldest-first (e.g., @base,org-layer,template).
# Overlayfs wants the top-most layer FIRST in its comma-separated
# lowerdir list, so we reverse. We pass $LAYERS_DIR into awk via -v so
# path substitution happens in awk (not a second eval pass), which
# keeps the script robust against metacharacters in the mount path.
LOWER=$(echo "$ASTONISH_LAYER_CHAIN" | awk -F, -v dir="$LAYERS_DIR" '
  {
    for (i = NF; i > 0; i--) {
      printf "%s/%s/rootfs%s", dir, $i, (i > 1 ? ":" : "")
    }
  }')
if [ -z "$LOWER" ]; then
  echo "astonish-entrypoint: empty ASTONISH_LAYER_CHAIN" 1>&2
  exit 1
fi

echo "astonish-entrypoint: mounting overlay at $MOUNT_POINT" 1>&2
mount -t overlay overlay \
  -o "lowerdir=$LOWER,upperdir=$UPPER_DIR,workdir=$WORK_DIR" \
  "$MOUNT_POINT"

# --- 3. Hand off to workload ------------------------------------------
`)
	b.WriteString("exec chroot \"$MOUNT_POINT\" ")
	b.WriteString(shellQuote(opts.Handoff))
	for _, a := range opts.HandoffArgs {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	b.WriteByte('\n')

	return b.String()
}

// writeSingleQuoted writes shellQuote(s) into b. Introduced so the
// top-of-script path initialisers read naturally; the real quoting is
// delegated to shellQuote in exec.go.
func writeSingleQuoted(b *strings.Builder, s string) {
	b.WriteString(shellQuote(s))
}
