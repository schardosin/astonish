package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// LazyMCPToolset presents cached MCP tools without connecting to the server.
// At startup, it creates lightweight proxy tools from cached metadata (instant).
// When a tool is actually invoked by the LLM, it lazily starts the real MCP
// server, gets the live tool, and executes it. The connection is kept alive
// for subsequent calls in the same session.
type LazyMCPToolset struct {
	serverName string
	serverCfg  config.MCPServerConfig
	entries    []cache.ToolEntry
	debugMode  bool

	// Lazy state: populated on first tool invocation
	mu        sync.Mutex
	manager   *mcp.Manager
	liveTools map[string]tool.Tool // name -> real MCP tool
	startErr  error                // cached error if server failed to start
	started   bool
}

// NewLazyMCPToolset creates a toolset that uses cached metadata for declarations
// and lazily connects to the MCP server only when a tool is actually invoked.
func NewLazyMCPToolset(serverName string, entries []cache.ToolEntry,
	serverCfg config.MCPServerConfig, debugMode bool) *LazyMCPToolset {
	return &LazyMCPToolset{
		serverName: serverName,
		serverCfg:  serverCfg,
		entries:    entries,
		debugMode:  debugMode,
	}
}

// Name returns the MCP server name.
func (l *LazyMCPToolset) Name() string {
	return l.serverName
}

// Tools returns proxy tools built from cached metadata. No MCP connection is needed.
func (l *LazyMCPToolset) Tools(_ adkagent.ReadonlyContext) ([]tool.Tool, error) {
	proxies := make([]tool.Tool, len(l.entries))
	for i := range l.entries {
		proxies[i] = &lazyProxyTool{
			toolset: l,
			entry:   &l.entries[i],
		}
	}
	return proxies, nil
}

// ensureServerStarted connects to the MCP server if not already connected.
// Thread-safe: only one goroutine will start the server.
func (l *LazyMCPToolset) ensureServerStarted(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.started {
		return l.startErr
	}

	if l.debugMode {
		fmt.Printf("[Chat Lazy] Starting MCP server '%s' on demand...\n", l.serverName)
	}

	mgr, err := mcp.NewManager()
	if err != nil {
		l.startErr = fmt.Errorf("failed to create MCP manager for '%s': %w", l.serverName, err)
		l.started = true
		return l.startErr
	}

	namedToolset, err := mgr.InitializeSingleToolset(ctx, l.serverName)
	if err != nil {
		mgr.Cleanup()
		l.startErr = fmt.Errorf("failed to start MCP server '%s': %w", l.serverName, err)
		l.started = true
		return l.startErr
	}

	// Resolve all tools from the live server
	minCtx := &minimalReadonlyContext{Context: ctx}
	liveToolList, err := namedToolset.Toolset.Tools(minCtx)
	if err != nil {
		mgr.Cleanup()
		l.startErr = fmt.Errorf("failed to get tools from MCP server '%s': %w", l.serverName, err)
		l.started = true
		return l.startErr
	}

	l.liveTools = make(map[string]tool.Tool, len(liveToolList))
	for _, t := range liveToolList {
		l.liveTools[t.Name()] = t
	}

	l.manager = mgr
	l.started = true

	if l.debugMode {
		fmt.Printf("[Chat Lazy] MCP server '%s' started: %d tools available\n", l.serverName, len(l.liveTools))
	}

	return nil
}

// getLiveTool returns the real MCP tool, starting the server if needed.
func (l *LazyMCPToolset) getLiveTool(ctx context.Context, name string) (tool.Tool, error) {
	if err := l.ensureServerStarted(ctx); err != nil {
		return nil, err
	}
	t, ok := l.liveTools[name]
	if !ok {
		return nil, fmt.Errorf("tool '%s' not found on MCP server '%s' (available: %d tools)",
			name, l.serverName, len(l.liveTools))
	}
	return t, nil
}

// Cleanup closes the MCP server connection if it was started.
func (l *LazyMCPToolset) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.manager != nil {
		l.manager.Cleanup()
		l.manager = nil
	}
}

// ToolCount returns the number of cached tools for this server.
func (l *LazyMCPToolset) ToolCount() int {
	return len(l.entries)
}

// lazyProxyTool is a lightweight proxy for a single MCP tool.
// It uses cached metadata for declarations and lazily delegates Run() to the real tool.
type lazyProxyTool struct {
	toolset *LazyMCPToolset
	entry   *cache.ToolEntry
}

func (p *lazyProxyTool) Name() string {
	return p.entry.Name
}

func (p *lazyProxyTool) Description() string {
	return p.entry.Description
}

func (p *lazyProxyTool) IsLongRunning() bool {
	return false
}

// Declaration returns a function declaration built from cached metadata.
func (p *lazyProxyTool) Declaration() *genai.FunctionDeclaration {
	decl := &genai.FunctionDeclaration{
		Name:        p.entry.Name,
		Description: p.entry.Description,
	}

	// Restore schema from cache
	if len(p.entry.InputSchema) > 0 {
		var schema any
		if err := json.Unmarshal(p.entry.InputSchema, &schema); err == nil {
			decl.ParametersJsonSchema = schema
		}
	}

	// If no cached schema, set a minimal valid object schema
	if decl.ParametersJsonSchema == nil {
		decl.ParametersJsonSchema = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	return decl
}

// ProcessRequest packs the cached tool declaration into the LLM request.
// Same approach as sanitizedTool: replicate PackTool logic to use our Declaration().
func (p *lazyProxyTool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}

	name := p.Name()
	if _, ok := req.Tools[name]; ok {
		return fmt.Errorf("duplicate tool: %q", name)
	}
	req.Tools[name] = p

	decl := p.Declaration()
	if decl == nil {
		return nil
	}

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}

	var funcTool *genai.Tool
	for _, gt := range req.Config.Tools {
		if gt != nil && gt.FunctionDeclarations != nil {
			funcTool = gt
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{decl},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, decl)
	}

	return nil
}

// Run lazily starts the MCP server (if not already started) and delegates to the real tool.
func (p *lazyProxyTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	liveTool, err := p.toolset.getLiveTool(ctx, p.entry.Name)
	if err != nil {
		return nil, err
	}
	// The live MCP tool implements Run via the FunctionTool interface
	runner, ok := liveTool.(interface {
		Run(tool.Context, any) (map[string]any, error)
	})
	if !ok {
		return nil, fmt.Errorf("live tool '%s' does not implement Run", p.entry.Name)
	}
	return runner.Run(ctx, args)
}
