package sandbox

import (
	"fmt"
	"net"
	"time"
)

// ExecConn wraps an Incus exec session (running socat) as a net.Conn.
// It tunnels a single TCP connection through the Incus API's WebSocket-based
// exec mechanism, making services inside containers reachable from any host
// (including macOS via Docker+Incus where container bridge IPs are not routable).
//
// The underlying process is: socat STDIO TCP:<host>:<port>
// Read/Write map to the process's stdout/stdin via io.Pipe → WebSocket → Incus.
type ExecConn struct {
	proc   *ContainerProcess
	local  execAddr
	remote execAddr
}

// execAddr is a minimal net.Addr for informational purposes.
type execAddr struct {
	container string
	port      int
}

func (a execAddr) Network() string { return "exec" }
func (a execAddr) String() string  { return fmt.Sprintf("%s:%d", a.container, a.port) }

// DialViaExec establishes a TCP tunnel to host:port inside a container by
// running `socat STDIO TCP:host:port` through the Incus exec API.
//
// The returned net.Conn routes all Read/Write calls through the Incus API
// WebSocket, so it works regardless of whether the container's bridge IP is
// reachable from the host (Linux native or Docker+Incus on macOS/Windows).
//
// Prerequisites: socat must be installed in the container (part of CoreTools).
//
// Lifecycle: closing the returned conn closes stdin, which causes socat to exit.
// If the remote TCP connection closes first, socat exits and stdout returns EOF.
func DialViaExec(client *IncusClient, containerName string, host string, port int) (net.Conn, error) {
	cmd := []string{"socat", "STDIO", fmt.Sprintf("TCP:%s:%d", host, port)}

	proc, err := ExecNonInteractive(client, containerName, cmd, ExecOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to start tunnel to %s:%d in %q: %w", host, port, containerName, err)
	}

	return &ExecConn{
		proc:   proc,
		local:  execAddr{container: containerName, port: 0},
		remote: execAddr{container: containerName, port: port},
	}, nil
}

// Read reads data from the tunneled TCP connection (via socat stdout).
func (c *ExecConn) Read(b []byte) (int, error) {
	return c.proc.Stdout.Read(b)
}

// Write sends data to the tunneled TCP connection (via socat stdin).
func (c *ExecConn) Write(b []byte) (int, error) {
	return c.proc.Stdin.Write(b)
}

// Close terminates the tunnel by closing stdin (socat exits on stdin EOF).
// Idempotent — safe to call multiple times.
func (c *ExecConn) Close() error {
	return c.proc.Close()
}

// LocalAddr returns a stub address identifying the tunnel.
func (c *ExecConn) LocalAddr() net.Addr { return c.local }

// RemoteAddr returns a stub address identifying the tunnel target.
func (c *ExecConn) RemoteAddr() net.Addr { return c.remote }

// SetDeadline is a no-op. The underlying io.Pipe does not support deadlines.
// Use context cancellation for timeouts instead.
func (c *ExecConn) SetDeadline(_ time.Time) error { return nil }

// SetReadDeadline is a no-op. See SetDeadline.
func (c *ExecConn) SetReadDeadline(_ time.Time) error { return nil }

// SetWriteDeadline is a no-op. See SetDeadline.
func (c *ExecConn) SetWriteDeadline(_ time.Time) error { return nil }
