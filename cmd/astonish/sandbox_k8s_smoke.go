// sandbox_k8s_smoke.go — end-to-end smoke probe for the k8s backend.
//
// `astonish sandbox k8s-smoke` exercises every primitive the Kubernetes
// sandbox backend has to get right on a real cluster:
//
//   1. BackendFromAppConfig → k8s.Backend with a real clientset.
//   2. CreateSession using a minimal SessionSpec against the @base layer.
//   3. Exec("echo hi") to verify SPDY transport + overlay workdir.
//   4. PushFile / PullFile roundtrip to verify tar-over-exec streaming.
//   5. StopSession then DestroySession, cleaning up cluster state.
//
// The intent is that a cluster admin can run this once after installing
// the Helm chart (deploy/helm/astonish) and confirm the plumbing end-to-end
// BEFORE wiring any real chat / fleet traffic to the new backend. Any
// failure surfaces as a non-zero exit with a contextual message.
//
// The command is intentionally conservative: it never writes to the
// user's config, never creates long-lived state, and uses a UUID-
// prefixed session ID (astonish-smoke-<ts>) so repeat runs don't
// collide and orphan runs are trivial to identify with
// `kubectl -n astonish-sandbox get pods -l astonish.io/session-id`.
//
// Phase F — overlay-strategy flags. The smoke can override the
// cluster's configured overlay mode / security path without editing
// config.yaml first, which makes it the recommended probe for
// "does my cluster support path X?" questions:
//
//   astonish sandbox k8s-smoke                                     # config.yaml defaults
//   astonish sandbox k8s-smoke --overlay-mode fuse --privileged    # dev / LXC path
//   astonish sandbox k8s-smoke --overlay-mode kernel --host-users=false  # K8s 1.33+ userns
//   astonish sandbox k8s-smoke --overlay-mode fuse \
//     --fuse-device-resource smarter-devices/fuse                  # production device plugin
//
// See docs/deployment/kubernetes.md for the full path matrix.

package astonish

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
)

