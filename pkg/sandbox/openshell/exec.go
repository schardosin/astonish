package openshell

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"

	"context"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Exec (non-interactive, synchronous)
// ---------------------------------------------------------------------------

// ensureSessionRunning checks if the session's sandbox is evicted (idle timeout)
// and transparently recreates it. Returns the up-to-date session record.
func (b *OpenShellBackend) ensureSessionRunning(ctx context.Context, sessionID, caller string) (*store.SandboxSession, error) {
	rec, err := b.sessions.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: %s(%s): lookup session: %w", caller, sessionID, err)
	}
	if rec == nil {
		return nil, fmt.Errorf("sandbox/openshell: %s: session %q has no sandbox", caller, sessionID)
	}

	// Auto-resume evicted sessions (idle timeout recycled the pod).
	if rec.State == store.SandboxSessionStateEvicted {
		slog.Info("auto-resuming evicted sandbox", "component", "openshell",
			"session", sessionID[:min(8, len(sessionID))])
		if err := b.StartSession(ctx, sessionID); err != nil {
			return nil, fmt.Errorf("sandbox/openshell: %s(%s): auto-resume failed: %w", caller, sessionID, err)
		}
		// Re-fetch with updated PodName.
		rec, err = b.sessions.GetSession(sessionID)
		if err != nil {
			return nil, fmt.Errorf("sandbox/openshell: %s(%s): post-resume lookup: %w", caller, sessionID, err)
		}
	}

	if rec == nil || rec.PodName == "" {
		return nil, fmt.Errorf("sandbox/openshell: %s: session %q has no sandbox", caller, sessionID)
	}
	return rec, nil
}

// Exec runs a command in the session's sandbox and returns the result.
// The command is routed through the OpenShell Gateway's exec API which
// enforces the per-sandbox Landlock/seccomp policy.
func (b *OpenShellBackend) Exec(ctx context.Context, sessionID string, opts sandbox.ExecSpec) (*sandbox.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("sandbox/openshell: Exec: Command is required")
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}

	rec, err := b.ensureSessionRunning(ctx, sessionID, "Exec")
	if err != nil {
		return nil, err
	}

	command := wrapCommand(opts.Command, opts.WorkDir, opts.Env)

	resp, err := b.gateway.ExecCommand(ctx, rec.PodName, ExecRequest{
		Command: command,
		Stdin:   opts.Stdin,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: Exec(%s): %w", sessionID, err)
	}

	b.sessions.TouchActivity(sessionID)

	return &sandbox.ExecResult{
		ExitCode: resp.ExitCode,
		Stdout:   resp.Stdout,
		Stderr:   resp.Stderr,
	}, nil
}

// ---------------------------------------------------------------------------
// ExecInteractive (PTY)
// ---------------------------------------------------------------------------

// ExecInteractive starts a PTY-attached process inside the session's
// sandbox. Returns a sandbox.ExecStream the caller wires to their local
// terminal.
func (b *OpenShellBackend) ExecInteractive(ctx context.Context, sessionID string, opts sandbox.PTYSpec) (sandbox.ExecStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("sandbox/openshell: ExecInteractive: Command is required")
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}

	rec, err := b.ensureSessionRunning(ctx, sessionID, "ExecInteractive")
	if err != nil {
		return nil, err
	}

	command := wrapCommand(opts.Command, opts.WorkDir, opts.Env)

	rows, cols := opts.Rows, opts.Cols
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}

	conn, err := b.gateway.ExecStream(ctx, rec.PodName, ExecRequest{
		Command: command,
		TTY:     true,
		Rows:    rows,
		Cols:    cols,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: ExecInteractive(%s): %w", sessionID, err)
	}

	stream := &gatewayExecStream{
		conn:           conn,
		separateStderr: opts.SeparateStderr,
		done:           make(chan struct{}),
	}

	// If SeparateStderr is requested, we'd need the gateway to
	// separate streams. For now, PTY mode merges stdout+stderr
	// (standard terminal behaviour), so SeparateStderr is a no-op.

	b.sessions.TouchActivity(sessionID)

	return stream, nil
}

// ---------------------------------------------------------------------------
// ExecStreaming (non-interactive, long-running bidirectional)
// ---------------------------------------------------------------------------

// ExecStreaming starts a non-interactive streaming process (no PTY) inside
// the session's sandbox. Used for machine-to-machine protocols (MCP JSON-RPC,
// NDJSON) that need a long-running bidirectional stdin/stdout stream.
func (b *OpenShellBackend) ExecStreaming(ctx context.Context, sessionID string, opts sandbox.ExecStreamSpec) (sandbox.ExecStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("sandbox/openshell: ExecStreaming: Command is required")
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}

	rec, err := b.ensureSessionRunning(ctx, sessionID, "ExecStreaming")
	if err != nil {
		return nil, err
	}

	command := wrapCommandRaw(opts.Command, opts.WorkDir, opts.Env)

	// Pass PTY-suppression env vars at the gRPC level so the gateway sets
	// them in the process environment before the shell even starts. This
	// closes the race window between PTY allocation and shell execution.
	// The shell's own "export" (inside wrapCommandRaw) is belt-and-suspenders.
	conn, err := b.gateway.ExecStream(ctx, rec.PodName, ExecRequest{
		Command: command,
		Env: map[string]string{
			"TERM":     "dumb",
			"CI":       "1",
			"NO_COLOR": "1",
		},
		TTY:            false,
		SeparateStderr: opts.SeparateStderr,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: ExecStreaming(%s): %w", sessionID, err)
	}

	stream := &gatewayExecStream{
		conn:           conn,
		separateStderr: opts.SeparateStderr,
		done:           make(chan struct{}),
	}

	b.sessions.TouchActivity(sessionID)

	return stream, nil
}

