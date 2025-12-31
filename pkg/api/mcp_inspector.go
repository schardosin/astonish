package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ToolSchema represents a tool's parameter schema
type ToolSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// ListServerToolsResponse is the response for GET /api/mcp/{serverName}/tools
type ListServerToolsResponse struct {
	Tools []ToolSchema `json:"tools"`
	Error string       `json:"error,omitempty"`
}

// ToolRunRequest is the request for POST /api/mcp/{serverName}/tools/{toolName}/run
type ToolRunRequest struct {
	Params map[string]interface{} `json:"params"`
}

// ToolRunResponse is the response for POST /api/mcp/{serverName}/tools/{toolName}/run
type ToolRunResponse struct {
	Success   bool        `json:"success"`
	Result    interface{} `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	TimeTaken string      `json:"time_taken"`
}

// ListServerToolsHandler handles GET /api/mcp/{serverName}/tools
// Lists all tools available from a specific MCP server
func ListServerToolsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverName := vars["serverName"]

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Create a new manager and initialize just this server
	mcpManager, err := mcp.NewManager()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create MCP manager: %v", err), http.StatusInternalServerError)
		return
	}
	defer mcpManager.Cleanup()

	namedToolset, err := mcpManager.InitializeSingleToolset(ctx, serverName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListServerToolsResponse{
			Error: err.Error(),
		})
		return
	}

	// Get tools from the toolset
	minimalCtx := &minimalReadonlyContext{Context: ctx}
	toolsList, err := namedToolset.Toolset.Tools(minimalCtx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListServerToolsResponse{
			Error: fmt.Sprintf("Failed to list tools: %v", err),
		})
		return
	}

	// Convert to our response format
	var tools []ToolSchema
	
	// Interface for tools that have declarations
	type ToolWithDeclaration interface {
		Declaration() *genai.FunctionDeclaration
	}
	
	for _, t := range toolsList {
		schema := ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
		}
		
		// Try to get the parameter schema from Declaration
		if declTool, ok := t.(ToolWithDeclaration); ok {
			decl := declTool.Declaration()
			if decl != nil && decl.ParametersJsonSchema != nil {
				// Convert genai.Schema to a map for JSON serialization
				if genaiSchema, ok := decl.ParametersJsonSchema.(*genai.Schema); ok {
					schema.Parameters = convertGenaiSchemaToMap(genaiSchema)
				} else if mapSchema, ok := decl.ParametersJsonSchema.(map[string]interface{}); ok {
					schema.Parameters = mapSchema
				} else {
					// Try to use it directly
					schema.Parameters = decl.ParametersJsonSchema
				}
			}
		}
		
		tools = append(tools, schema)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListServerToolsResponse{
		Tools: tools,
	})
}

// RunServerToolHandler handles POST /api/mcp/{serverName}/tools/{toolName}/run
// Executes a specific tool with provided parameters
func RunServerToolHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverName := vars["serverName"]
	toolName := vars["toolName"]

	// Parse request body
	var req ToolRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolRunResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// Set a timeout for tool execution
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	startTime := time.Now()

	// Create a new manager and initialize just this server
	mcpManager, err := mcp.NewManager()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolRunResponse{
			Success:   false,
			Error:     fmt.Sprintf("Failed to create MCP manager: %v", err),
			TimeTaken: time.Since(startTime).String(),
		})
		return
	}
	defer mcpManager.Cleanup()

	namedToolset, err := mcpManager.InitializeSingleToolset(ctx, serverName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolRunResponse{
			Success:   false,
			Error:     err.Error(),
			TimeTaken: time.Since(startTime).String(),
		})
		return
	}

	// Get the specific tool
	minimalCtx := &minimalReadonlyContext{Context: ctx}
	toolsList, err := namedToolset.Toolset.Tools(minimalCtx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolRunResponse{
			Success:   false,
			Error:     fmt.Sprintf("Failed to list tools: %v", err),
			TimeTaken: time.Since(startTime).String(),
		})
		return
	}

	// Find the requested tool and execute it
	var result map[string]any
	var runErr error
	toolFound := false
	
	for _, t := range toolsList {
		if t.Name() == toolName {
			toolFound = true
			
			// Try to call Run using the runnableTool interface
			type runnableTool interface {
				Run(tool.Context, any) (map[string]any, error)
			}
			
			if runnable, ok := t.(runnableTool); ok {
				// Create a minimal tool context for execution
				toolCtx := &minimalToolContext{Context: ctx}
				result, runErr = runnable.Run(toolCtx, req.Params)
			} else {
				runErr = fmt.Errorf("tool '%s' does not implement Run method", toolName)
			}
			break
		}
	}

	if !toolFound {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolRunResponse{
			Success:   false,
			Error:     fmt.Sprintf("Tool '%s' not found in server '%s'", toolName, serverName),
			TimeTaken: time.Since(startTime).String(),
		})
		return
	}

	if runErr != nil {
		log.Printf("Tool execution error for %s/%s: %v", serverName, toolName, runErr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolRunResponse{
			Success:   false,
			Error:     runErr.Error(),
			TimeTaken: time.Since(startTime).String(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToolRunResponse{
		Success:   true,
		Result:    result,
		TimeTaken: time.Since(startTime).String(),
	})
}

// convertGenaiSchemaToMap converts a genai.Schema to a JSON-serializable map
func convertGenaiSchemaToMap(schema *genai.Schema) map[string]interface{} {
	if schema == nil {
		return nil
	}
	
	result := make(map[string]interface{})
	
	if schema.Type != "" {
		result["type"] = string(schema.Type)
	}
	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	
	// Convert properties
	if len(schema.Properties) > 0 {
		props := make(map[string]interface{})
		for name, propSchema := range schema.Properties {
			props[name] = convertGenaiSchemaToMap(propSchema)
		}
		result["properties"] = props
	}
	
	// Handle enum values
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}
	
	// Handle items for arrays
	if schema.Items != nil {
		result["items"] = convertGenaiSchemaToMap(schema.Items)
	}
	
	return result
}
