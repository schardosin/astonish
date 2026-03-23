package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/schardosin/astonish/pkg/config"
)

// ContainerMCPTransport implements mcp.Transport by running the MCP server
// process inside an Incus container instead of on the host. This is the core
// building block for sandboxed MCP execution.
//
// The MCP SDK uses the Transport interface to get a bidirectional JSON-RPC
// connection. The standard CommandTransport wraps os/exec.Cmd; this transport
// wraps a ContainerProcess started via ExecNonInteractive.
type ContainerMCPTransport struct {
	client        *IncusClient
	containerName string
	command       []string
	env           map[string]string

	// Stderr captures the MCP server's stderr output for diagnostics.
	Stderr *bytes.Buffer

	mu   sync.Mutex
	proc *ContainerProcess
}

// NewContainerMCPTransport creates a transport that will run the given MCP
// server config inside the specified container. The transport is not started
// until Connect() is called.
func NewContainerMCPTransport(client *IncusClient, containerName string, cfg config.MCPServerConfig) (*ContainerMCPTransport, *bytes.Buffer) {
	cmd := append([]string{cfg.Command}, cfg.Args...)
	env := make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		env[k] = v
	}

	var stderr bytes.Buffer
	return &ContainerMCPTransport{
		client:        client,
		containerName: containerName,
		command:       cmd,
		env:           env,
		Stderr:        &stderr,
	}, &stderr
}

// Connect starts the MCP server process inside the container and returns a
// Connection that communicates with it over stdin/stdout. This implements the
// mcp.Transport interface.
func (t *ContainerMCPTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Build env with a default PATH that includes common tool locations.
	// Non-interactive exec doesn't load shell profiles, so tools installed
	// via uv/npm (in /root/.local/bin, /usr/local/bin) need explicit PATH.
	env := make(map[string]string, len(t.env)+1)
	for k, v := range t.env {
		env[k] = v
	}
	if _, ok := env["PATH"]; !ok {
		env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/root/.local/bin"
	}

	proc, err := ExecNonInteractive(t.client, t.containerName, t.command, ExecOpts{
		Env:            env,
		SeparateStderr: t.Stderr, // Keep stderr separate from stdout to avoid corrupting JSON-RPC
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start MCP server in container %q: %w", t.containerName, err)
	}
	t.proc = proc

	// Wrap the container process stdin/stdout in an IOTransport.
	// The IOTransport creates a newline-delimited JSON connection from
	// separate reader/writer streams — exactly what the MCP protocol needs.
	//
	// We use a containerRWC that handles proper cleanup: closing stdin first
	// (signals the MCP server to shut down), then closing the process.
	rwc := &containerRWC{
		stdout: proc.Stdout,
		stdin:  proc.Stdin,
		proc:   proc,
	}

	inner := &mcp.IOTransport{
		Reader: rwc,
		Writer: rwc,
	}
	return inner.Connect(ctx)
}

// Close terminates the MCP server process inside the container.
func (t *ContainerMCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.proc != nil {
		if err := t.proc.Close(); err != nil {
			log.Printf("[sandbox] Warning: failed to close MCP container process: %v", err)
		}
		t.proc = nil
	}
	return nil
}

// containerRWC bridges a ContainerProcess's stdin/stdout to the
// io.ReadCloser/io.WriteCloser interface that mcp.IOTransport expects.
type containerRWC struct {
	stdout io.Reader      // ContainerProcess.Stdout
	stdin  io.WriteCloser // ContainerProcess.Stdin
	proc   *ContainerProcess

	closeOnce sync.Once
	closeErr  error
}

func (c *containerRWC) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *containerRWC) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *containerRWC) Close() error {
	c.closeOnce.Do(func() {
		// Close stdin first — this signals the MCP server to shut down
		// (same as the MCP spec's stdio shutdown procedure).
		if err := c.stdin.Close(); err != nil {
			log.Printf("[sandbox] Warning: failed to close MCP server stdin: %v", err)
		}
		// Close the container process (waits for exit, then cleans up).
		if c.proc != nil {
			c.closeErr = c.proc.Close()
		}
	})
	return c.closeErr
}
