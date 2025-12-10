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
	"gopkg.in/yaml.v3"
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

## BEFORE GENERATING YAML, THINK THROUGH THESE STEPS:

### Step 1: Understand the Goal
- What does the user want to achieve?
- What is the main purpose of this flow?

### Step 2: Design Minimal Flow
- What is the MINIMUM number of nodes needed?
- Avoid unnecessary nodes (no separate "check_exit" nodes - use input with options instead)
- A simple Q&A needs only: input → llm → (optional: input to continue) → loop

### Step 3: Tool Requirements Analysis (CRITICAL!)
For EACH tool you plan to use:
1. Look at the tool's required parameters
2. Ask: "Do I have this data already, or do I need to collect it from the user?"
3. If data is missing, add an INPUT node BEFORE the tool node to gather it

Examples:
- "list_pull_requests" needs: owner, repo → Add input node asking for repository (or use state from earlier)
- "get_pull_request" needs: owner, repo, pullNumber → Make sure pr selection happened first
- If user said "my PRs", you still need to know WHICH repo → ask via input node

### Step 4: User Experience Check
- Will the output be visible to the user? (add user_message to LLM nodes!)
- Are the prompts clear and friendly?
- Is the flow intuitive to use?

### Step 5: Validate Design
- Any redundant nodes that can be removed?
- Are conditional edges based on INPUT options (reliable) not LLM output (unreliable)?
- Does every LLM response the user should see have user_message?

## IMPORTANT: Judge the Request Type

**ACTION REQUEST** (user wants to create/modify something):
→ Follow the planning steps above and generate YAML

**QUESTION** (user is asking about the flow, asking for info, or chatting):
→ Just answer conversationally. Do NOT generate YAML.

Examples of QUESTIONS (no YAML needed):
- "What is the name of this flow?"
- "How many nodes does it have?"
- "Can you explain what this does?"
- "What tools are available?"

Examples of ACTION REQUESTS (generate YAML):
- "Create a Q&A chatbot"
- "Add a node that summarizes"
- "Make it loop until the user says stop"
- "Change the prompt to be friendlier"

## RESPONSE FORMAT (for action requests only):
1. **Brief explanation** of your design decisions (1-2 sentences)
2. The **complete YAML** wrapped in ` + "```yaml" + ` code blocks

CRITICAL TOOL RULES:
- ONLY use tools from the "Available Tools" list above
- For LLM nodes that need tools, you MUST set "tools: true"
- Use tools_selection to limit which tools are available to that node

Be concise. Focus on simplicity and good UX.`

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
You are an AI assistant helping users optimize a SINGLE node's configuration.
The user is editing a specific node and wants help improving it.

## What You Can Help With:
- **LLM nodes**: Improve system prompt, prompt phrasing, add user_message for output
- **Input nodes**: Better prompt wording, appropriate options for choices
- **Tool nodes**: Suggest which tools to use, configure args correctly
- **Output nodes**: Format user_message for clarity

## Guidelines:
- Always suggest adding user_message if the user should see the result
- Recommend specific tools from the Available Tools list
- Keep prompts clear and concise
- Suggest output_model fields if the data should be used by later nodes

## RESPONSE FORMAT (IMPORTANT - follow exactly):
1. A brief, positive explanation of your improvements (1-2 sentences)
2. The improved node YAML wrapped in ` + "```yaml" + ` code blocks

**YOU MUST ONLY RETURN THE SINGLE NODE** - starting with "- name:".
DO NOT apologize or mention needing the full flow.
DO NOT return the complete flow YAML.
The system automatically merges your node into the existing flow.

Example good response:
"I've improved the system prompt to be clearer and added user_message so the response is shown to the user."
` + "```yaml" + `
- name: answer_question
  type: llm
  system: "You are a helpful assistant..."
  prompt: "{question}"
  output_model:
    answer: str
  user_message:
    - answer
` + "```" + "`"

	case "multi_node":
		return basePrompt + `

# Your Task
You are an AI assistant helping users modify SELECTED NODES in their flow.
The user has selected specific nodes and wants to make targeted changes.

## SCOPE RULES (CRITICAL!):
- You can MODIFY the selected nodes
- You can ADD NEW NODES between the selected nodes
- You must NOT modify nodes that are NOT selected
- You must keep the rest of the flow exactly as provided

## What You Can Help With:
- Improve prompts in selected nodes
- Add new nodes between selected ones (e.g., add confirmation, validation)
- Refactor selected nodes
- Split a node into multiple nodes

## RESPONSE FORMAT:
1. Brief explanation of your changes (1-2 sentences)
2. Return the **COMPLETE FLOW YAML** wrapped in ` + "```yaml" + ` code blocks

**CRITICAL: Return the ENTIRE flow YAML** - including nodes, flow, and all other sections.
Only the selected nodes should be modified. All other nodes must remain exactly the same.
The system will replace the entire flow with your response.

When adding new nodes between selected ones:
1. Add the new node(s) in the nodes section
2. Update the flow section to connect them properly`

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

