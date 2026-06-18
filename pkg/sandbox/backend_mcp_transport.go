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
	// The jsonPrefixFilterReader discards non-JSON preamble lines
	// (e.g. "Tavily MCP server running on stdio") that some servers
	// print to stdout before the JSON-RPC stream starts.
	rwc := &backendStreamRWC{stream: stream}
	filteredReader := newJSONPrefixFilterReader(rwc)

	inner := &mcp.IOTransport{
		Reader: filteredReader,
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

// jsonPrefixFilterReader wraps an io.Reader and discards any non-JSON lines
// before the first line that starts with '{'. Some MCP servers (e.g. tavily-mcp)
// print human-readable startup messages to stdout before the JSON-RPC stream
// begins. When connected via a PTY (OpenShell always allocates one), these
// messages cannot be separated via stderr and would break json.Decoder.
//
// After the first '{' byte is encountered, this reader becomes transparent
// and passes all subsequent bytes through without inspection.
type jsonPrefixFilterReader struct {
	inner      io.ReadCloser
	foundJSON  bool
	buf        []byte // leftover from partial read that found JSON
	bufOffset  int
}

func newJSONPrefixFilterReader(r io.ReadCloser) *jsonPrefixFilterReader {
	return &jsonPrefixFilterReader{inner: r}
}

func (f *jsonPrefixFilterReader) Read(p []byte) (int, error) {
	// Fast path: after finding JSON, pass through directly.
	if f.foundJSON {
		// Drain any buffered leftover first.
		if f.bufOffset < len(f.buf) {
			n := copy(p, f.buf[f.bufOffset:])
			f.bufOffset += n
			if f.bufOffset >= len(f.buf) {
				f.buf = nil
				f.bufOffset = 0
			}
			return n, nil
		}
		return f.inner.Read(p)
	}

	// Slow path: read and discard non-JSON lines until we find one starting with '{'.
	tmp := make([]byte, len(p))
	for {
		n, err := f.inner.Read(tmp[:])
		if n == 0 {
			return 0, err
		}

		data := tmp[:n]
		// Look for the first '{' in the data — that marks the start of JSON-RPC.
		idx := bytes.IndexByte(data, '{')
		if idx >= 0 {
			f.foundJSON = true
			// Everything from '{' onward is valid; discard anything before it.
			remaining := data[idx:]
			copied := copy(p, remaining)
			if copied < len(remaining) {
				// Buffer the excess.
				f.buf = append([]byte(nil), remaining[copied:]...)
				f.bufOffset = 0
			}
			return copied, nil
		}

		// No '{' found — entire chunk is non-JSON preamble, discard it.
		// If the underlying reader errored (including EOF), propagate.
		if err != nil {
			return 0, err
		}
		// Continue reading.
	}
}

func (f *jsonPrefixFilterReader) Close() error {
	return f.inner.Close()
}