// ---------------------------------------------------------------------------
// PushFile
// ---------------------------------------------------------------------------

// PushFile writes content to the specified path inside the session's
// sandbox. Implementation: builds a single-file tar archive, pipes it
// to `tar -C <dir> -xmpf -` inside the container via gateway exec.
func (b *OpenShellBackend) PushFile(ctx context.Context, sessionID, filePath string, content io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if filePath == "" {
		return errors.New("sandbox/openshell: PushFile: path is required")
	}
	if !path.IsAbs(filePath) {
		return fmt.Errorf("sandbox/openshell: PushFile: path %q must be absolute", filePath)
	}
	if content == nil {
		return errors.New("sandbox/openshell: PushFile: content is required")
	}
	if b.gateway == nil {
		return ErrNotImplementedYet
	}

	rec, err := b.ensureSessionRunning(ctx, sessionID, "PushFile")
	if err != nil {
		return err
	}

	dir, name := path.Split(filePath)
	if dir == "" {
		dir = "/"
	}
	if len(dir) > 1 {
		dir = strings.TrimRight(dir, "/")
	}

	body, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("sandbox/openshell: PushFile(%s): read source: %w", sessionID, err)
	}

	archive, err := buildSingleFileTar(name, mode, body)
	if err != nil {
		return fmt.Errorf("sandbox/openshell: PushFile(%s): build tar: %w", sessionID, err)
	}

	// Ensure target directory exists, then extract.
	command := []string{"/bin/sh", "-c", fmt.Sprintf("mkdir -p %s && tar -C %s -xmpf -", shellQuote(dir), shellQuote(dir))}

	resp, err := b.gateway.ExecCommand(ctx, rec.PodName, ExecRequest{
		Command: command,
		Stdin:   bytes.NewReader(archive),
	})
	if err != nil {
		return fmt.Errorf("sandbox/openshell: PushFile(%s): %w", sessionID, err)
	}
	if resp.ExitCode != 0 {
		return fmt.Errorf("sandbox/openshell: PushFile(%s): tar exit %d: %s", sessionID, resp.ExitCode, string(resp.Stderr))
	}

	b.sessions.TouchActivity(sessionID)

	return nil
}

// ---------------------------------------------------------------------------
// PullFile
// ---------------------------------------------------------------------------

// PullFile reads a file from the session's sandbox as an io.ReadCloser.
// Implementation: exec `tar -C <dir> -cf - <basename>`, stream the tar
// over gateway exec stdout, extract the single entry in-process.
func (b *OpenShellBackend) PullFile(ctx context.Context, sessionID, filePath string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if filePath == "" {
		return nil, errors.New("sandbox/openshell: PullFile: path is required")
	}
	if !path.IsAbs(filePath) {
		return nil, fmt.Errorf("sandbox/openshell: PullFile: path %q must be absolute", filePath)
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}

	rec, err := b.ensureSessionRunning(ctx, sessionID, "PullFile")
	if err != nil {
		return nil, err
	}

	dir, name := path.Split(filePath)
	if dir == "" {
		dir = "/"
	}
	if len(dir) > 1 {
		dir = strings.TrimRight(dir, "/")
	}

	command := []string{"tar", "-C", dir, "-cf", "-", name}

	resp, err := b.gateway.ExecCommand(ctx, rec.PodName, ExecRequest{
		Command: command,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: PullFile(%s): %w", sessionID, err)
	}
	if resp.ExitCode != 0 {
		return nil, fmt.Errorf("sandbox/openshell: PullFile(%s): tar exit %d: %s", sessionID, resp.ExitCode, string(resp.Stderr))
	}

	// Extract the single file from the tar archive.
	tr := tar.NewReader(bytes.NewReader(resp.Stdout))
	_, err = tr.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("sandbox/openshell: PullFile(%s): file not found in archive", sessionID)
		}
		return nil, fmt.Errorf("sandbox/openshell: PullFile(%s): read tar header: %w", sessionID, err)
	}

	// Read the entire file content. For PullFile via synchronous exec,
	// the data is already fully buffered in resp.Stdout so the tar body
	// cannot exceed what is already in memory.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, tr); err != nil { //nolint:gosec // G110: source is bounded by synchronous exec response, not user-controlled decompression
		return nil, fmt.Errorf("sandbox/openshell: PullFile(%s): extract tar body: %w", sessionID, err)
	}

	b.sessions.TouchActivity(sessionID)

	return io.NopCloser(&buf), nil
}

