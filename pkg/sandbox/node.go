package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// nodeRequest mirrors cmd/astonish.NodeRequest for the host side.
type nodeRequest struct {
	ID   string                 `json:"id"`
	Tool string                 `json:"tool"`
	Args map[string]interface{} `json:"args"`
}

// nodeResponse mirrors cmd/astonish.NodeResponse for the host side.
type nodeResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Ready  bool            `json:"ready,omitempty"`
}

// NodeClient manages a persistent NDJSON connection to an `astonish node`
// process running inside an Incus container. It starts the node via
// ExecInteractive and proxies tool calls over stdin/stdout.
//
// Sequential dispatch: one request at a time (no concurrent tool calls).
// Auto-restart: if the node process crashes, the next Call() restarts it.
type NodeClient struct {
	client        *IncusClient
	containerName string

	mu      sync.Mutex
	proc    *ContainerProcess
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	nextID  atomic.Int64
	started bool
	closed  bool
}

// NewNodeClient creates a NodeClient for the given container.
// The node is NOT started until Start() or the first Call().
func NewNodeClient(client *IncusClient, containerName string) *NodeClient {
	return &NodeClient{
		client:        client,
		containerName: containerName,
	}
}

// Start launches the `astonish node` process inside the container and waits
// for the ready signal. The container must already be running.
func (nc *NodeClient) Start() error {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	if nc.closed {
		return fmt.Errorf("node client is closed")
	}

	return nc.startLocked()
}

// startLocked starts the node process. Caller must hold nc.mu.
func (nc *NodeClient) startLocked() error {
	// Clean up any existing process
	nc.stopLocked()

	cmd := []string{BinaryDestPath, "node"}
	proc, err := ExecNonInteractive(nc.client, nc.containerName, cmd, ExecOpts{})
	if err != nil {
		return fmt.Errorf("failed to start astonish node in %q: %w", nc.containerName, err)
	}

	nc.proc = proc
	nc.stdin = proc.Stdin
	nc.scanner = bufio.NewScanner(proc.Stdout)

	// Increase scanner buffer for large responses (e.g., read_file of big files)
	const maxScanSize = 10 * 1024 * 1024 // 10MB
	nc.scanner.Buffer(make([]byte, 0, 64*1024), maxScanSize)

	// Wait for the ready signal with timeout
	readyCh := make(chan error, 1)
	go func() {
		if nc.scanner.Scan() {
			line := nc.scanner.Bytes()
			var resp nodeResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				readyCh <- fmt.Errorf("invalid ready message: %w", err)
				return
			}
			if !resp.Ready {
				readyCh <- fmt.Errorf("unexpected first message (expected ready): %s", string(line))
				return
			}
			readyCh <- nil
		} else {
			if err := nc.scanner.Err(); err != nil {
				readyCh <- fmt.Errorf("node stdout closed before ready: %w", err)
			} else {
				readyCh <- fmt.Errorf("node stdout closed before ready (EOF)")
			}
		}
	}()

	select {
	case err := <-readyCh:
		if err != nil {
			nc.stopLocked()
			return err
		}
	case <-time.After(30 * time.Second):
		nc.stopLocked()
		return fmt.Errorf("timeout waiting for node ready signal in %q", nc.containerName)
	}

	nc.started = true
	return nil
}

// stopLocked stops the current node process. Caller must hold nc.mu.
func (nc *NodeClient) stopLocked() {
	if nc.proc != nil {
		nc.proc.Close()
		nc.proc = nil
	}
	nc.stdin = nil
	nc.scanner = nil
	nc.started = false
}