// isNodeSnippet checks if the YAML is just node(s) (not a full flow)
// Handles: "- name:", "name:", or multi-node snippets separated by ---
func isNodeSnippet(yamlContent string) bool {
	trimmed := strings.TrimSpace(yamlContent)
	// Check for YAML array format (- name:)
	if strings.HasPrefix(trimmed, "- name:") {
		return true
	}
	// Check for single node without dash (name:)
	if strings.HasPrefix(trimmed, "name:") {
		return true
	}
	// Not a node snippet - has other flow elements
	return false
}

// mergeNodeIntoFlow merges one or more node snippet(s) into the full flow YAML
// selectedNodeNames is the list of originally selected nodes that should be replaced
func mergeNodeIntoFlow(nodeSnippet, fullFlowYAML string, selectedNodeNames []string) (string, error) {
	// Handle multi-document YAML (nodes separated by ---)
	// Convert to single array format
	nodeSnippet = strings.TrimSpace(nodeSnippet)
	
	// If snippet starts with "name:" (no dash), add the dash
	if strings.HasPrefix(nodeSnippet, "name:") {
		nodeSnippet = "- " + nodeSnippet
	}
	
	// Replace various forms of "---" separator with proper YAML array notation
	// Handle: "---\nname:", "---\n\nname:", "---\n- name:", "---\n\n- name:"
	nodeSnippet = strings.ReplaceAll(nodeSnippet, "---\n- name:", "\n- name:")
	nodeSnippet = strings.ReplaceAll(nodeSnippet, "---\n\n- name:", "\n- name:")
	nodeSnippet = strings.ReplaceAll(nodeSnippet, "---\nname:", "\n- name:")
	nodeSnippet = strings.ReplaceAll(nodeSnippet, "---\n\nname:", "\n- name:")
	// Also handle case where --- is at the beginning
	nodeSnippet = strings.TrimPrefix(nodeSnippet, "---\n")
	nodeSnippet = strings.TrimPrefix(nodeSnippet, "---")
	nodeSnippet = strings.TrimSpace(nodeSnippet)
	
	// If after trimming it now starts with "name:", add the dash
	if strings.HasPrefix(nodeSnippet, "name:") {
		nodeSnippet = "- " + nodeSnippet
	}
	
	// Parse the node snippet(s) - should now be a YAML array
	var newNodes []map[string]interface{}
	if err := yaml.Unmarshal([]byte(nodeSnippet), &newNodes); err != nil {
		return "", fmt.Errorf("failed to parse node snippet: %w (snippet: %s)", err, nodeSnippet[:min(100, len(nodeSnippet))])
	}
	if len(newNodes) == 0 {
		return "", fmt.Errorf("no nodes found in snippet")
	}
	
	// Parse the full flow
	var flow map[string]interface{}
	if err := yaml.Unmarshal([]byte(fullFlowYAML), &flow); err != nil {
		return "", fmt.Errorf("failed to parse flow: %w", err)
	}
	
	// Get the nodes section
	flowNodes, ok := flow["nodes"].([]interface{})
	if !ok {
		return "", fmt.Errorf("nodes section not found in flow")
	}
	
	// Create a set of selected node names for quick lookup
	selectedSet := make(map[string]bool)
	for _, name := range selectedNodeNames {
		selectedSet[name] = true
	}
	
	// If no selected nodes specified, use simple update/append logic (backward compatibility)
	if len(selectedNodeNames) == 0 {
		for _, snippetNode := range newNodes {
			nodeName, ok := snippetNode["name"].(string)
			if !ok {
				continue
			}
			
			nodeFound := false
			for i, n := range flowNodes {
				nodeMap, ok := n.(map[string]interface{})
				if !ok {
					continue
				}
				if nodeMap["name"] == nodeName {
					flowNodes[i] = snippetNode
					nodeFound = true
					break
				}
			}
			
			if !nodeFound {
				flowNodes = append(flowNodes, snippetNode)
			}
		}
		
		flow["nodes"] = flowNodes
		result, err := yaml.Marshal(flow)
		if err != nil {
			return "", fmt.Errorf("failed to marshal updated flow: %w", err)
		}
		return string(result), nil
	}
	
	// Scoped replacement: find insertion position (index of first selected node)
	insertPosition := -1
	for i, n := range flowNodes {
		nodeMap, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		nodeName, _ := nodeMap["name"].(string)
		if selectedSet[nodeName] && insertPosition == -1 {
			insertPosition = i
			break
		}
	}
	
	// If we couldn't find the insertion position, append to end
	if insertPosition == -1 {
		insertPosition = len(flowNodes)
	}
	
	// Build new nodes list: keep non-selected nodes, insert new nodes at position
	var updatedNodes []interface{}
	insertedNewNodes := false
	
	for i, n := range flowNodes {
		nodeMap, ok := n.(map[string]interface{})
		if !ok {
			updatedNodes = append(updatedNodes, n)
			continue
		}
		nodeName, _ := nodeMap["name"].(string)
		
		if selectedSet[nodeName] {
			// This is a selected node - skip it (will be replaced)
			// Insert new nodes at the position of the first selected node
			if !insertedNewNodes && i == insertPosition {
				for _, newNode := range newNodes {
					updatedNodes = append(updatedNodes, newNode)
				}
				insertedNewNodes = true
			}
			continue
		}
		
		updatedNodes = append(updatedNodes, n)
	}
	
	// If we didn't insert yet (no selected nodes found), append new nodes
	if !insertedNewNodes {
		for _, newNode := range newNodes {
			updatedNodes = append(updatedNodes, newNode)
		}
	}
	
	flow["nodes"] = updatedNodes
	
	// Update flow connections if we have selected nodes
	if len(selectedNodeNames) > 0 {
		flowEdges, ok := flow["flow"].([]interface{})
		if ok {
			// Get first and last node names from new nodes and selected nodes
			firstSelectedNode := selectedNodeNames[0]
			lastSelectedNode := selectedNodeNames[len(selectedNodeNames)-1]
			
			// Get first and last from new nodes
			firstNewNode := ""
			lastNewNode := ""
			if len(newNodes) > 0 {
				if name, ok := newNodes[0]["name"].(string); ok {
					firstNewNode = name
				}
				if name, ok := newNodes[len(newNodes)-1]["name"].(string); ok {
					lastNewNode = name
				}
			}
			
			// Update edges
			var updatedEdges []interface{}
			for _, e := range flowEdges {
				edge, ok := e.(map[string]interface{})
				if !ok {
					updatedEdges = append(updatedEdges, e)
					continue
				}
				
				// Make a copy to modify
				newEdge := make(map[string]interface{})
				for k, v := range edge {
					newEdge[k] = v
				}
				
				// Update connections TO first selected node -> point to first new node
				if to, ok := newEdge["to"].(string); ok && to == firstSelectedNode && firstNewNode != "" {
					newEdge["to"] = firstNewNode
				}
				
				// Update connections FROM last selected node -> change source to last new node  
				if from, ok := newEdge["from"].(string); ok && from == lastSelectedNode && lastNewNode != "" {
					newEdge["from"] = lastNewNode
				}
				
				// Handle edges array (conditional branches)
				if edges, ok := newEdge["edges"].([]interface{}); ok {
					var newSubEdges []interface{}
					for _, subE := range edges {
						subEdge, ok := subE.(map[string]interface{})
						if !ok {
							newSubEdges = append(newSubEdges, subE)
							continue
						}
						newSubEdge := make(map[string]interface{})
						for k, v := range subEdge {
							newSubEdge[k] = v
						}
						if to, ok := newSubEdge["to"].(string); ok && to == firstSelectedNode && firstNewNode != "" {
							newSubEdge["to"] = firstNewNode
						}
						newSubEdges = append(newSubEdges, newSubEdge)
					}
					newEdge["edges"] = newSubEdges
				}
				
				// Skip edges that are FROM a selected node (except the last one, handled above)
				// or TO a selected node (except the first one, handled above)
				from, _ := newEdge["from"].(string)
				to, _ := newEdge["to"].(string)
				
				// Keep edge if it's not internal to the selection
				if !selectedSet[from] || from == lastSelectedNode {
					if !selectedSet[to] || to == firstSelectedNode {
						updatedEdges = append(updatedEdges, newEdge)
					}
				}
			}
			
			flow["flow"] = updatedEdges
		}
	}
	
	// Marshal back to YAML
	result, err := yaml.Marshal(flow)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated flow: %w", err)
	}
	
	return string(result), nil
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
	
	// Add selected nodes context if provided - include full YAML content of selected nodes
	if len(req.SelectedNodes) > 0 {
		systemPrompt += "\n\n# Selected Nodes (these are the nodes you should modify/replace):\n"
		systemPrompt += "Names: " + strings.Join(req.SelectedNodes, ", ") + "\n"
		
		// Extract full YAML of selected nodes from the current YAML
		if req.CurrentYAML != "" {
			var flow map[string]interface{}
			if err := yaml.Unmarshal([]byte(req.CurrentYAML), &flow); err == nil {
				if nodes, ok := flow["nodes"].([]interface{}); ok {
					selectedSet := make(map[string]bool)
					for _, name := range req.SelectedNodes {
						selectedSet[name] = true
					}
					
					var selectedNodeYAMLs []map[string]interface{}
					for _, n := range nodes {
						nodeMap, ok := n.(map[string]interface{})
						if !ok {
							continue
						}
						nodeName, _ := nodeMap["name"].(string)
						if selectedSet[nodeName] {
							selectedNodeYAMLs = append(selectedNodeYAMLs, nodeMap)
						}
					}
					
					if len(selectedNodeYAMLs) > 0 {
						selectedYAML, err := yaml.Marshal(selectedNodeYAMLs)
						if err == nil {
							systemPrompt += "\n```yaml\n" + string(selectedYAML) + "```\n"
							systemPrompt += "\n**IMPORTANT: Return ALL nodes (modified + any new ones) as a YAML array. The first node should replace '" + req.SelectedNodes[0] + "' and the last node should replace '" + req.SelectedNodes[len(req.SelectedNodes)-1] + "'.**"
						}
					}
				}
			}
		}
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
		
		// For node_config with node snippets, skip full-flow validation here
		// (will be validated after merging into full flow)
		if req.Context == "node_config" && isNodeSnippet(proposedYAML) {
			break
		}
		
		// Validate the YAML (for full flows)
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
	
	// For node_config context (single node editing), if the AI returned a node snippet, merge it into the full flow
	// NOTE: multi_node context now returns the full flow directly, so no merge needed
	// CRITICAL: We MUST return a full flow, never a partial snippet
	if req.Context == "node_config" && proposedYAML != "" && isNodeSnippet(proposedYAML) {
		if req.CurrentYAML != "" {
			mergedYAML, err := mergeNodeIntoFlow(proposedYAML, req.CurrentYAML, req.SelectedNodes)
			if err != nil {
				// If merge fails, DO NOT return the snippet - return nothing and show error
				fullResponse += "\n\n⚠️ Could not merge node(s) into flow: " + err.Error()
				fullResponse += "\n\nPlease try again with a clearer request."
				proposedYAML = "" // Clear the snippet - never return partial YAML
			} else {
				proposedYAML = mergedYAML
				// Re-validate the merged flow
				validation := ValidateFlowYAML(proposedYAML, availableTools)
				if !validation.Valid {
					lastValidationErrors = validation.Errors
				} else {
					lastValidationErrors = nil
				}
			}
		} else {
			// No current YAML to merge into - can't use snippet
			fullResponse += "\n\n⚠️ Cannot apply node changes without the current flow context."
			proposedYAML = "" // Clear the snippet
		}
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
