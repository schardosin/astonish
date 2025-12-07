package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// AIChatRequest is the request body for AI chat
type AIChatRequest struct {
	Message       string   `json:"message"`
	Context       string   `json:"context"`       // "create_flow" | "modify_nodes" | "node_config"
	CurrentYAML   string   `json:"currentYaml"`   // Current flow state
	SelectedNodes []string `json:"selectedNodes"` // For node operations
	History       []ChatMessage `json:"history"`  // Conversation history
}

// ChatMessage represents a message in the conversation
type ChatMessage struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"`
}

// AIChatResponse is the response from AI chat
type AIChatResponse struct {
	Message      string `json:"message"`      // AI response text
	ProposedYAML string `json:"proposedYaml"` // Generated/modified YAML (if any)
	Action       string `json:"action"`       // "preview" | "apply" | "info"
	Error        string `json:"error,omitempty"`
}

// getSystemPrompt returns the system prompt based on context
func getSystemPrompt(ctx string, availableTools []ToolInfo) string {
	// Build tools list
	var toolsList strings.Builder
	for _, t := range availableTools {
		toolsList.WriteString("- " + t.Name + ": " + t.Description + " (source: " + t.Source + ")\n")
	}
	
	basePrompt := GetFlowSchema() + "\n\n# Available Tools\nONLY use tools from this list. Do NOT invent or hallucinate tool names.\n" + toolsList.String()
	
	switch ctx {
	case "create_flow":
		return basePrompt + `

# Your Task
You are an AI assistant helping users create agent workflows.
When the user describes what they want, generate a COMPLETE and VALID YAML flow.

CRITICAL TOOL RULES:
- ONLY use tools from the "Available Tools" list above
- For LLM nodes that need tools, you MUST set "tools: true"
- Use tools_selection to limit which tools are available to that node

TOOL TIPS:
- shell_command: Can run any shell command including curl for HTTP requests/web APIs
  Example: Use curl to call search APIs, fetch web pages, etc.
- write_file: Save content to files
- read_file: Read file contents
- run_python_code: Execute Python for complex logic
- filter_json: Extract specific fields from JSON data

If the user needs web search, suggest using shell_command with curl to call a search API,
or explain that they need an MCP server with search capabilities.

Response format:
1. Brief explanation of the flow you're creating
2. The complete YAML wrapped in ` + "```yaml" + ` code blocks

Be concise but ensure the YAML is complete and valid.`

	case "modify_nodes":
		return basePrompt + `

# Your Task
You are an AI assistant helping users modify existing agent workflows.
The user has selected specific nodes and wants to make changes.

Response format:
1. Brief explanation of the changes
2. The MODIFIED YAML (full flow) wrapped in ` + "```yaml" + ` code blocks

Preserve existing nodes unless explicitly asked to change them.`

	case "node_config":
		return basePrompt + `

# Your Task
You are an AI assistant helping users optimize a specific node.
Help them improve prompts, select appropriate tools, and configure the node correctly.

Response format:
1. Suggestions for improvement
2. If changing the node, provide the updated node YAML wrapped in ` + "```yaml" + ` code blocks`

	default:
		return basePrompt + `

# Your Task
You are an AI assistant helping users design agent workflows.
Answer questions about flow design, suggest improvements, and help with configuration.
When providing YAML, wrap it in ` + "```yaml" + ` code blocks.`
	}
}

// buildConversationHistory converts chat history to LLM format
func buildConversationHistory(history []ChatMessage) []*genai.Content {
	var contents []*genai.Content
	for _, msg := range history {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role: role,
			Parts: []*genai.Part{
				genai.NewPartFromText(msg.Content),
			},
		})
	}
	return contents
}

// extractYAML extracts YAML from markdown code blocks
func extractYAML(text string) string {
	// Look for ```yaml ... ``` blocks
	startMarker := "```yaml"
	endMarker := "```"
	
	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return ""
	}
	
	startIdx += len(startMarker)
	remaining := text[startIdx:]
	
	endIdx := strings.Index(remaining, endMarker)
	if endIdx == -1 {
		return ""
	}
	
	return strings.TrimSpace(remaining[:endIdx])
}

