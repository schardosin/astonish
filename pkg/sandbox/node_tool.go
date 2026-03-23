package sandbox

import (
	"encoding/json"
	"fmt"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ToolWithDeclaration is the interface for tools that can declare their schema.
// Matches ADK's internal toolinternal.FunctionTool.Declaration() method.
type ToolWithDeclaration interface {
	Declaration() *genai.FunctionDeclaration
}

// NodeTool wraps an existing tool.Tool and proxies its Run() method to a
// LazyNodeClient (astonish node inside a container). It preserves the original
// tool's Name(), Description(), Declaration(), and IsLongRunning() so the
// ADK sees it as a normal tool.
//
// On the first Run() call, the LazyNodeClient creates the session container
// and starts the astonish node process. The session ID is obtained from
// tool.Context.SessionID().
//
// This follows the same structural interface pattern as ADK's mcpTool and
// Astonish's ProtectedTool/sanitizedTool — it satisfies FunctionTool and
// RequestProcessor by implementing the methods directly.
type NodeTool struct {
	tool.Tool // Embed the original tool for Name(), Description(), IsLongRunning()

	pool       *NodeClientPool // for chat sessions (per-session containers)
	lazyClient *LazyNodeClient // for fleet sessions (dedicated per-session client)
}

// NewNodeTool creates a NodeTool that proxies the given tool through a pool.
// Used by chat/Studio sessions where each session gets its own container.
func NewNodeTool(original tool.Tool, pool *NodeClientPool) *NodeTool {
	return &NodeTool{
		Tool: original,
		pool: pool,
	}
}

// NewNodeToolWithClient creates a NodeTool that proxies through a single
// LazyNodeClient. Used by fleet sessions where one dedicated container
// is created per fleet session.
func NewNodeToolWithClient(original tool.Tool, client *LazyNodeClient) *NodeTool {
	return &NodeTool{
		Tool:       original,
		lazyClient: client,
	}
}

// Declaration returns the original tool's function declaration (schema).
// This is required by ADK's FunctionTool interface.
func (nt *NodeTool) Declaration() *genai.FunctionDeclaration {
	if dt, ok := nt.Tool.(ToolWithDeclaration); ok {
		return dt.Declaration()
	}
	return nil
}

// ProcessRequest packs the tool declaration into the LLM request.
// This replicates ADK's internal toolutils.PackTool logic since that
// package is internal and cannot be imported directly.
//
// Additionally, it eagerly triggers container creation via BindSession.
// ProcessRequest is called before the LLM call, so the container starts
// cloning in the background while the LLM generates its response. By the
// time the first tool call arrives, the container is likely already running.
func (nt *NodeTool) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	// Eagerly bind the session — starts container creation in background.
	// Idempotent: only the first call per session triggers init.
	if ctx != nil {
		if sessionID := ctx.SessionID(); sessionID != "" {
			client := nt.getClient(sessionID)
			if client != nil {
				client.BindSession(sessionID)
			}
		}
	}

	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}

	name := nt.Name()
	if _, ok := req.Tools[name]; ok {
		return fmt.Errorf("duplicate tool: %q", name)
	}
	req.Tools[name] = nt

	decl := nt.Declaration()
	if decl == nil {
		return nil
	}

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}

	// Find an existing genai.Tool with FunctionDeclarations
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

// Run proxies the tool call to the container's astonish node process.
// On the first call, the LazyNodeClient creates the session container and
// starts the node. The session ID is extracted from tool.Context.SessionID().
func (nt *NodeTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	// Extract session ID from ADK context for container routing
	var sessionID string
	if ctx != nil {
		sessionID = ctx.SessionID()
	}
	if sessionID == "" {
		// No session ID — fall back to running on the original tool locally
		if runner, ok := nt.Tool.(interface {
			Run(tool.Context, any) (map[string]any, error)
		}); ok {
			return runner.Run(ctx, args)
		}
		return nil, fmt.Errorf("tool %q: no session ID and no local fallback", nt.Name())
	}

	client := nt.getClient(sessionID)
	if client == nil {
		return nil, fmt.Errorf("tool %q: no sandbox client available for session %s", nt.Name(), sessionID)
	}

	// Convert args to map[string]interface{}
	argsMap, ok := args.(map[string]interface{})
	if !ok {
		// Try JSON round-trip for struct args
		data, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal args: %w", err)
		}
		argsMap = make(map[string]interface{})
		if err := json.Unmarshal(data, &argsMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal args to map: %w", err)
		}
	}

	// Send to node (lazy init on first call)
	rawResult, err := client.Call(sessionID, nt.Name(), argsMap)
	if err != nil {
		return nil, err
	}

	// Unmarshal the result
	var result map[string]any
	if rawResult != nil {
		if err := json.Unmarshal(rawResult, &result); err != nil {
			// If it's not a map, wrap it
			return map[string]any{"result": json.RawMessage(rawResult)}, nil
		}
	}

	return result, nil
}

// getClient returns the LazyNodeClient for the given session. If the NodeTool
// was created with a pool (chat sessions), it gets or creates a client from
// the pool. If created with a direct client (fleet sessions), returns that.
func (nt *NodeTool) getClient(sessionID string) *LazyNodeClient {
	if nt.lazyClient != nil {
		return nt.lazyClient
	}
	if nt.pool != nil {
		return nt.pool.GetOrCreate(sessionID)
	}
	return nil
}

// containerTools lists tool names that should execute inside the container.
// Tools NOT in this set run on the host (memory, credentials, scheduler, etc.).
var containerTools = map[string]bool{
	"read_file":                 true,
	"write_file":                true,
	"edit_file":                 true,
	"file_tree":                 true,
	"grep_search":               true,
	"find_files":                true,
	"shell_command":             true,
	"process_read":              true,
	"process_write":             true,
	"process_list":              true,
	"process_kill":              true,
	"http_request":              true,
	"web_fetch":                 true,
	"read_pdf":                  true,
	"filter_json":               true,
	"git_diff_add_line_numbers": true,
	"opencode":                  true,
}

// WrapToolsWithNode wraps tools with NodeTool proxies using a pool, selectively.
// Only tools listed in containerTools are wrapped — they run inside the container.
// Host-side tools (memory, credentials, scheduler, delegate_tasks, browser,
// email, etc.) pass through unwrapped and continue to run on the host.
// Used by chat/Studio sessions where each session gets its own container.
func WrapToolsWithNode(tools []tool.Tool, pool *NodeClientPool) []tool.Tool {
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		if containerTools[t.Name()] {
			wrapped[i] = NewNodeTool(t, pool)
		} else {
			wrapped[i] = t // pass through unwrapped
		}
	}
	return wrapped
}

// WrapToolsWithNodeClient wraps tools with NodeTool proxies using a single
// LazyNodeClient, selectively. Same filtering as WrapToolsWithNode but uses
// a dedicated client instead of a pool. Used by fleet sessions where each
// session has its own LazyNodeClient.
func WrapToolsWithNodeClient(tools []tool.Tool, lazyClient *LazyNodeClient) []tool.Tool {
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		if containerTools[t.Name()] {
			wrapped[i] = NewNodeToolWithClient(t, lazyClient)
		} else {
			wrapped[i] = t // pass through unwrapped
		}
	}
	return wrapped
}
