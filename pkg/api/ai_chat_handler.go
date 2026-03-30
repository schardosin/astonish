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
	"gopkg.in/yaml.v3"
)

// AIChatHandler handles AI chat requests
func AIChatHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request
	var req AIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check for streaming request
	streaming := r.Header.Get("Accept") == "text/event-stream"
	var flusher http.Flusher
	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if f, ok := w.(http.Flusher); ok {
			flusher = f
			f.Flush()
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
	}

	// Load app config
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		if streaming {
			sendSSE(w, flusher, "error", map[string]string{"error": "Failed to load config: " + err.Error()})
			return
		}
		json.NewEncoder(w).Encode(AIChatResponse{
			Error: "Failed to load config: " + err.Error(),
		})
		return
	}
	injectProviderSecrets(appCfg)

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
		if streaming {
			sendSSE(w, flusher, "error", map[string]string{"error": "Failed to create LLM client: " + err.Error()})
			return
		}
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

	// Add tools for create_flow context so AI can search for missing tools
	if req.Context == "create_flow" {
		llmReq.Config.Tools = getFlowCreationTools()
	}

	// Call LLM with validation retry loop (max 3 attempts)
	const maxRetries = 3
	var fullResponse string
	var proposedYAML string
	var lastValidationErrors []string
	var toolLogs strings.Builder

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Call LLM (with potential tool execution loop)
		const maxToolCalls = 5 // Limit tool calls to prevent infinite loops
		toolCallCount := 0

		foundStoreResults := false
	toolLoop:
		for {
			var responseText strings.Builder
			var functionCalls []*genai.FunctionCall

			for resp, err := range llm.GenerateContent(ctx, llmReq, true) {
				if err != nil {
					if streaming {
						sendSSE(w, flusher, "error", map[string]string{"error": "LLM error: " + err.Error()})
						return
					}
					json.NewEncoder(w).Encode(AIChatResponse{
						Error: "LLM error: " + err.Error(),
					})
					return
				}
				if resp != nil && resp.Content != nil {
					for _, part := range resp.Content.Parts {
						if part.Text != "" {
							responseText.WriteString(part.Text)
							if streaming {
								sendSSE(w, flusher, "chunk", map[string]string{"content": part.Text})
							}
						}
						if part.FunctionCall != nil {
							functionCalls = append(functionCalls, part.FunctionCall)
						}
					}
				}
			}

			// If no function calls, we have the final response
			if len(functionCalls) == 0 {
				fullResponse = responseText.String()
				break toolLoop
			}

			// Check tool call limit
			toolCallCount++
			if toolCallCount > maxToolCalls {
				msg := "\n\n(Tool call limit reached)"
				fullResponse = responseText.String() + msg
				if streaming {
					sendSSE(w, flusher, "chunk", map[string]string{"content": msg})
				}
				break toolLoop
			}

			// Execute the function call and prepare response
			for _, fc := range functionCalls {
				// Convert args
				args := make(map[string]interface{})
				if fc.Args != nil {
					for k, v := range fc.Args {
						args[k] = v
					}
				}

				// Guard: If we found store results, prevent internet search
				shouldSkip := fc.Name == "search_mcp_internet" && foundStoreResults

				// Stream tool start
				if streaming && !shouldSkip {
					sendSSE(w, flusher, "tool_start", map[string]interface{}{"name": fc.Name, "args": args})
				}

				var result string
				var data interface{}
				var execErr error

				if shouldSkip {
					result = "Skipped: Store results already found. Please stop searching and present the store results to the user."
					toolLogs.WriteString("> Skipped internet search (Store results found).\n\n")
				} else {
					// Log the action for user visibility (log buffer still used for history/legacy)
					if fc.Name == "search_mcp_store" {
						q, _ := args["query"].(string)
						toolLogs.WriteString(fmt.Sprintf("> **Searching Store** for: `%s`...\n", q))
					} else if fc.Name == "search_mcp_internet" {
						q, _ := args["query"].(string)
						toolLogs.WriteString(fmt.Sprintf("> **Searching Internet** for: `%s`...\n", q))
					}

					// Execute the tool
					result, data, execErr = executeFlowCreationTool(ctx, fc.Name, args)
					if execErr != nil {
						result = "Error executing tool: " + execErr.Error()
						toolLogs.WriteString(fmt.Sprintf("> Error: %s\n\n", execErr.Error()))
					} else {
						// Track if store results found
						if fc.Name == "search_mcp_store" && data != nil {
							foundStoreResults = true
						}

						// Summarize result in logs
						if strings.Contains(result, "No MCP servers found") {
							toolLogs.WriteString("> found 0 results.\n\n")
						} else {
							lines := strings.Split(result, "\n")
							if len(lines) > 0 {
								toolLogs.WriteString(fmt.Sprintf("> %s\n\n", lines[0]))
							} else {
								toolLogs.WriteString("> Found results.\n\n")
							}
						}
					}
				}

				// Stream tool end
				if streaming && !shouldSkip {
					sendSSE(w, flusher, "tool_end", map[string]interface{}{"name": fc.Name, "result": result, "result_data": data})
				}

				// Add assistant's function call and function response to history
				llmReq.Contents = append(llmReq.Contents, &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{FunctionCall: fc},
					},
				})
				llmReq.Contents = append(llmReq.Contents, &genai.Content{
					Role: "user",
					Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{
							ID:       fc.ID,
							Name:     fc.Name,
							Response: map[string]any{"result": result},
						}},
					},
				})
			}
			// Continue loop to get AI's next response
		}
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

		// Info about validation
		if streaming {
			sendSSE(w, flusher, "status", map[string]string{"message": "Validating flow..."})
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

		// Info about retry
		if streaming {
			sendSSE(w, flusher, "status", map[string]string{"message": fmt.Sprintf("Validation failed, retrying (%d/%d)...", attempt, maxRetries)})
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
			mergedYAML, mergeErr := mergeNodeIntoFlow(proposedYAML, req.CurrentYAML, req.SelectedNodes)
			if mergeErr != nil {
				// If merge fails, DO NOT return the snippet - return nothing and show error
				fullResponse += "\n\n⚠️ Could not merge node(s) into flow: " + mergeErr.Error()
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
			for _, validErr := range lastValidationErrors {
				fullResponse += "- " + validErr + "\n"
			}
		} else {
			action = "preview"
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// Prepend tool logs if any
	finalMessage := fullResponse
	if toolLogs.Len() > 0 {
		finalMessage = toolLogs.String() + "\n---\n" + fullResponse
	}

	if streaming {
		sendSSE(w, flusher, "complete", AIChatResponse{
			Message:      finalMessage,
			ProposedYAML: proposedYAML,
			Action:       action,
		})
		return
	}

	json.NewEncoder(w).Encode(AIChatResponse{
		Message:      finalMessage,
		ProposedYAML: proposedYAML,
		Action:       action,
	})
}

// sendSSE writes a Server-Sent Event to the response writer.
func sendSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(payload))
	if flusher != nil {
		flusher.Flush()
	}
}
