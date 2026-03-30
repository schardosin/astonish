package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/mcpstore"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

// getFlowCreationTools returns the tools available to AI during flow creation
func getFlowCreationTools() []*genai.Tool {
	return []*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{
			{
				Name:        "search_mcp_store",
				Description: "Search the MCP store for servers/tools. Use this when the user needs a tool that is not in the Available Tools list.",
				Parameters: &genai.Schema{
					Type:        genai.TypeObject,
					Description: "Parameters for store search",
					Properties: map[string]*genai.Schema{
						"query": {
							Type:        genai.TypeString,
							Description: "Search query for the MCP server/tool (e.g., 'ui5', 'github', 'web search')",
						},
					},
					Required: []string{"query"},
				},
			},
			{
				Name:        "search_mcp_internet",
				Description: "Search the internet for MCP servers. Use this when the tool is not found in the store.",
				Parameters: &genai.Schema{
					Type:        genai.TypeObject,
					Description: "Parameters for internet search",
					Properties: map[string]*genai.Schema{
						"query": {
							Type:        genai.TypeString,
							Description: "Search query for the MCP server (e.g., 'ui5 mcp server')",
						},
					},
					Required: []string{"query"},
				},
			},
		},
	}}
}

// executeFlowCreationTool executes a tool call from the AI and returns the result
func executeFlowCreationTool(ctx context.Context, toolName string, args map[string]interface{}) (string, interface{}, error) {
	switch toolName {
	case "search_mcp_store":
		query, _ := args["query"].(string)
		if query == "" {
			return "", nil, fmt.Errorf("query is required")
		}

		// Load all servers from taps
		servers, err := loadAllServersFromTaps()
		if err != nil {
			return fmt.Sprintf("Error loading store: %s", err.Error()), nil, nil
		}

		// Filter to only installable servers
		var installableServers []mcpstore.Server
		var toolSummaries []string
		for _, srv := range servers {
			if srv.Config != nil {
				installableServers = append(installableServers, srv)
				tags := ""
				if len(srv.Tags) > 0 {
					tags = " [tags: " + strings.Join(srv.Tags, ", ") + "]"
				}
				toolSummaries = append(toolSummaries, fmt.Sprintf("- %s: %s%s", srv.Name, srv.Description, tags))
			}
		}

		// Use the same AI search logic
		matchingTools := findToolsWithAI(ctx, query, toolSummaries, installableServers)

		if len(matchingTools) == 0 {
			return fmt.Sprintf("No MCP servers found in the store matching '%s'. Try search_mcp_internet to search online.", query), nil, nil
		}

		// Build result
		var result strings.Builder
		result.WriteString(fmt.Sprintf("Found %d MCP servers in the store:\n", len(matchingTools)))
		for i, t := range matchingTools {
			result.WriteString(fmt.Sprintf("%d. %s - %s (ID: %s)\n", i+1, t.Name, t.Description, t.ID))
		}
		result.WriteString("\nTell the user to install one of these servers before creating the flow.")
		return result.String(), matchingTools, nil

	case "search_mcp_internet":
		query, _ := args["query"].(string)
		if query == "" {
			return "", nil, fmt.Errorf("query is required")
		}

		// Check if web search is configured
		webSearchConfigured, serverName, toolName := IsWebSearchConfigured()
		if !webSearchConfigured {
			return "Internet search is not configured. Tell the user to configure a web search tool (like Tavily) in Settings.", nil, nil
		}

		// Use the internet search function
		results, err := searchInternetForMCPServers(ctx, serverName, toolName, query+" MCP server github npm")
		if err != nil {
			return fmt.Sprintf("Search error: %s", err.Error()), nil, nil
		}

		if len(results) == 0 {
			return fmt.Sprintf("No MCP servers found on the internet for '%s'.", query), nil, nil
		}

		// Build result
		var result strings.Builder
		result.WriteString(fmt.Sprintf("Found %d MCP servers online:\n", len(results)))
		for i, t := range results {
			if i >= 5 {
				break // Limit to 5 results
			}
			result.WriteString(fmt.Sprintf("%d. %s - %s\n   Install: %s\n", i+1, t.Name, t.Description, t.URL))
		}
		result.WriteString("\nTell the user to install one of these servers before creating the flow.")
		return result.String(), results, nil

	default:
		return "", nil, fmt.Errorf("unknown tool: %s", toolName)
	}
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
Create agent workflows. Be CONCISE and PROCESS-ORIENTED.

## You Have Tools
You have TWO tools available to find MCP servers:
1. search_mcp_store - Search the store for installed/installable MCP servers
2. search_mcp_internet - Search the internet for MCP servers (use if not in store)

## Process for Creating Flows with External Tools

**IMPORTANT: Tool Already Installed Check**
If the user says "install X" or "find X" or "get X mcp", FIRST check the "Available Tools" list above.
If you find a tool with "X" in its name or source, the tool is ALREADY INSTALLED! Respond with:
"Great news! The [tool name] is already installed and ready to use. Would you like me to create a flow that uses it?"
Do NOT search the store or internet if the tool is already available.

**Step 1: Check if needed tool is in Available Tools**
Look at the "Available Tools" list above. If the tool the user needs is there, proceed to create the flow.

**Step 2: If tool is NOT available, CALL search_mcp_store**
Call the search_mcp_store tool with the tool name. Example:
- User wants "ui5 mcp" → call search_mcp_store(query: "ui5")

**CRITICAL: If search_mcp_store finds tools, STOP SEARCHING.** 
If the store returns ANY results (even if not perfect matches), you MUST STOP.
1. Present the store results to the user.
2. Say: "I found potential tools in the store. Please install one if it fits."
3. Say: "If none of these work, you can use the 'Search Internet' link below."
4. Do NOT call search_mcp_internet. (The system prohibits this).
5. Do NOT say "Let me search the internet".
6. Do NOT offer unrelated alternatives (Shell Commands, Filesystem) at this stage. Focus on finding the tool.

**Step 3: If not in store, CALL search_mcp_internet**
If store search returns 0 results, call search_mcp_internet.

**Step 4: Present results to user**
After search, simply tell the user what was found.
**CRITICAL:** Instruct the user to CLICK THE "INSTALL" BUTTON on the results card above.
Do NOT ask "Which one would you like to install?" (because they must click the button).
Just say: "Please examine the results above and click 'Install' on the server you want to use."

## When Creating the Flow
- Generate complete YAML
- Make reasonable assumptions (don't ask unnecessary questions)
- Follow the flow schema exactly

## Design Guidelines

### Step 2: Design Minimal Flow
- What is the MINIMUM number of nodes needed?
- Avoid unnecessary nodes (no separate "check_exit" nodes - use input with options instead)
- A simple Q&A needs only: input → llm → (optional: input to continue) → loop

### Step 3: Tool Parameter Analysis
For EACH tool you plan to use (if any):
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

### Step 6: Tool Usage Check (CRITICAL!)
- **NEVER use standalone "tool" nodes** - they execute blindly without intelligence
- **ALWAYS use LLM nodes with tools: true** - the LLM can reason, adapt, handle errors
- Tool nodes are ONLY for rare cases of fixed/deterministic operations
- Example: Instead of type: tool, use type: llm with tools_selection and tools: true

## IMPORTANT: Judge the Request Type

**ACTION REQUEST** (user wants to create/modify flows or install tools):
→ Follow the planning steps above and generate YAML

**FLOW-RELATED QUESTION** (user is asking about their flow, tools, or Astonish capabilities):
→ Answer the question helpfully. Do NOT generate YAML.

**OFF-TOPIC QUESTION** (user asks about unrelated topics like history, science, general knowledge, etc.):
→ Politely decline and redirect. Example response:
"I'm your Astonish Flow Assistant, focused on helping you create and modify AI agent workflows. I can help you with:
- Creating new flows
- Modifying existing flows  
- Finding and installing MCP tools
- Questions about available tools and capabilities

What would you like to build today?"

Examples of FLOW-RELATED QUESTIONS (answer these):
- "What is the name of this flow?"
- "How many nodes does it have?"
- "What tools are available?"
- "Can you explain what this does?"
- "How do I add a tool to my flow?"

Examples of OFF-TOPIC QUESTIONS (politely decline these):
- "Who was Einstein?"
- "What is the capital of France?"
- "Explain quantum physics"
- "Write me a poem"
- "Tell me a joke"

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
You are the Astonish Flow Assistant with THREE main capabilities:

## Capability 1: Design and Create Agent Workflows
Help users design, create, and modify agent workflows. Check if required tools are available before generating YAML.

## Capability 2: Install MCP Servers
When a user asks to install an MCP server (e.g., "install ui5 mcp server", "find github tool"):
1. First, ALWAYS search the store by responding with:
   "Let me search for [tool name] in the MCP store..."
   (The system will automatically trigger a store search)
2. If found in store, show results and let user install
3. If NOT found in store, respond with:
   "I couldn't find [tool name] in the store. Let me search the internet for it..."
   (The system will automatically trigger an internet search)
4. Show internet results and let user install from there

## Capability 3: Search for MCP Servers
When a user wants to find tools (e.g., "find a tool for screenshots", "what MCP servers exist for X"):
1. First search the store for matching tools
2. Then search the internet if needed
3. Present options to the user

## TASK DETECTION:
- If user mentions "install", "find", "get", "add" + MCP/tool/server → Use Capability 2
- If user asks to "create", "build", "design" + flow/workflow/agent → Use Capability 1
- If user asks "what tools", "search for", "find tools for" → Use Capability 3

## What You CANNOT Help With:
- General knowledge questions (history, science, etc.)
- Off-topic conversations
- Anything unrelated to Astonish and AI agent workflows

**If the user asks an off-topic question**, politely decline:
"I'm your Astonish Flow Assistant. I can help you create AI agent workflows, find and install MCP tools, and answer questions about flow design. What would you like to do?"

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
		result, err := yaml.Marshal(OrderYamlKeys(flow))
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
	result, err := yaml.Marshal(OrderYamlKeys(flow))
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated flow: %w", err)
	}

	return string(result), nil
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
