package sandbox

import (
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// ExecOpts configures an interactive exec session inside a container.
type ExecOpts struct {
	WorkDir string            // Working directory inside the container
	Env     map[string]string // Additional environment variables
	Rows    int               // Terminal height (default 24)
	Cols    int               // Terminal width (default 80)
}

// ContainerProcess represents a running interactive process inside an Incus container.
// It provides io.Reader/io.WriteCloser for the process's PTY, matching the
// interface expected by ProcessSession in process_mgr.go.
type ContainerProcess struct {
	// Stdout is the reader for the process's PTY output.
	// Read from this to get command output.
	Stdout io.Reader

	// Stdin is the writer for the process's PTY input.
	// Write to this to send keystrokes/data to the process.
	Stdin io.WriteCloser

	// dataDone is closed when all I/O goroutines have finished.
	dataDone chan bool

	// op is the Incus operation tracking this exec.
	op incus.Operation

	// controlConn is the websocket for sending control messages (resize, signals).
	controlConn *websocket.Conn

	// mu protects controlConn and closed state
	mu     sync.Mutex
	closed bool
}

// Wait blocks until the process exits and returns the exit code.
func (cp *ContainerProcess) Wait() (int, error) {
	// Wait for the Incus operation (process exit)
	if err := cp.op.Wait(); err != nil {
		// Even on error, try to get exit code from metadata
		return cp.exitCode(), fmt.Errorf("exec operation failed: %w", err)
	}

	// Wait for I/O to flush
	<-cp.dataDone

	return cp.exitCode(), nil
}

// exitCode extracts the exit code from the operation metadata.
func (cp *ContainerProcess) exitCode() int {
	opAPI := cp.op.Get()
	if opAPI.Metadata == nil {
		return -1
	}

	rc, ok := opAPI.Metadata["return"]
	if !ok {
		return -1
	}

	switch v := rc.(type) {
	case float64:
		return int(v)
	case string:
		code, _ := strconv.Atoi(v)
		return code
	default:
		return -1
	}
}

// Resize sends a window resize event to the container process.
func (cp *ContainerProcess) Resize(width, height int) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if cp.closed || cp.controlConn == nil {
		return nil
	}

	msg := api.InstanceExecControl{
		Command: "window-resize",
		Args: map[string]string{
			"width":  strconv.Itoa(width),
			"height": strconv.Itoa(height),
		},
	}

	return cp.controlConn.WriteJSON(msg)
}

// Close cleans up the container process resources.
func (cp *ContainerProcess) Close() error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if cp.closed {
		return nil
	}
	cp.closed = true

	// Close stdin to signal the process
	if cp.Stdin != nil {
		cp.Stdin.Close()
	}

	// Close control connection
	if cp.controlConn != nil {
		cp.controlConn.Close()
	}

	return nil
}

// ExecInteractive starts an interactive (PTY-backed) command inside a container.
// It returns a ContainerProcess with readers/writers that can be used by ProcessSession.
//
// The Incus SDK's ExecInstance handles websocket setup internally — we provide
// io.Pipe pairs as Stdin/Stdout, and the SDK wires them to the container's PTY
// via websockets. This gives us native Go I/O that works identically to local PTY I/O.
func ExecInteractive(client *IncusClient, containerName string, command []string, opts ExecOpts) (*ContainerProcess, error) {
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	if opts.Cols <= 0 {
		opts.Cols = 80
	}

	// Build environment: safe defaults + caller overrides
	env := map[string]string{
		"TERM":   "xterm-256color",
		"EDITOR": "true",
		"VISUAL": "true",
	}
	for k, v := range opts.Env {
		env[k] = v
	}

	req := api.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: true,
		Environment: env,
		Width:       opts.Cols,
		Height:      opts.Rows,
		Cwd:         opts.WorkDir,
	}

	// Create pipe pairs for stdin/stdout.
	// The SDK will copy between these pipes and the websocket internally.
	//
	// stdinReader -> [SDK copies to websocket] -> container PTY
	// container PTY -> [SDK copies from websocket] -> stdoutWriter
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	dataDone := make(chan bool)

	cp := &ContainerProcess{
		Stdout:   stdoutReader,
		Stdin:    stdinWriter,
		dataDone: dataDone,
	}

	args := &incus.InstanceExecArgs{
		Stdin:    stdinReader,
		Stdout:   stdoutWriter,
		Stderr:   stdoutWriter, // Interactive mode merges stderr into PTY, but set it anyway
		DataDone: dataDone,
		Control: func(conn *websocket.Conn) {
			// Store the control connection for resize/signal support
			cp.mu.Lock()
			cp.controlConn = conn
			cp.mu.Unlock()
		},
	}

	op, err := client.server.ExecInstance(containerName, req, args)
	if err != nil {
		stdinReader.Close()
		stdinWriter.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
		return nil, fmt.Errorf("failed to exec interactive command in %q: %w", containerName, err)
	}

	cp.op = op

	// When the operation completes (process exits), close the stdout writer
	// so that readers get EOF. This mirrors how local PTY gives EIO when
	// the slave side closes.
	go func() {
		_ = op.Wait()
		stdoutWriter.Close()
		stdinReader.Close()
	}()

	return cp, nil
}

// ExecNonInteractive starts a non-interactive command inside a container.
// Unlike ExecInteractive, this does NOT allocate a PTY — stdin/stdout are
// raw byte streams with no terminal processing (no echo, no line editing).
// This is essential for machine-to-machine communication like the NDJSON
// protocol used by `astonish node`, where PTY echo would corrupt the stream.
func ExecNonInteractive(client *IncusClient, containerName string, command []string, opts ExecOpts) (*ContainerProcess, error) {
	// Build environment
	env := map[string]string{}
	for k, v := range opts.Env {
		env[k] = v
	}

	req := api.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: false,
		Environment: env,
		Cwd:         opts.WorkDir,
	}

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	dataDone := make(chan bool)

	cp := &ContainerProcess{
		Stdout:   stdoutReader,
		Stdin:    stdinWriter,
		dataDone: dataDone,
	}

	args := &incus.InstanceExecArgs{
		Stdin:    stdinReader,
		Stdout:   stdoutWriter,
		Stderr:   stdoutWriter, // merge stderr into stdout
		DataDone: dataDone,
	}

	op, err := client.server.ExecInstance(containerName, req, args)
	if err != nil {
		stdinReader.Close()
		stdinWriter.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
		return nil, fmt.Errorf("failed to exec command in %q: %w", containerName, err)
	}

	cp.op = op

	go func() {
		_ = op.Wait()
		stdoutWriter.Close()
		stdinReader.Close()
	}()

	return cp, nil
}