// handleSandboxK8sSmoke is the entry point for `astonish sandbox k8s-smoke`.
// It takes over stdout with step-by-step progress so operators can spot
// which stage failed without spelunking logs.
func handleSandboxK8sSmoke(args []string) error {
	fs := flag.NewFlagSet("sandbox k8s-smoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we format our own errors
	templateID := fs.String("template", "@base", "layer chain anchor (template ID)")
	sessionPrefix := fs.String("name", "astonish-smoke", "session-id prefix")
	timeout := fs.Duration("timeout", 2*time.Minute, "overall smoke-test timeout")
	keep := fs.Bool("keep", false, "skip Stop/Destroy so the pod stays running for manual inspection")

	// Phase F — overlay strategy overrides. These are optional; when
	// empty/unset the smoke falls through to whatever the operator's
	// config.yaml says.
	overlayMode := fs.String("overlay-mode", "",
		`override sandbox.kubernetes.overlay_mode ("fuse"|"kernel"|"auto"; default: config)`)
	privileged := fs.Bool("privileged", false,
		"force sandbox.kubernetes.privileged_pods=true (dev / LXC path)")
	fuseDeviceResource := fs.String("fuse-device-resource", "",
		"override sandbox.kubernetes.fuse_device_resource (e.g. smarter-devices/fuse)")
	hostUsersFlag := fs.String("host-users", "",
		`override sandbox.kubernetes.host_users: "true" / "false" (default: leave config value)`)
	runtimeClass := fs.String("runtime-class", "",
		"override sandbox.kubernetes.runtime_class_name (e.g. sysbox-runc)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	// Force the k8s backend even if the operator's config.yaml still
	// has sandbox.backend unset — a zero-config cluster admin should be
	// able to run this right after installing the Helm chart.
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("sandbox k8s-smoke: load config: %w", err)
	}
	if appCfg.Sandbox.Backend == "" {
		appCfg.Sandbox.Backend = "k8s"
	} else if appCfg.Sandbox.BackendKind() != "k8s" {
		return fmt.Errorf(
			"sandbox k8s-smoke: config.yaml has sandbox.backend=%q; "+
				"this command only runs against the k8s backend",
			appCfg.Sandbox.Backend,
		)
	}

	// Phase F — apply optional CLI overrides on top of the config.
	if *overlayMode != "" {
		appCfg.Sandbox.Kubernetes.OverlayMode = *overlayMode
	}
	if *privileged {
		appCfg.Sandbox.Kubernetes.PrivilegedPods = true
	}
	if *fuseDeviceResource != "" {
		appCfg.Sandbox.Kubernetes.FuseDeviceResource = *fuseDeviceResource
	}
	if *runtimeClass != "" {
		appCfg.Sandbox.Kubernetes.RuntimeClassName = *runtimeClass
	}
	switch strings.ToLower(*hostUsersFlag) {
	case "":
		// leave config value
	case "true", "1", "yes":
		tv := true
		appCfg.Sandbox.Kubernetes.HostUsers = &tv
	case "false", "0", "no":
		fv := false
		appCfg.Sandbox.Kubernetes.HostUsers = &fv
	default:
		return fmt.Errorf("sandbox k8s-smoke: --host-users=%q (want true/false)", *hostUsersFlag)
	}

	backend, cleanup, err := sandbox.BackendFromAppConfig(appCfg)
	if err != nil {
		return fmt.Errorf("sandbox k8s-smoke: build backend: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	sessionID := fmt.Sprintf("%s-%d", *sessionPrefix, time.Now().UnixNano())
	spec := sandbox.SessionSpec{
		SessionID:  sessionID,
		Type:       sandbox.SessionTypeChat,
		TemplateID: *templateID,
		// Minimal layer chain: just the anchor. Real callers resolve
		// this via store.SandboxTemplateStore.Resolve; the smoke test
		// deliberately skips that to isolate cluster-side failures
		// from template-store bugs.
		LayerChain: []string{*templateID},
		Limits: sandbox.ResourceLimits{
			CPUs:      1,
			MemoryMiB: 256,
			TimeoutS:  int64(timeout.Seconds()),
		},
	}

	fmt.Printf("sandbox k8s-smoke: session id = %s\n", sessionID)
	fmt.Printf("sandbox k8s-smoke: template   = %s\n", *templateID)
	// Echo the chosen Phase F strategy so the operator can see at a
	// glance which path the smoke is probing.
	kc := appCfg.Sandbox.Kubernetes
	hostUsers := "<unset>"
	if kc.HostUsers != nil {
		hostUsers = fmt.Sprintf("%v", *kc.HostUsers)
	}
	mode := kc.OverlayMode
	if mode == "" {
		mode = "fuse (default)"
	}
	fmt.Printf("sandbox k8s-smoke: overlay    = mode=%s privileged=%v host_users=%s fuse_device=%q runtime_class=%q\n",
		mode, kc.PrivilegedPods, hostUsers, kc.FuseDeviceResource, kc.RuntimeClassName)

	// --- Step 1: CreateSession ----------------------------------------
	step := func(label string, fn func() error) error {
		fmt.Printf("  [%s] ...", label)
		start := time.Now()
		err := fn()
		dur := time.Since(start).Round(time.Millisecond)
		if err != nil {
			fmt.Printf(" FAIL (%s): %v\n", dur, err)
			return fmt.Errorf("%s: %w", label, err)
		}
		fmt.Printf(" ok (%s)\n", dur)
		return nil
	}

	if err := step("CreateSession", func() error {
		_, err := backend.CreateSession(ctx, spec)
		return err
	}); err != nil {
		// CreateSession failed → nothing to clean up.
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	// From here on, we own a session in the cluster. Always attempt
	// cleanup on exit unless --keep is set. Cleanup uses a fresh
	// context with its own small timeout so a hung DestroySession
	// doesn't mask the original error.
	cleanupSession := func() {
		if *keep {
			fmt.Printf("  [cleanup] skipped (--keep); destroy manually with:\n")
			fmt.Printf("            astonish sandbox destroy %s\n", sessionID)
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if err := backend.StopSession(cctx, sessionID); err != nil {
			fmt.Printf("  [cleanup] StopSession: %v\n", err)
		}
		if err := backend.DestroySession(cctx, sessionID); err != nil {
			fmt.Printf("  [cleanup] DestroySession: %v\n", err)
		} else {
			fmt.Printf("  [cleanup] destroyed\n")
		}
	}
	defer cleanupSession()

	// --- Step 2: Exec -------------------------------------------------
	var execOut *sandbox.ExecResult
	if err := step("Exec echo", func() error {
		var xerr error
		execOut, xerr = backend.Exec(ctx, sessionID, sandbox.ExecSpec{
			Command: []string{"sh", "-c", "echo hi"},
		})
		if xerr != nil {
			return xerr
		}
		if execOut.ExitCode != 0 {
			return fmt.Errorf("exit=%d stderr=%q", execOut.ExitCode, execOut.Stderr)
		}
		if !strings.Contains(string(execOut.Stdout), "hi") {
			return fmt.Errorf("stdout = %q, want contains \"hi\"", execOut.Stdout)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	// --- Step 3: PushFile / PullFile roundtrip ------------------------
	const probePath = "/tmp/astonish-smoke-probe.txt"
	probeBody := []byte("smoke-" + sessionID)

	if err := step("PushFile", func() error {
		return backend.PushFile(ctx, sessionID, probePath, bytes.NewReader(probeBody), 0o644)
	}); err != nil {
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	if err := step("PullFile", func() error {
		r, err := backend.PullFile(ctx, sessionID, probePath)
		if err != nil {
			return err
		}
		defer r.Close()
		got, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if !bytes.Equal(got, probeBody) {
			return fmt.Errorf("body mismatch: got %q, want %q", got, probeBody)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	// --- Step 4: SessionState sanity check ----------------------------
	if err := step("SessionState", func() error {
		st, err := backend.SessionState(ctx, sessionID)
		if err != nil {
			return err
		}
		if st != sandbox.SessionStateRunning {
			return fmt.Errorf("state = %s, want running", st)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	fmt.Println("sandbox k8s-smoke: all steps passed")
	return nil
}