// Call sends a tool execution request to the node and blocks until
// the response is received. Returns the result as raw JSON (to be
// unmarshalled by the caller) or an error.
//
// If the node is not started or has crashed, Call() attempts to restart it.
func (nc *NodeClient) Call(toolName string, args map[string]interface{}) (json.RawMessage, error) {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	if nc.closed {
		return nil, fmt.Errorf("node client is closed")
	}

	// Auto-start or restart if needed
	if !nc.started {
		if err := nc.startLocked(); err != nil {
			return nil, fmt.Errorf("failed to start node: %w", err)
		}
	}

	id := strconv.FormatInt(nc.nextID.Add(1), 10)

	req := nodeRequest{
		ID:   id,
		Tool: toolName,
		Args: args,
	}

	// Send request
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	reqBytes = append(reqBytes, '\n')

	if _, err := nc.stdin.Write(reqBytes); err != nil {
		// Node likely crashed — mark for restart on next call
		nc.stopLocked()
		return nil, fmt.Errorf("failed to write to node stdin: %w", err)
	}

	// Read response
	if !nc.scanner.Scan() {
		// Node stdout closed — process likely crashed
		nc.stopLocked()
		if err := nc.scanner.Err(); err != nil {
			return nil, fmt.Errorf("node stdout error: %w", err)
		}
		return nil, fmt.Errorf("node process exited unexpectedly")
	}

	line := nc.scanner.Bytes()
	var resp nodeResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("invalid response from node: %w (line: %s)", err, string(line))
	}

	// Verify correlation ID
	if resp.ID != id {
		return nil, fmt.Errorf("response ID mismatch: expected %s, got %s", id, resp.ID)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("tool %q error: %s", toolName, resp.Error)
	}

	return resp.Result, nil
}

// Close shuts down the node process and releases resources.
func (nc *NodeClient) Close() error {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	if nc.closed {
		return nil
	}
	nc.closed = true
	nc.stopLocked()
	return nil
}

// IsStarted returns whether the node process is currently running.
func (nc *NodeClient) IsStarted() bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return nc.started
}

// ContainerName returns the name of the container this client is connected to.
func (nc *NodeClient) ContainerName() string {
	return nc.containerName
}

// LazyNodeClient wraps lazy container creation + NodeClient initialization.
// On the first Call(), it creates the session container, starts the node,
// and then proxies. Subsequent calls go directly to the underlying NodeClient.
//
// This is needed because the factory builds tools before a session ID exists.
// The session ID becomes available during ProcessRequest (before the LLM call).
//
// Lifecycle:
//  1. Factory creates LazyNodeClient (no session ID yet)
//  2. NodeTool.ProcessRequest calls BindSession(sessionID) — starts container in background
//  3. NodeTool.Run calls Call() — waits for background init if still in progress, then forwards
type LazyNodeClient struct {
	incusClient  *IncusClient
	sessRegistry *SessionRegistry
	template     string // template to clone from (empty = @base)

	mu            sync.Mutex
	sessionID     string
	nodeClient    *NodeClient
	containerName string
	initialized   bool
	closed        bool

	// initDone is created by BindSession and closed when background init completes.
	// Call() waits on this channel before forwarding to the NodeClient.
	initDone chan struct{}
	initErr  error
}

// NewLazyNodeClient creates a lazy node client that defers container creation
// until BindSession is called (typically from ProcessRequest, before the LLM call).
func NewLazyNodeClient(client *IncusClient, registry *SessionRegistry, template string) *LazyNodeClient {
	return &LazyNodeClient{
		incusClient:  client,
		sessRegistry: registry,
		template:     template,
	}
}

// BindSession starts container creation and node startup in the background.
// This is idempotent — only the first call for a given session ID triggers init.
// Typically called from NodeTool.ProcessRequest, which runs before the LLM call,
// giving the container time to start while the LLM generates its response.
func (lnc *LazyNodeClient) BindSession(sessionID string) {
	if sessionID == "" {
		return
	}

	lnc.mu.Lock()
	// Already bound (same or different session) — no-op
	if lnc.initDone != nil {
		lnc.mu.Unlock()
		return
	}

	if lnc.closed {
		lnc.mu.Unlock()
		return
	}

	lnc.sessionID = sessionID
	lnc.initDone = make(chan struct{})
	lnc.mu.Unlock()

	// Start container creation + node startup in background
	go lnc.initBackground(sessionID)
}

