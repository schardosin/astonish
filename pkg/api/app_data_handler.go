package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// loadAppFromStore loads an app from the request-scoped store and converts
// the map[string]any result to a *apps.VisualApp for DataSource resolution.
func loadAppFromStore(r *http.Request, name string) (*apps.VisualApp, error) {
	svc := store.FromRequest(r)
	if svc == nil {
		return nil, fmt.Errorf("store not available")
	}

	slug := apps.Slugify(name)

	var raw any
	var err error

	// Try personal first, then team
	if svc.PersonalApps != nil {
		raw, err = svc.PersonalApps.Load(r.Context(), slug)
	}
	if (raw == nil || err != nil) && svc.Apps != nil {
		raw, err = svc.Apps.Load(r.Context(), slug)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load app %q: %w", name, err)
	}
	if raw == nil {
		return nil, fmt.Errorf("app %q not found", name)
	}

	// Convert map[string]any → *apps.VisualApp via JSON round-trip.
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize app %q: %w", name, err)
	}
	var app apps.VisualApp
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, fmt.Errorf("failed to deserialize app %q: %w", name, err)
	}
	return &app, nil
}

// maxResponseBodySize is the maximum number of bytes the HTTP data proxy will
// read from an external response. This prevents out-of-memory conditions if
// a sandboxed app (accidentally or maliciously) points to a huge resource.
const maxResponseBodySize = 10 << 20 // 10 MB

// privateIPNets lists CIDR ranges that the HTTP data proxy must never contact.
// This prevents SSRF attacks where a sandboxed app could probe internal
// services, cloud metadata endpoints, or other private infrastructure.
var privateIPNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"0.0.0.0/8",          // "this" network (RFC 1122)
		"10.0.0.0/8",         // RFC 1918
		"100.64.0.0/10",      // shared address space (RFC 6598, e.g. CGNAT)
		"127.0.0.0/8",        // loopback
		"169.254.0.0/16",     // link-local / cloud metadata (AWS, GCP, Azure)
		"172.16.0.0/12",      // RFC 1918
		"192.0.0.0/24",       // IETF protocol assignments (RFC 6890)
		"192.168.0.0/16",     // RFC 1918
		"198.18.0.0/15",      // benchmarking (RFC 2544)
		"::1/128",            // IPv6 loopback
		"fc00::/7",           // IPv6 unique local (RFC 4193)
		"fe80::/10",          // IPv6 link-local
	} {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("bad CIDR in privateIPNets: %s: %v", cidr, err))
		}
		privateIPNets = append(privateIPNets, ipNet)
	}
}

// isPrivateIP reports whether the given IP falls within a private,
// loopback, link-local, or otherwise non-public address range.
func isPrivateIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 (e.g. ::ffff:127.0.0.1) to plain IPv4
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, ipNet := range privateIPNets {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfSafeTransport returns an *http.Transport that validates resolved IPs
// before connecting, blocking requests to private/internal networks.
// Checking at the dial level prevents DNS-rebinding attacks where a public
// hostname resolves to 127.0.0.1 or another private address.
func ssrfSafeTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}

			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("DNS lookup failed for %q: %w", host, err)
			}

			for _, ipAddr := range ips {
				if isPrivateIP(ipAddr.IP) {
					return nil, fmt.Errorf("requests to private/internal networks are not allowed (host %q resolved to %s)", host, ipAddr.IP)
				}
			}

			// All IPs are public — connect to the first one.
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 4,
	}
}

// appDataRequest is the JSON body for POST /api/apps/data.
// sourceId uses convention-based routing:
//   - "mcp:<serverName>/<toolName>"      → invoke MCP tool
//   - "http:<METHOD>:<url>"              → make HTTP request (no auth)
//   - "http:<METHOD>:<url>@<credential>" → make HTTP request with credential auth
//   - "static:<key>"                     → return static data from app config
type appDataRequest struct {
	SourceID  string         `json:"sourceId"`
	Args      map[string]any `json:"args"`
	RequestID string         `json:"requestId"`
	AppName   string         `json:"appName,omitempty"` // Optional: for saved app data source lookups
}

