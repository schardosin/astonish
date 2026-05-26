package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/schardosin/astonish/pkg/config"
)

// BackendMCPTransport implements mcp.Transport by running the MCP server
// process inside a sandbox container via the abstract Backend interface.
// This works for both Incus and Kubernetes backends.
//
// The MCP SDK expects a Transport that returns a bidirectional JSON-RPC
// connection. This transport uses Backend.ExecInteractive() to get a stream
// attached to the MCP server process's stdin/stdout inside the container.
type BackendMCPTransport struct {
	backend   Backend
	sessionID string
	command   []string
	env       map[string]string

	// Stderr captures the MCP server's stderr output for diagnostics.
	Stderr *bytes.Buffer

	mu     sync.Mutex
	stream ExecStream
}

// NewBackendMCPTransport creates a transport that will run the given MCP
// server config inside the specified session's sandbox. The transport is
// not started until Connect() is called.
func NewBackendMCPTransport(backend Backend, sessionID string, cfg config.MCPServerConfig) (*BackendMCPTransport, *bytes.Buffer) {
	cmd := append([]string{cfg.Command}, cfg.Args...)
	env := make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		env[k] = v
	}

	var stderr bytes.Buffer
	return &BackendMCPTransport{
		backend:   backend,
		sessionID: sessionID,
		command:   cmd,
		env:       env,
		Stderr:    &stderr,
	}, &stderr
}

// Connect starts the MCP server process inside the container and returns a
// Connection that communicates with it over stdin/stdout. Implements mcp.Transport.
func (t *BackendMCPTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Ensure PATH includes common tool locations (npx, uv, etc.)
	env := make(map[string]string, len(t.env)+1)
	for k, v := range t.env {
		env[k] = v
	}
	if _, ok := env["PATH"]; !ok {
		env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/root/.local/bin"
	}

	// Determine the exec command based on backend kind:
	//
	// K8s: kubectl exec lands in the pod's base namespace (bare Debian),
	//   not PID 1's chroot (/sandbox/rootfs where runtimes like Node.js,
	//   Python etc. are installed). The astonish-shell wrapper does
	//   `exec chroot /sandbox/rootfs "$@"` to enter the composed overlay.
	//
	// Incus: the overlay IS the container's root filesystem — no chroot
	//   needed. Commands execute directly in the correct namespace.
	//   Using astonish-shell here would be WRONG because the fallback
	//   path (when /sandbox/rootfs doesn't exist) discards all arguments
	//   and just launches /bin/bash -l.
	execCmd := t.command
	if t.backend.Kind() == BackendKindK8s {
		execCmd = append([]string{"/usr/local/bin/astonish-shell"}, t.command...)
	}

	stream, err := t.backend.ExecStreaming(ctx, t.sessionID, ExecStreamSpec{
		Command:        execCmd,
		Env:            env,
		SeparateStderr: t.Stderr, // Keep stderr separate from JSON-RPC stdout
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start MCP server in session %q: %w", t.sessionID, err)
	}
	t.stream = stream

	// Wrap the stream's Reader/Writer in an IOTransport.
	// ExecStream implements io.Reader (stdout) and io.Writer (stdin).
	rwc := &backendStreamRWC{stream: stream}

	inner := &mcp.IOTransport{
		Reader: rwc,
		Writer: rwc,
	}
	return inner.Connect(ctx)
}

// Close terminates the MCP server process.
func (t *BackendMCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stream != nil {
		if err := t.stream.Close(); err != nil {
			slog.Warn("failed to close MCP backend stream", "component", "sandbox", "session", t.sessionID, "error", err)
		}
		t.stream = nil
	}
	return nil
}

// backendStreamRWC adapts an ExecStream to io.ReadWriteCloser for mcp.IOTransport.
type backendStreamRWC struct {
	stream    ExecStream
	closeOnce sync.Once
	closeErr  error
}

func (s *backendStreamRWC) Read(p []byte) (int, error) {
	return s.stream.Read(p)
}

func (s *backendStreamRWC) Write(p []byte) (int, error) {
	return s.stream.Write(p)
}

func (s *backendStreamRWC) Close() error {
	s.closeOnce.Do(func() {
		// Close stdin by closing the write side — signals MCP server to shut down
		if closer, ok := s.stream.(io.Closer); ok {
			s.closeErr = closer.Close()
		}
	})
	return s.closeErr
}