// initBackground runs container creation and node startup, then signals completion.
func (lnc *LazyNodeClient) initBackground(sessionID string) {
	defer close(lnc.initDone)

	// Create or get the session container
	containerName, err := EnsureSessionContainer(lnc.incusClient, lnc.sessRegistry, sessionID, lnc.template)
	if err != nil {
		lnc.mu.Lock()
		lnc.initErr = fmt.Errorf("failed to create session container: %w", err)
		lnc.mu.Unlock()
		return
	}

	// Create and start the node client
	nc := NewNodeClient(lnc.incusClient, containerName)
	if err := nc.Start(); err != nil {
		lnc.mu.Lock()
		lnc.initErr = fmt.Errorf("failed to start node in %q: %w", containerName, err)
		lnc.containerName = containerName // store for cleanup even on failure
		lnc.mu.Unlock()
		return
	}

	lnc.mu.Lock()
	lnc.containerName = containerName
	lnc.nodeClient = nc
	lnc.initialized = true
	lnc.mu.Unlock()
}

// Call proxies a tool call to the container node. If BindSession was called,
// it waits for the background init to complete. If not, it does a synchronous
// init as a fallback (safety net for code paths that skip ProcessRequest).
func (lnc *LazyNodeClient) Call(sessionID, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	// Ensure BindSession was called (idempotent — no-op if already called)
	lnc.BindSession(sessionID)

	// Wait for background init to complete
	<-lnc.initDone

	lnc.mu.Lock()
	if lnc.closed {
		lnc.mu.Unlock()
		return nil, fmt.Errorf("lazy node client is closed")
	}
	if lnc.initErr != nil {
		err := lnc.initErr
		lnc.mu.Unlock()
		return nil, err
	}
	nc := lnc.nodeClient
	lnc.mu.Unlock()

	// Delegate to the NodeClient (which handles its own locking)
	return nc.Call(toolName, args)
}

// Close shuts down the node and optionally the container.
func (lnc *LazyNodeClient) Close() error {
	lnc.mu.Lock()
	defer lnc.mu.Unlock()

	if lnc.closed {
		return nil
	}
	lnc.closed = true

	if lnc.nodeClient != nil {
		lnc.nodeClient.Close()
	}

	return nil
}

// Cleanup closes the node and destroys the session container.
func (lnc *LazyNodeClient) Cleanup() {
	// If init is in progress, wait for it to finish before cleaning up
	lnc.mu.Lock()
	done := lnc.initDone
	lnc.mu.Unlock()
	if done != nil {
		<-done
	}

	lnc.mu.Lock()
	defer lnc.mu.Unlock()

	if lnc.nodeClient != nil {
		lnc.nodeClient.Close()
		lnc.nodeClient = nil
	}

	if lnc.containerName != "" && lnc.incusClient != nil {
		_ = lnc.incusClient.StopAndDeleteInstance(lnc.containerName)
	}

	if lnc.containerName != "" && lnc.sessRegistry != nil {
		// Find and remove the session registry entry for this container
		for _, entry := range lnc.sessRegistry.List() {
			if entry.ContainerName == lnc.containerName {
				_ = lnc.sessRegistry.Remove(entry.SessionID)
				break
			}
		}
	}

	lnc.initialized = false
	lnc.closed = true
}

// IsInitialized returns whether the container and node are running.
func (lnc *LazyNodeClient) IsInitialized() bool {
	lnc.mu.Lock()
	defer lnc.mu.Unlock()
	return lnc.initialized
}

// GetContainerName returns the container name (empty if not yet initialized).
func (lnc *LazyNodeClient) GetContainerName() string {
	lnc.mu.Lock()
	defer lnc.mu.Unlock()
	return lnc.containerName
}