// appDataResponse is the JSON response for data/action endpoints.
type appDataResponse struct {
	RequestID string `json:"requestId"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AppDataHandler handles one-shot data requests from the sandboxed iframe.
// The parent page relays postMessage data_requests here, and the response
// flows back: Go → parent → postMessage → iframe.
//
// POST /api/apps/data
func AppDataHandler(w http.ResponseWriter, r *http.Request) {
	var req appDataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.SourceID == "" {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			RequestID: req.RequestID,
			Error:     "sourceId is required",
		})
		return
	}

	slog.Debug("app data request", "sourceId", req.SourceID, "requestId", req.RequestID)

	data, err := resolveDataSource(r, req.SourceID, req.Args, req.AppName)
	if err != nil {
		slog.Debug("app data request failed", "sourceId", req.SourceID, "error", err)
		respondJSON(w, http.StatusOK, appDataResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, appDataResponse{
		RequestID: req.RequestID,
		Data:      data,
	})
}

// AppActionHandler handles action requests (mutations) from the sandboxed iframe.
// Similar to AppDataHandler but semantically for write operations.
//
// POST /api/apps/action
func AppActionHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ActionID  string         `json:"actionId"`
		Payload   map[string]any `json:"payload"`
		RequestID string         `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.ActionID == "" {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			RequestID: req.RequestID,
			Error:     "actionId is required",
		})
		return
	}

	slog.Debug("app action request", "actionId", req.ActionID, "requestId", req.RequestID)

	// Actions use the same routing convention as data sources
	data, err := resolveDataSource(r, req.ActionID, req.Payload, "")
	if err != nil {
		respondJSON(w, http.StatusOK, appDataResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, appDataResponse{
		RequestID: req.RequestID,
		Data:      data,
	})
}

// AppStreamHandler provides SSE streaming for saved apps with data sources
// that have a refresh interval. The parent page connects to this and forwards
// data_update events to the iframe.
//
// GET /api/apps/{name}/stream?sourceId=X
func AppStreamHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	sourceID := r.URL.Query().Get("sourceId")

	if sourceID == "" {
		respondError(w, http.StatusBadRequest, "sourceId query parameter is required")
		return
	}

	// Load the app to get data source config (for interval)
	app, err := loadAppFromStore(r, name)
	if err != nil {
		respondError(w, http.StatusNotFound, "app not found: "+err.Error())
		return
	}

	// Find the data source config
	var interval time.Duration
	var dsArgs map[string]any
	for _, ds := range app.DataSources {
		if ds.ID == sourceID {
			if ds.Interval != "" {
				interval, _ = time.ParseDuration(ds.Interval)
			}
			dsArgs = ds.Config
			break
		}
	}

	// Default to 30s if no interval configured
	if interval <= 0 {
		interval = 30 * time.Second
	}

	// Minimum interval of 5s to prevent abuse
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	// Set up SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	slog.Debug("app stream started", "app", name, "sourceId", sourceID, "interval", interval)

	// Send initial data immediately
	data, err := resolveDataSource(r, sourceID, dsArgs, name)
	sendStreamEvent(w, flusher, sourceID, data, err)

	// Poll at the configured interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			slog.Debug("app stream ended (client disconnected)", "app", name, "sourceId", sourceID)
			return
		case <-ticker.C:
			data, err := resolveDataSource(r, sourceID, dsArgs, name)
			sendStreamEvent(w, flusher, sourceID, data, err)
		}
	}
}

// sendStreamEvent writes a single SSE event with data_update payload.
func sendStreamEvent(w http.ResponseWriter, flusher http.Flusher, sourceID string, data any, err error) {
	evt := map[string]any{
		"sourceId": sourceID,
	}
	if err != nil {
		evt["error"] = err.Error()
	} else {
		evt["data"] = data
	}
	jsonBytes, _ := json.Marshal(evt)
	fmt.Fprintf(w, "event: data_update\ndata: %s\n\n", jsonBytes)
	flusher.Flush()
}

