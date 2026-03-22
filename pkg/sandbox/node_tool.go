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

	lazyClient *LazyNodeClient
}

// NewNodeTool creates a NodeTool that proxies the given tool through a lazy node client.
func NewNodeTool(original tool.Tool, lazyClient *LazyNodeClient) *NodeTool {
	return &NodeTool{
		Tool:       original,
		lazyClient: lazyClient,
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
			nt.lazyClient.BindSession(sessionID)
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
	rawResult, err := nt.lazyClient.Call(sessionID, nt.Name(), argsMap)
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

// WrapToolsWithNode wraps a slice of tools with NodeTool proxies.
// Each wrapped tool delegates its Run() to the given LazyNodeClient while
// preserving the original tool's identity (name, description, schema).
func WrapToolsWithNode(tools []tool.Tool, lazyClient *LazyNodeClient) []tool.Tool {
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		wrapped[i] = NewNodeTool(t, lazyClient)
	}
	return wrapped
}
