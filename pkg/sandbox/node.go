package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	// Env holds environment variables to inject into the node process.
	// Set before Start() or startLocked() to pass credentials (GH_TOKEN,
	// BIFROST_API_KEY, etc.) into the container.
	Env map[string]string
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
	proc, err := ExecNonInteractive(nc.client, nc.containerName, cmd, ExecOpts{Env: nc.Env})
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

	// Env holds environment variables to inject into the node process.
	// Set before BindSession() to pass credentials into the container.
	Env map[string]string

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
	nc.Env = lnc.Env // Forward environment variables (credentials) to node
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

	// If GH_TOKEN is available, configure git credential helper in the background.
	// gh auth setup-git configures git to use `gh` as a credential helper, which
	// reads GH_TOKEN from the environment. This enables `git clone` of private repos.
	if ghToken := lnc.Env["GH_TOKEN"]; ghToken != "" {
		go func() {
			_, err := ExecSimpleWithEnv(lnc.incusClient, containerName,
				[]string{"sh", "-c", "command -v gh >/dev/null 2>&1 && gh auth setup-git"},
				map[string]string{"GH_TOKEN": ghToken})
			if err != nil {
				log.Printf("[sandbox] Warning: failed to run gh auth setup-git in %q: %v", containerName, err)
			}
		}()
	}
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

// GetIncusClient returns the Incus client for host-side operations (e.g., snapshotting).
func (lnc *LazyNodeClient) GetIncusClient() *IncusClient {
	return lnc.incusClient
}

// GetSessionRegistry returns the session registry.
func (lnc *LazyNodeClient) GetSessionRegistry() *SessionRegistry {
	return lnc.sessRegistry
}

// StopNode stops the node process without destroying the container.
// Used when snapshotting the container (must be quiescent).
// Call RestartNode() to bring the node back up afterward.
func (lnc *LazyNodeClient) StopNode() error {
	lnc.mu.Lock()
	defer lnc.mu.Unlock()

	if lnc.nodeClient == nil {
		return nil
	}

	return lnc.nodeClient.Close()
}

// RestartNode restarts the node process after it was stopped.
// Re-injects the Env variables for credentials.
func (lnc *LazyNodeClient) RestartNode() error {
	lnc.mu.Lock()
	defer lnc.mu.Unlock()

	if lnc.containerName == "" {
		return fmt.Errorf("no container to restart node in")
	}

	nc := NewNodeClient(lnc.incusClient, lnc.containerName)
	nc.Env = lnc.Env
	if err := nc.Start(); err != nil {
		return fmt.Errorf("failed to restart node in %q: %w", lnc.containerName, err)
	}

	lnc.nodeClient = nc
	lnc.initialized = true
	return nil
}

// ---------------------------------------------------------------------------
// NodeClientPool — per-session container multiplexer
// ---------------------------------------------------------------------------

// NodeClientPool manages a set of LazyNodeClient instances, one per session.
// When a tool call arrives for a session that doesn't have a client yet, the
// pool creates one. This gives every chat/Studio session its own container.
//
// Fleet sessions bypass the pool — they create LazyNodeClient directly via
// wireFleetSandbox(), which is correct because each fleet session already
// has its own lifecycle.
type NodeClientPool struct {
	incusClient  *IncusClient
	sessRegistry *SessionRegistry
	template     string

	mu      sync.Mutex
	clients map[string]*LazyNodeClient
	env     map[string]string
	closed  bool
}

// NewNodeClientPool creates a pool that will create per-session LazyNodeClients
// on demand. The template parameter selects which container template to clone
// from (empty = @base).
func NewNodeClientPool(client *IncusClient, registry *SessionRegistry, template string) *NodeClientPool {
	return &NodeClientPool{
		incusClient:  client,
		sessRegistry: registry,
		template:     template,
		clients:      make(map[string]*LazyNodeClient),
	}
}

// SetEnv sets environment variables that will be injected into all future
// LazyNodeClient instances created by this pool. Must be called before any
// GetOrCreate calls (typically at factory init time).
func (p *NodeClientPool) SetEnv(env map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.env = env
}

// GetOrCreate returns the LazyNodeClient for the given session ID, creating
// one if it doesn't exist yet. Thread-safe.
func (p *NodeClientPool) GetOrCreate(sessionID string) *LazyNodeClient {
	if sessionID == "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	if client, ok := p.clients[sessionID]; ok {
		return client
	}

	// Create a new LazyNodeClient for this session
	client := NewLazyNodeClient(p.incusClient, p.sessRegistry, p.template)
	client.Env = p.env
	p.clients[sessionID] = client
	return client
}

// Remove stops the node for a session and removes it from the pool.
// Does NOT destroy the Incus container — that's TryDestroySessionContainer's
// job from the session deletion handler.
func (p *NodeClientPool) Remove(sessionID string) {
	p.mu.Lock()
	client, ok := p.clients[sessionID]
	if ok {
		delete(p.clients, sessionID)
	}
	p.mu.Unlock()

	if ok && client != nil {
		_ = client.Close()
	}
}

// Cleanup destroys all session containers managed by this pool. Called on
// factory shutdown / daemon restart.
func (p *NodeClientPool) Cleanup() {
	p.mu.Lock()
	p.closed = true
	clients := make(map[string]*LazyNodeClient, len(p.clients))
	for k, v := range p.clients {
		clients[k] = v
	}
	p.clients = nil
	p.mu.Unlock()

	for _, client := range clients {
		client.Cleanup()
	}
}

// GetContainerName returns the container name for a session (empty if not initialized).
func (p *NodeClientPool) GetContainerName(sessionID string) string {
	p.mu.Lock()
	client, ok := p.clients[sessionID]
	p.mu.Unlock()

	if !ok || client == nil {
		return ""
	}
	return client.GetContainerName()
}

// StopNode stops the node process for a session without destroying the container.
func (p *NodeClientPool) StopNode(sessionID string) error {
	p.mu.Lock()
	client, ok := p.clients[sessionID]
	p.mu.Unlock()

	if !ok || client == nil {
		return fmt.Errorf("no client for session %s", sessionID)
	}
	return client.StopNode()
}

// RestartNode restarts the node process for a session.
func (p *NodeClientPool) RestartNode(sessionID string) error {
	p.mu.Lock()
	client, ok := p.clients[sessionID]
	p.mu.Unlock()

	if !ok || client == nil {
		return fmt.Errorf("no client for session %s", sessionID)
	}
	return client.RestartNode()
}

// GetIncusClient returns the shared Incus client.
func (p *NodeClientPool) GetIncusClient() *IncusClient {
	return p.incusClient
}