// resolveDataSource routes a sourceId to the appropriate backend:
//   - "mcp:<server>/<tool>"           → MCP tool invocation
//   - "http:<METHOD>:<url>"           → server-side HTTP request (no auth)
//   - "http:<METHOD>:<url>@<cred>"    → server-side HTTP request with credential auth
//   - "static:<key>"                  → static data from app's DataSource config
//
// If sourceId doesn't match any convention, it tries to find a matching
// DataSource in the saved app (if appName is provided).
func resolveDataSource(r *http.Request, sourceID string, args map[string]any, appName string) (any, error) {
	// Convention-based routing
	if strings.HasPrefix(sourceID, "mcp:") {
		return resolveMCPSource(r, sourceID[4:], args)
	}
	if strings.HasPrefix(sourceID, "http:") {
		return resolveHTTPSource(r, sourceID[5:], args)
	}
	if strings.HasPrefix(sourceID, "static:") {
		return resolveStaticSource(r, sourceID[7:], appName)
	}

	// Fallback: look up in saved app's DataSources config
	if appName != "" {
		return resolveAppDataSource(r, sourceID, args, appName)
	}

	return nil, fmt.Errorf("unknown source format: %q (expected mcp:<server>/<tool>, http:<METHOD>:<url>, or static:<key>)", sourceID)
}

// resolveMCPSource invokes an MCP tool inside a sandbox container.
// serverTool format: "<serverName>/<toolName>"
//
// Security: stdio MCP servers are ALWAYS executed inside a per-user sandbox
// container (never on the host). SSE/remote servers connect over the network.
func resolveMCPSource(r *http.Request, serverTool string, args map[string]any) (any, error) {
	parts := strings.SplitN(serverTool, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid MCP source format: expected 'server/tool', got %q", serverTool)
	}
	serverName := parts[0]
	toolName := parts[1]

	// Load server config from DB (3-tier platform merge)
	mcpCfg := loadMCPConfigForRequest(r)
	serverCfg, ok := mcpCfg.MCPServers[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not configured", serverName)
	}

	// SSE/remote servers: direct network connection (no local exec)
	if serverCfg.Transport == "sse" || serverCfg.URL != "" {
		return invokeMCPToolSSE(r.Context(), serverName, toolName, serverCfg, args)
	}

	// Stdio servers: MUST run inside a sandbox container
	appCfg, _ := config.LoadAppConfig()
	if appCfg == nil || !sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		return nil, fmt.Errorf("MCP server %q requires sandbox mode (stdio transport cannot run on host)", serverName)
	}

	// Resolve the team's template (custom template takes precedence over @base)
	templateName := ""
	if svc := store.FromRequest(r); svc != nil && svc.Settings != nil {
		if settings, err := svc.Settings.Get(r.Context()); err == nil && settings != nil && settings.TemplateName != "" {
			templateName = settings.TemplateName
		}
	}

	userID := effectiveUserID(r)
	return invokeMCPToolInContainer(r.Context(), userID, templateName, serverName, toolName, serverCfg, args)
}

