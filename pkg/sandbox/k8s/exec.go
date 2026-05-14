// Package k8s — SPDY exec helpers (Phase C, skeleton slice).
//
// This file will hold the client-go SPDY exec implementation for both
// Exec (non-interactive) and ExecInteractive (PTY with resize). The
// skeleton slice leaves it empty; the first real slice will add:
//
//   type execStream struct {
//       // wraps remotecommand.Executor.StreamWithContext()
//       // implements sandbox.ExecStream
//   }
//
//   func (b *K8sBackend) execNonInteractive(ctx, spec) (*ExecResult, error)
//   func (b *K8sBackend) execInteractive(ctx, spec) (sandbox.ExecStream, error)
//
// Notes on SPDY/HTTP-2 choice:
//
//   - client-go's current exec API uses SPDY over HTTPS (the
//     /exec subresource); plain HTTP/2 upstream support exists but the
//     matrix of Kubernetes versions we target still requires SPDY. See
//     k8s.io/client-go/tools/remotecommand.NewSPDYExecutor.
//
//   - Resize is done via a separate TerminalSizeQueue channel on the
//     executor. sandbox.ExecStream.Resize translates to pushing a
//     remotecommand.TerminalSize onto this channel.
//
//   - The Wait() contract returns an *int exit code; remotecommand
//     surfaces non-zero exits as an *exec.ExitError-ish wrapper. The
//     exec.go slice will centralise the decoding so other files can
//     depend on the typed error shape.
//
// References:
//   - docs/architecture/sandbox-backends.md §3.2 and §11 Phase C.
//   - sandbox.ExecSpec / ExecStream / PTYSpec in
//     pkg/sandbox/backend.go.

package k8s