// AIChatHandler handles AI chat requests
func AIChatHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Parse request
	var req AIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	
	// Load app config
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		json.NewEncoder(w).Encode(AIChatResponse{
			Error: "Failed to load config: " + err.Error(),
		})
		return
	}
	
	// Get default provider and model
	// Note: Provider env vars are set up at studio startup in cmd/astonish/studio.go
	providerName := appCfg.General.DefaultProvider
	modelName := appCfg.General.DefaultModel
	
	if providerName == "" {
		providerName = "gemini" // fallback
	}
	if modelName == "" {
		modelName = "gemini-2.0-flash" // fallback
	}
	
	// Create LLM client
	llm, err := provider.GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		json.NewEncoder(w).Encode(AIChatResponse{
			Error: "Failed to create LLM client: " + err.Error(),
		})
		return
	}
	
	// Get available tools for context
	availableTools := getAvailableTools(ctx)
	
	// Build system prompt
	systemPrompt := getSystemPrompt(req.Context, availableTools)
	
	// Add current YAML context if provided
	if req.CurrentYAML != "" {
		systemPrompt += "\n\n# Current Flow YAML\n```yaml\n" + req.CurrentYAML + "\n```"
	}
	
	// Add selected nodes context if provided
	if len(req.SelectedNodes) > 0 {
		systemPrompt += "\n\n# Selected Nodes\n" + strings.Join(req.SelectedNodes, ", ")
	}
	
	// Build conversation
	history := buildConversationHistory(req.History)
	
	// Add current user message
	history = append(history, &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			genai.NewPartFromText(req.Message),
		},
	})
	
	// Create LLM request
	llmReq := &model.LLMRequest{
		Contents: history,
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					genai.NewPartFromText(systemPrompt),
				},
			},
			Temperature: genai.Ptr(float32(0.7)),
		},
	}
	
	// Call LLM with validation retry loop (max 3 attempts)
	const maxRetries = 3
	var fullResponse string
	var proposedYAML string
	var lastValidationErrors []string
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Call LLM
		var responseText strings.Builder
		for resp, err := range llm.GenerateContent(ctx, llmReq, false) {
			if err != nil {
				json.NewEncoder(w).Encode(AIChatResponse{
					Error: "LLM error: " + err.Error(),
				})
				return
			}
			if resp != nil && resp.Content != nil {
				for _, part := range resp.Content.Parts {
					if part.Text != "" {
						responseText.WriteString(part.Text)
					}
				}
			}
		}
		
		fullResponse = responseText.String()
		proposedYAML = extractYAML(fullResponse)
		
		// If no YAML was generated, no need to validate
		if proposedYAML == "" {
			break
		}
		
		// Validate the YAML
		validation := ValidateFlowYAML(proposedYAML, availableTools)
		if validation.Valid {
			// YAML is valid, we're done
			break
		}
		
		// YAML has errors
		lastValidationErrors = validation.Errors
		
		// If this was the last attempt, break and return with errors
		if attempt == maxRetries {
			break
		}
		
		// Add the assistant's response and validation error to history for retry
		llmReq.Contents = append(llmReq.Contents, &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				genai.NewPartFromText(fullResponse),
			},
		})
		llmReq.Contents = append(llmReq.Contents, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				genai.NewPartFromText(FormatValidationErrors(validation.Errors)),
			},
		})
	}
	
	// Determine action based on whether YAML was generated and valid
	action := "info"
	if proposedYAML != "" {
		if len(lastValidationErrors) > 0 {
			// YAML has validation errors after all retries
			action = "preview"
			fullResponse += "\n\n⚠️ **Validation Warnings** (after " + fmt.Sprintf("%d", maxRetries) + " attempts):\n"
			for _, err := range lastValidationErrors {
				fullResponse += "- " + err + "\n"
			}
		} else {
			action = "preview"
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AIChatResponse{
		Message:      fullResponse,
		ProposedYAML: proposedYAML,
		Action:       action,
	})
}

// getAvailableTools fetches tools for AI context from cache
func getAvailableTools(ctx context.Context) []ToolInfo {
	// Use cached tools (initialized at startup)
	cached := GetCachedTools()
	if cached != nil {
		return cached
	}
	
	// Fallback to internal tools only if cache not ready
	var allTools []ToolInfo
	internalTools, _ := tools.GetInternalTools()
	for _, t := range internalTools {
		allTools = append(allTools, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Source:      "internal",
		})
	}
	return allTools
}