// invokeMCPToolInContainer runs an MCP tool inside a per-user sandbox container.
// The container is created on first use and destroyed after idle timeout.
// Works for both Incus and K8s backends via the abstract sandbox.Backend interface.
func invokeMCPToolInContainer(ctx context.Context, userID, templateName, serverName, toolName string, serverCfg config.MCPServerConfig, args map[string]any) (any, error) {
	// Get the abstract sandbox backend (works for both Incus and K8s)
	appCfg, _ := config.LoadAppConfig()
	backend, cleanup, err := sandbox.BackendFromAppConfig(appCfg)
	if err != nil {
		return nil, fmt.Errorf("sandbox unavailable for app MCP: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Ensure the idle watchdog is running with a reference to the backend factory
	appMCPIdleTracker.StartIdleWatchdog(context.Background(), 10*time.Minute)

	// Create/ensure per-user app session (idempotent — returns existing if present)
	syntheticSessionID := "app-mcp-" + userID
	if templateName == "" {
		templateName = sandbox.BaseTemplateID
	}

	// Resolve layer chain so K8s gets content-addressed layer IDs (same as
	// chat sessions). Without this, the pod would only mount the bare @base
	// layer, missing any runtimes installed via "Configure Base".
	var layerChain []string
	if templateName != sandbox.BaseTemplateID {
		layerChain = resolveTemplateLayerChain(ctx, templateName)
	}
	if len(layerChain) == 0 {
		layerChain = resolveBaseLayerChain(ctx)
	}

	_, err = backend.CreateSession(ctx, sandbox.SessionSpec{
		SessionID:  syntheticSessionID,
		Type:       sandbox.SessionTypeChat,
		TemplateID: templateName,
		LayerChain: layerChain,
		UserID:     userID,
		Labels:     map[string]string{"purpose": "app-mcp"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure app MCP session for user %q: %w", userID, err)
	}

	// Wait for the session to be running before exec'ing into it.
	// On K8s this waits for pod scheduling + image pull + overlay mount.
	// On Incus this returns almost immediately.
	if err := backend.WaitForSessionReady(ctx, syntheticSessionID); err != nil {
		return nil, fmt.Errorf("app MCP session not ready for user %q: %w", userID, err)
	}

	// Mark as active immediately (prevents idle watchdog from destroying mid-request)
	appMCPIdleTracker.touch(syntheticSessionID)

	// Create backend-agnostic transport for the MCP server
	transport, stderrBuf := sandbox.NewBackendMCPTransport(backend, syntheticSessionID, serverCfg)

	// Create ADK toolset (connects to the MCP server inside the container/pod)
	toolset, err := mcptoolset.New(mcptoolset.Config{
		Transport: transport,
	})
	if err != nil {
		transport.Close()
		stderrStr := stderrBuf.String()
		return nil, fmt.Errorf("failed to start MCP server %q in sandbox: %w (stderr: %s)", serverName, err, stderrStr)
	}

	// Get tools from the server
	toolCtx := &appMCPToolContext{Context: ctx}
	tools, err := toolset.Tools(toolCtx)
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("failed to list tools from MCP server %q: %w", serverName, err)
	}

	// Find the requested tool
	var targetTool tool.Tool
	for _, t := range tools {
		if declTool, ok := t.(interface {
			Declaration() *genai.FunctionDeclaration
		}); ok {
			decl := declTool.Declaration()
			if decl != nil && decl.Name == toolName {
				targetTool = t
				break
			}
		}
	}
	if targetTool == nil {
		transport.Close()
		available := make([]string, 0, len(tools))
		for _, t := range tools {
			if declTool, ok := t.(interface {
				Declaration() *genai.FunctionDeclaration
			}); ok {
				if decl := declTool.Declaration(); decl != nil {
					available = append(available, decl.Name)
				}
			}
		}
		return nil, fmt.Errorf("tool %q not found on MCP server %q (available: %v)", toolName, serverName, available)
	}

	// Invoke the tool
	runner, ok := targetTool.(interface {
		Run(tool.Context, any) (map[string]any, error)
	})
	if !ok {
		transport.Close()
		return nil, fmt.Errorf("tool %q on %q does not implement Run", toolName, serverName)
	}

	slog.Debug("app MCP invoke", "server", serverName, "tool", toolName, "session", syntheticSessionID)
	result, err := runner.Run(toolCtx, args)
	transport.Close()
	if err != nil {
		return nil, fmt.Errorf("MCP tool %q returned error: %w", toolName, err)
	}

	// Update last activity so idle watchdog knows the session is in use
	appMCPIdleTracker.touch(syntheticSessionID)

	return result, nil
}

// invokeMCPToolSSE invokes an MCP tool on a remote SSE server (no local exec).
func invokeMCPToolSSE(ctx context.Context, serverName, toolName string, serverCfg config.MCPServerConfig, args map[string]any) (any, error) {
	if serverCfg.URL == "" {
		return nil, fmt.Errorf("MCP server %q has SSE transport but no URL configured", serverName)
	}

	transport := &mcpsdk.SSEClientTransport{
		Endpoint: serverCfg.URL,
	}

	toolset, err := mcptoolset.New(mcptoolset.Config{
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSE MCP server %q at %s: %w", serverName, serverCfg.URL, err)
	}

	// Get tools
	toolCtx := &appMCPToolContext{Context: ctx}
	tools, err := toolset.Tools(toolCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from SSE server %q: %w", serverName, err)
	}

	// Find and invoke
	var targetTool tool.Tool
	for _, t := range tools {
		if declTool, ok := t.(interface {
			Declaration() *genai.FunctionDeclaration
		}); ok {
			decl := declTool.Declaration()
			if decl != nil && decl.Name == toolName {
				targetTool = t
				break
			}
		}
	}
	if targetTool == nil {
		return nil, fmt.Errorf("tool %q not found on SSE server %q", toolName, serverName)
	}

	runner, ok := targetTool.(interface {
		Run(tool.Context, any) (map[string]any, error)
	})
	if !ok {
		return nil, fmt.Errorf("tool %q on %q does not implement Run", toolName, serverName)
	}

	slog.Debug("app MCP SSE invoke", "server", serverName, "tool", toolName, "url", serverCfg.URL)
	result, err := runner.Run(toolCtx, args)
	if err != nil {
		return nil, fmt.Errorf("SSE MCP tool %q returned error: %w", toolName, err)
	}
	return result, nil
}

// --- App MCP Container Idle Management ---
//
// App MCP containers are per-user, created on first MCP invocation, and
// DESTROYED (not just stopped) after an idle timeout. This is a lightweight
// in-memory tracker separate from the NodeClientPool (which manages chat
// session containers with stop-on-idle semantics).

var appMCPIdleTracker = &appMCPTracker{
	lastActivity: make(map[string]time.Time),
}

type appMCPTracker struct {
	mu           sync.Mutex
	lastActivity map[string]time.Time // syntheticSessionID → last use time
	started      bool
}

// touch records activity for a synthetic session ID.
func (t *appMCPTracker) touch(sessionID string) {
	t.mu.Lock()
	t.lastActivity[sessionID] = time.Now()
	t.mu.Unlock()
}

// StartIdleWatchdog starts a background goroutine that destroys app MCP
// containers after they've been idle for the given timeout.
func (t *appMCPTracker) StartIdleWatchdog(ctx context.Context, timeout time.Duration) {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return
	}
	t.started = true
	t.mu.Unlock()

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.destroyIdle(timeout)
			}
		}
	}()
}

