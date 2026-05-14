// sandbox_k8s_smoke.go — Phase-D end-to-end smoke probe for the k8s backend.
//
// `astonish sandbox k8s-smoke` exercises every primitive the K8s+Sysbox
// backend has to get right on a real cluster:
//
//   1. BackendFromAppConfig → k8s.Backend with a real clientset.
//   2. CreateSession using a minimal SessionSpec against the @base layer.
//   3. Exec("echo hi") to verify SPDY transport + overlay workdir.
//   4. PushFile / PullFile roundtrip to verify tar-over-exec streaming.
//   5. StopSession then DestroySession, cleaning up cluster state.
//
// The intent is that a cluster admin can run this once after applying
// deploy/k8s/* and confirm the plumbing end-to-end BEFORE wiring any
// real chat / fleet traffic to the new backend. Any failure surfaces
// as a non-zero exit with a contextual message.
//
// The command is intentionally conservative: it never writes to the
// user's config, never creates long-lived state, and uses a UUID-
// prefixed session ID (astonish-smoke-<ts>) so repeat runs don't
// collide and orphan runs are trivial to identify with
// `kubectl -n astonish-sandboxes get pods -l astonish.io/session-id`.
//
// Usage:
//
//   astonish sandbox k8s-smoke                 # uses @base, default limits
//   astonish sandbox k8s-smoke --template foo  # override template ID
//   astonish sandbox k8s-smoke --keep          # leave the pod running for debugging
//
// See docs/architecture/sandbox-backends.md §11 for the Phase-D
// milestone definition.

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
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("sandbox k8s-smoke: %w", err)
	}

	// Force the k8s backend even if the operator's config.yaml still
	// has sandbox.backend unset — a zero-config cluster admin should be
	// able to run this right after applying deploy/k8s/*.
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