// ---------------------------------------------------------------------------
// gatewayExecStream: sandbox.ExecStream over ExecStreamConn
// ---------------------------------------------------------------------------

// gatewayExecStream adapts ExecStreamConn to the sandbox.ExecStream interface.
type gatewayExecStream struct {
	conn           ExecStreamConn
	separateStderr io.Writer // non-nil → stderr goes here (not merged)
	done           chan struct{}

	mu       sync.Mutex
	closed   bool
	exitCode int
}

// Read pulls bytes from the remote stdout.
func (s *gatewayExecStream) Read(p []byte) (int, error) {
	return s.conn.Read(p)
}

// Write forwards bytes to the remote stdin.
func (s *gatewayExecStream) Write(p []byte) (int, error) {
	return s.conn.Write(p)
}

// Resize sends a terminal resize event.
func (s *gatewayExecStream) Resize(rows, cols int) error {
	if rows <= 0 || cols <= 0 {
		return fmt.Errorf("sandbox/openshell: Resize: rows and cols must be positive")
	}
	return s.conn.Resize(cols, rows)
}

// Wait blocks until the remote process exits, returns the exit code.
func (s *gatewayExecStream) Wait() (int, error) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode, nil
}

// Close terminates the exec session and releases resources.
func (s *gatewayExecStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	err := s.conn.Close()

	s.mu.Lock()
	s.exitCode = s.conn.ExitCode()
	s.mu.Unlock()

	close(s.done)
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// wrapCommand emits a /bin/sh -c wrapper that applies WorkDir and Env
// before exec'ing the user's command. When WorkDir is empty and Env is
// empty, the user command is returned verbatim.
func wrapCommand(command []string, workDir string, env map[string]string) []string {
	if workDir == "" && len(env) == 0 {
		return command
	}
	var buf bytes.Buffer
	if workDir != "" {
		buf.WriteString("cd ")
		buf.WriteString(shellQuote(workDir))
		buf.WriteString(" && ")
	}
	if len(env) > 0 {
		buf.WriteString("export")
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			buf.WriteByte(' ')
			buf.WriteString(k)
			buf.WriteByte('=')
			buf.WriteString(shellQuote(env[k]))
		}
		buf.WriteString(" && ")
	}
	buf.WriteString("exec")
	for _, a := range command {
		buf.WriteByte(' ')
		buf.WriteString(shellQuote(a))
	}
	return []string{"/bin/sh", "-c", buf.String()}
}

// wrapCommandRaw is like wrapCommand but prepends "stty raw -echo 2>/dev/null;"
// to disable PTY echo. Used by ExecStreaming for machine-to-machine protocols
// (MCP JSON-RPC) where the OpenShell gateway unconditionally allocates a PTY
// on ExecSandboxInteractive regardless of the tty=false flag.
//
// Also injects TERM=dumb (suppresses spinner/ANSI output from npx and friends)
// and NODE_OPTIONS=--no-warnings (suppresses ExperimentalWarning on stdout)
// unless the caller has already set them.
func wrapCommandRaw(command []string, workDir string, env map[string]string) []string {
	// Merge in PTY-safe defaults for non-interactive use.
	mergedEnv := make(map[string]string, len(env)+2)
	for k, v := range env {
		mergedEnv[k] = v
	}
	if _, ok := mergedEnv["TERM"]; !ok {
		mergedEnv["TERM"] = "dumb"
	}
	if _, ok := mergedEnv["NODE_OPTIONS"]; !ok {
		mergedEnv["NODE_OPTIONS"] = "--no-warnings"
	}
	if _, ok := mergedEnv["CI"]; !ok {
		mergedEnv["CI"] = "1"
	}
	if _, ok := mergedEnv["NO_COLOR"]; !ok {
		mergedEnv["NO_COLOR"] = "1"
	}

	var buf bytes.Buffer
	// Disable PTY echo and \n→\r\n translation. The gateway's
	// ExecSandboxInteractive always allocates a PTY; without this,
	// stdin bytes are echoed back on stdout, corrupting JSON-RPC.
	buf.WriteString("stty raw -echo 2>/dev/null; ")
	if workDir != "" {
		buf.WriteString("cd ")
		buf.WriteString(shellQuote(workDir))
		buf.WriteString(" && ")
	}
	buf.WriteString("export")
	keys := make([]string, 0, len(mergedEnv))
	for k := range mergedEnv {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		buf.WriteByte(' ')
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(shellQuote(mergedEnv[k]))
	}
	buf.WriteString(" && exec")
	for _, a := range command {
		buf.WriteByte(' ')
		buf.WriteString(shellQuote(a))
	}
	return []string{"/bin/sh", "-c", buf.String()}
}

// shellQuote wraps s in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	var b bytes.Buffer
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			b.WriteString(`'"'"'`)
			continue
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('\'')
	return b.String()
}

// sortStrings sorts a string slice in place (insertion sort for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// buildSingleFileTar emits a POSIX-format tar archive containing a single
// regular file.
func buildSingleFileTar(name string, mode os.FileMode, body []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:     name,
		Mode:     int64(mode.Perm()),
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := w.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := w.Write(body); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