func (t *appMCPTracker) destroyIdle(timeout time.Duration) {
	t.mu.Lock()
	now := time.Now()
	var expired []string
	for sid, lastUse := range t.lastActivity {
		if now.Sub(lastUse) > timeout {
			expired = append(expired, sid)
		}
	}
	for _, sid := range expired {
		delete(t.lastActivity, sid)
	}
	t.mu.Unlock()

	if len(expired) == 0 {
		return
	}

	// Get backend to destroy sessions (works for both Incus and K8s)
	appCfg, _ := config.LoadAppConfig()
	if appCfg == nil {
		slog.Warn("cannot destroy idle app MCP sessions: app config not available")
		return
	}
	backend, cleanup, err := sandbox.BackendFromAppConfig(appCfg)
	if err != nil {
		slog.Warn("cannot destroy idle app MCP sessions: backend unavailable", "error", err)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, sid := range expired {
		slog.Info("destroying idle app MCP session", "sessionID", sid)
		if err := backend.DestroySession(ctx, sid); err != nil {
			slog.Warn("failed to destroy idle app MCP session", "sessionID", sid, "error", err)
		}
	}
}

// appMCPToolContext is a minimal tool.Context for programmatic MCP tool invocation
// from app data requests. All optional methods return zero values — this is
// sufficient for most MCP tools which only need the embedded context.Context.
type appMCPToolContext struct {
	context.Context
}

func (c *appMCPToolContext) Actions() *session.EventActions       { return &session.EventActions{} }
func (c *appMCPToolContext) Branch() string                       { return "" }
func (c *appMCPToolContext) AgentName() string                    { return "app-data-proxy" }
func (c *appMCPToolContext) AppName() string                      { return "astonish" }
func (c *appMCPToolContext) Artifacts() agent.Artifacts           { return nil }
func (c *appMCPToolContext) FunctionCallID() string               { return "" }
func (c *appMCPToolContext) InvocationID() string                 { return "" }
func (c *appMCPToolContext) SessionID() string                    { return "" }
func (c *appMCPToolContext) UserID() string                       { return "" }
func (c *appMCPToolContext) UserContent() *genai.Content          { return nil }
func (c *appMCPToolContext) ReadonlyState() session.ReadonlyState { return nil }
func (c *appMCPToolContext) State() session.State                 { return nil }
func (c *appMCPToolContext) SearchMemory(_ context.Context, _ string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (c *appMCPToolContext) RequestConfirmation(_ string, _ any) error   { return nil }
func (c *appMCPToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }

// credentialSuffixRe matches a @credential-name suffix at the end of a URL.
// Only matches simple names (alphanumeric, dash, underscore) after the last @,
// ensuring it doesn't conflict with @ in URLs (e.g., user:pass@host).
var credentialSuffixRe = regexp.MustCompile(`@([a-zA-Z][a-zA-Z0-9_-]*)$`)

// validateHTTPURL performs a fast, pre-flight check on the URL before any
// network I/O. It rejects non-http(s) schemes and IP-literal hosts that
// resolve to private ranges. Hostnames are checked later at dial time.
func validateHTTPURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q (only http and https are allowed)", parsed.Scheme)
	}

	// If the host is an IP literal, check it immediately.
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("requests to private/internal networks are not allowed (%s)", ip)
		}
	}

	return nil
}

