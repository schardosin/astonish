package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// AIChatRequest is the request body for AI chat
type AIChatRequest struct {
	Message       string        `json:"message"`
	Context       string        `json:"context"`       // "create_flow" | "modify_nodes" | "node_config"
	CurrentYAML   string        `json:"currentYaml"`   // Current flow state
	SelectedNodes []string      `json:"selectedNodes"` // For node operations
	History       []ChatMessage `json:"history"`       // Conversation history
}

// ChatMessage represents a message in the conversation
type ChatMessage struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// AIChatResponse is the response from AI chat
type AIChatResponse struct {
	Message      string `json:"message"`      // AI response text
	ProposedYAML string `json:"proposedYaml"` // Generated/modified YAML (if any)
	Action       string `json:"action"`       // "preview" | "apply" | "info"
	Error        string `json:"error,omitempty"`
}

// IntentClassifyRequest is the request for intent classification
type IntentClassifyRequest struct {
	Message string   `json:"message"`
	Tools   []string `json:"tools"` // List of installed tool names
}

// IntentClassifyResponse is the response for intent classification
type IntentClassifyResponse struct {
	Intent      string  `json:"intent"`      // "create_flow" | "install_mcp" | "browse_mcp_store" | "search_mcp_internet" | "general_question"
	Requirement string  `json:"requirement"` // Extracted tool/search requirement (for install/search intents)
	Confidence  float32 `json:"confidence"`  // 0.0-1.0 confidence score
	Error       string  `json:"error,omitempty"`
}

// IntentClassifyHandler handles POST /api/ai/classify-intent
func IntentClassifyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req IntentClassifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IntentClassifyResponse{
			Intent:     "general_question",
			Confidence: 0.5,
		})
		return
	}

	// Get LLM provider
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IntentClassifyResponse{
			Error: "Failed to load config: " + err.Error(),
		})
		return
	}
	injectProviderSecrets(appCfg)

	providerName := appCfg.General.DefaultProvider
	modelName := appCfg.General.DefaultModel
	if providerName == "" {
		providerName = "gemini"
	}
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}

	llm, err := provider.GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IntentClassifyResponse{
			Error: "Failed to get LLM provider: " + err.Error(),
		})
		return
	}

	// Intent classification prompt
	toolsContext := ""
	if len(req.Tools) > 0 {
		toolsContext = fmt.Sprintf("\nCURRENTLY INSTALLED TOOLS: %s\nCheck this list! If the user wants to use/switch to a tool that is ALREADY INSTALLED, classify as 'create_flow' (to modify the flow), NOT 'install_mcp'.\n", strings.Join(req.Tools, ", "))
	}

	classifyPrompt := fmt.Sprintf(`Classify the user's intent. Respond with ONLY a JSON object, no other text.

User message: "%s"
%s
Classify into ONE of these intents:
- "create_flow": User wants to CREATE, BUILD, DESIGN, or MODIFY an agent workflow/flow
- "install_mcp": User wants to INSTALL, ADD, or GET a specific MCP server/tool
- "browse_mcp_store": User wants to BROWSE, LIST, or SEE what tools are available
- "search_mcp_internet": User wants to SEARCH the internet/web for MCP servers
- "extract_mcp_url": User provides a GitHub/NPM URL to install/extract an MCP server from (e.g. "install from https://github.com/...")
- "general_question": Questions about Astonish, flows, or how things work

IMPORTANT DISTINCTION:
- "create a flow that uses GitHub mcp" → create_flow (they want to CREATE a flow)
- "install the GitHub mcp server" → install_mcp (they want to INSTALL a tool)
- "find ui5 mcp server" → install_mcp (they want to FIND and likely install a tool)
- "use https://github.com/foo/bar" → extract_mcp_url (URL provided)

If install_mcp, search_mcp_internet, or extract_mcp_url, extract the tool name or URL as requirement.

Response format:
{"intent": "...", "requirement": "...", "confidence": 0.95}

For create_flow or general_question, requirement should be empty string.`, req.Message, toolsContext)

	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(classifyPrompt),
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.1)), // Low temperature for classification
		},
	}

	var responseText strings.Builder
	for resp, err := range llm.GenerateContent(ctx, llmReq, true) {
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(IntentClassifyResponse{
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

	response := strings.TrimSpace(responseText.String())

	// Parse JSON response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		// Fallback if JSON extraction fails
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IntentClassifyResponse{
			Intent:     "general_question",
			Confidence: 0.5,
		})
		return
	}
	jsonStr := response[jsonStart : jsonEnd+1]

	var result IntentClassifyResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback if parsing fails
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IntentClassifyResponse{
			Intent:     "general_question",
			Confidence: 0.5,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