// extractHTTPBodyAndHeaders separates the HTTP body payload from custom headers
// in the args map. LLM-generated apps use two conventions:
//
// Structured: { headers: { "X-Custom": "v" }, body: { messages: [...] } }
//
//	→ body = args["body"], headers from args["headers"]
//
// Flat: { query: "SELECT ..." } or { query: "...", headers: { "X-Key": "v" } }
//
//	→ body = args (minus "headers"), headers from args["headers"]
//
// For non-body methods (GET, DELETE, etc.) bodyData is always nil.
func extractHTTPBodyAndHeaders(method string, args map[string]any) (bodyData any, headers map[string]string) {
	headers = make(map[string]string)

	// Extract custom headers regardless of method
	if args != nil {
		if h, ok := args["headers"].(map[string]any); ok {
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
	}

	// Only POST/PUT/PATCH carry a body
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return nil, headers
	}
	if args == nil {
		return nil, headers
	}

	if b, ok := args["body"]; ok {
		// Structured convention: { headers: {...}, body: {...} }
		return b, headers
	}

	// Flat convention: the entire args map is the body,
	// but strip "headers" so it doesn't leak into the payload.
	if _, hasHeaders := args["headers"]; hasHeaders {
		clean := make(map[string]any, len(args)-1)
		for k, v := range args {
			if k != "headers" {
				clean[k] = v
			}
		}
		return clean, headers
	}
	return args, headers
}

// resolveHTTPSource makes a server-side HTTP request.
// spec format: "<METHOD>:<url>" or "<METHOD>:<url>@<credential-name>"
// When a @credential-name suffix is present, the named credential is resolved
// from the credential store and its auth header is injected into the request.
func resolveHTTPSource(r *http.Request, spec string, args map[string]any) (any, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid HTTP source format: expected 'METHOD:url', got %q", spec)
	}
	method := strings.ToUpper(parts[0])
	url := parts[1]

	// Extract @credential-name suffix from URL if present
	var credentialName string
	if m := credentialSuffixRe.FindStringSubmatchIndex(url); m != nil {
		credentialName = url[m[2]:m[3]]
		url = url[:m[0]]
	}

	// Build the request body and extract custom headers.
	bodyData, customHeaders := extractHTTPBodyAndHeaders(method, args)

	var body io.Reader
	if bodyData != nil {
		jsonBytes, err := json.Marshal(bodyData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		body = strings.NewReader(string(jsonBytes))
	}

	httpReq, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	// Apply any custom headers from args
	for k, v := range customHeaders {
		httpReq.Header.Set(k, v)
	}

	// Resolve and inject credential (after custom headers — credential takes precedence for auth)
	if credentialName != "" {
		var credStore store.CredentialStore
		if r != nil {
			credStore = effectiveCredentialStore(r)
		}
		if credStore == nil {
			return nil, fmt.Errorf("credential store is not available — cannot resolve credential %q", credentialName)
		}
		headerKey, headerValue, err := credStore.Resolve(r.Context(), credentialName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credential %q: %w", credentialName, err)
		}
		httpReq.Header.Set(headerKey, headerValue)
	}

	// Validate the URL scheme and host before making the request.
	// The Transport's DialContext also checks resolved IPs (DNS-rebinding defence).
	if err := validateHTTPURL(url); err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: ssrfSafeTransport(),
		// Do not follow redirects to private IPs — each hop is checked
		// by the Transport's DialContext, but we also cap redirects.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Try to parse as JSON
	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Return as raw string if not valid JSON
		return string(respBody), nil
	}
	return result, nil
}

// resolveStaticSource returns static data from the app's DataSource config.
func resolveStaticSource(r *http.Request, key string, appName string) (any, error) {
	if appName == "" {
		return nil, fmt.Errorf("static source %q requires an app name", key)
	}

	app, err := loadAppFromStore(r, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load app %q: %w", appName, err)
	}

	for _, ds := range app.DataSources {
		if ds.ID == key && ds.Type == "static" {
			return ds.Config, nil
		}
	}

	return nil, fmt.Errorf("static data source %q not found in app %q", key, appName)
}

// resolveAppDataSource resolves a sourceId by looking up the app's DataSource config.
// This handles the case where sourceId is a logical name (e.g. "sales_data") and
// the actual routing info is in the saved app's definition.
func resolveAppDataSource(r *http.Request, sourceID string, args map[string]any, appName string) (any, error) {
	app, err := loadAppFromStore(r, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load app %q: %w", appName, err)
	}

	for _, ds := range app.DataSources {
		if ds.ID == sourceID {
			// Merge config args with request args (request args take precedence)
			mergedArgs := make(map[string]any)
			for k, v := range ds.Config {
				mergedArgs[k] = v
			}
			for k, v := range args {
				mergedArgs[k] = v
			}

			// Route based on the data source type
			switch ds.Type {
			case "mcp_tool":
				server, _ := ds.Config["server"].(string)
				tool, _ := ds.Config["tool"].(string)
				if server == "" || tool == "" {
					return nil, fmt.Errorf("mcp_tool data source %q missing server or tool config", sourceID)
				}
				// Remove server/tool from args — they're routing info, not tool args
				delete(mergedArgs, "server")
				delete(mergedArgs, "tool")
				return resolveMCPSource(r, server+"/"+tool, mergedArgs)

			case "http_api":
				urlStr, _ := ds.Config["url"].(string)
				method, _ := ds.Config["method"].(string)
				if urlStr == "" {
					return nil, fmt.Errorf("http_api data source %q missing url config", sourceID)
				}
				if method == "" {
					method = "GET"
				}
				return resolveHTTPSource(r, method+":"+urlStr, mergedArgs)

			case "static":
				return ds.Config, nil

			default:
				return nil, fmt.Errorf("unknown data source type %q for %q", ds.Type, sourceID)
			}
		}
	}

	return nil, fmt.Errorf("data source %q not found in app %q", sourceID, appName)
}
