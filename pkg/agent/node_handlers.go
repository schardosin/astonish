package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/ui"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// handleUpdateStateNode handles update_state nodes
func (a *AstonishAgent) handleUpdateStateNode(ctx agent.InvocationContext, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	// Fallback to simple Updates map if Action is not set
	if node.Action == "" && len(node.Updates) > 0 {
		stateDelta := make(map[string]any)
		for key, valueTemplate := range node.Updates {
			value := a.renderString(valueTemplate, state)
			if err := state.Set(key, value); err != nil {
				yield(nil, fmt.Errorf("failed to set state key %s: %w", key, err))
				return false
			}
			stateDelta[key] = value
		}
		// Emit event (no LLMResponse text - state updates are internal)
		event := &session.Event{
			Actions: session.EventActions{StateDelta: stateDelta},
		}
		return yield(event, nil)
	}

	// Python-compatible logic
	if len(node.OutputModel) != 1 {
		yield(nil, fmt.Errorf("update_state node must have exactly one key in output_model defining the target variable"))
		return false
	}

	var targetVar string
	for k := range node.OutputModel {
		targetVar = k
		break
	}

	var valueToUse any
	if node.SourceVariable != "" {
		val, err := state.Get(node.SourceVariable)
		if err != nil {
			// If source variable missing, is it an error? Python raises KeyError.
			yield(nil, fmt.Errorf("failed to get source variable %s: %w", node.SourceVariable, err))
			return false
		}
		valueToUse = val
	} else if node.Value != nil {
		valueToUse = node.Value
	} else {
		yield(nil, fmt.Errorf("update_state node must have either source_variable or value"))
		return false
	}

	// Render string values
	if strVal, ok := valueToUse.(string); ok {
		valueToUse = a.renderString(strVal, state)
	}

	stateDelta := make(map[string]any)

	switch node.Action {
	case "overwrite":
		if err := state.Set(targetVar, valueToUse); err != nil {
			yield(nil, fmt.Errorf("failed to set state variable %s: %w", targetVar, err))
			return false
		}
		stateDelta[targetVar] = valueToUse

	case "append":
		// Get existing list
		existing, err := state.Get(targetVar)
		var list []any
		if err == nil && existing != nil {
			if l, ok := existing.([]any); ok {
				list = l
			} else if l, ok := existing.([]string); ok {
				list = make([]any, len(l))
				for i, v := range l {
					list[i] = v
				}
			} else {
				// Initialize new list if type mismatch
				list = []any{}
			}
		} else {
			list = []any{}
		}

		// Append
		if valList, ok := valueToUse.([]any); ok {
			list = append(list, valList...)
		} else if valList, ok := valueToUse.([]string); ok {
			for _, v := range valList {
				list = append(list, v)
			}
		} else {
			list = append(list, valueToUse)
		}

		if err := state.Set(targetVar, list); err != nil {
			yield(nil, fmt.Errorf("failed to set state variable %s: %w", targetVar, err))
			return false
		}
		stateDelta[targetVar] = list

	case "increment":
		// Get increment amount from value field (default to 1)
		incrementBy := 1
		if node.Value != nil {
			switch v := node.Value.(type) {
			case int:
				incrementBy = v
			case float64:
				incrementBy = int(v)
			case string:
				// Try to parse as int
				if parsed, err := strconv.Atoi(v); err == nil {
					incrementBy = parsed
				}
			}
		}

		// Get current value (from source_variable or target)
		var currentVal int
		sourceVar := node.SourceVariable
		if sourceVar == "" {
			sourceVar = targetVar // If no source, use target as both source and dest
		}
		if existing, err := state.Get(sourceVar); err == nil && existing != nil {
			switch v := existing.(type) {
			case int:
				currentVal = v
			case float64:
				currentVal = int(v)
			case string:
				if parsed, err := strconv.Atoi(v); err == nil {
					currentVal = parsed
				}
			}
		}

		// Increment
		newVal := currentVal + incrementBy

		if err := state.Set(targetVar, newVal); err != nil {
			yield(nil, fmt.Errorf("failed to set state variable %s: %w", targetVar, err))
			return false
		}
		stateDelta[targetVar] = newVal

	default:
		yield(nil, fmt.Errorf("unsupported action: %s", node.Action))
		return false
	}

	// Emit event with state delta (no LLMResponse text - state updates are internal)
	event := &session.Event{
		Actions: session.EventActions{
			StateDelta: stateDelta,
		},
	}

	return yield(event, nil)
}

// handleToolApproval handles the approval flow when resuming after pause
func (a *AstonishAgent) handleToolApproval(ctx agent.InvocationContext, state session.State, yield func(*session.Event, error) bool) bool {
	// Check if user provided approval
	if ctx.UserContent() == nil || len(ctx.UserContent().Parts) == 0 {
		return true // No user input, continue
	}

	// Get the tool name that's awaiting approval
	toolNameVal, _ := state.Get("approval_tool")
	toolName, _ := toolNameVal.(string)

	// Extract approval response
	var responseText string
	for _, part := range ctx.UserContent().Parts {
		if part.Text != "" {
			responseText += part.Text
		}
	}

	// Strip the timestamp prefix injected by NewTimestampedUserContent
	// (format: "[2026-03-20 14:30:05 UTC]\n") before checking approval.
	responseText = strings.ToLower(strings.TrimSpace(StripTimestamp(responseText)))

	approved := responseText == "yes" || responseText == "y" || responseText == "approve"

	if approved {
		// Get current node for node-scoped approval
		currentNode := ""
		if nodeVal, err := state.Get("current_node"); err == nil && nodeVal != nil {
			if nodeName, ok := nodeVal.(string); ok {
				currentNode = nodeName
			}
		}

		// Grant approval using the node-scoped key
		approvalKey := fmt.Sprintf("approval:%s:%s", currentNode, toolName)
		state.Set(approvalKey, true)
		state.Set("awaiting_approval", false)
		state.Set("approval_tool", "")
		state.Set("approval_args", nil)

		// Emit state delta event so history fallback sees approval was resolved
		yield(&session.Event{
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"awaiting_approval": false,
				},
			},
		}, nil)

		// [MCP-SPECIFIC FIX] Inject a very specific instruction for MCP tools
		// MCP tools are sensitive to exact arguments - must retry with same args
		retryPrompt := fmt.Sprintf(
			"User approved execution. IMMEDIATELY call the function '%s' again with the exact same arguments as before.",
			toolName,
		)

		// Override the user input in the context so the LLM sees the instruction
		ctx.UserContent().Parts = []*genai.Part{{
			Text: retryPrompt,
		}}

		return true // Continue execution with retry prompt
	} else {
		// User denied - move to next node
		state.Set("awaiting_approval", false)
		state.Set("approval_tool", "")
		state.Set("approval_args", nil)

		event := &session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{
						Text: "[ℹ️ Info] Tool execution denied by user. Skipping to next node.\n",
					}},
					Role: "model",
				},
			},
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"awaiting_approval": false,
				},
			},
		}
		yield(event, nil)

		// Get current node and move to next
		currentNodeVal, _ := state.Get("current_node")
		currentNode, _ := currentNodeVal.(string)
		nextNode, err := a.getNextNode(currentNode, state)
		if err != nil {
			yield(nil, err)
			return false
		}
		state.Set("current_node", nextNode)

		return true // Continue to next node
	}
}

// handleParallelNode handles nodes with parallel configuration
func (a *AstonishAgent) handleParallelNode(ctx agent.InvocationContext, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	pConfig := node.Parallel
	if pConfig == nil {
		return false
	}

	// 1. Get the list to iterate over
	listKey := strings.Trim(pConfig.ForEach, "{}") // Remove potential braces
	listVal, err := state.Get(listKey)
	if err != nil {
		yield(nil, fmt.Errorf("failed to get parallel list '%s': %w", listKey, err))
		return false
	}

	// Convert to slice
	var items []any
	if l, ok := listVal.([]any); ok {
		items = l
	} else if l, ok := listVal.([]string); ok {
		for _, v := range l {
			items = append(items, v)
		}
	} else if l, ok := listVal.([]int); ok {
		for _, v := range l {
			items = append(items, v)
		}
	} else {
		// Try reflection or assume empty/error
		// Also handle if it's a list of maps (common in JSON)
		if l, ok := listVal.([]map[string]any); ok {
			for _, v := range l {
				items = append(items, v)
			}
		} else if l, ok := listVal.([]interface{}); ok {
			items = l
		} else {
			yield(nil, fmt.Errorf("variable '%s' is not a list (type: %T)", listKey, listVal))
			return false
		}
	}

	if len(items) == 0 {
		return true
	}

	// 2. Prepare for aggregation
	if len(node.OutputModel) != 1 {
		yield(nil, fmt.Errorf("parallel node must have exactly one key in output_model"))
		return false
	}
	var outputKey string
	for k := range node.OutputModel {
		outputKey = k
		break
	}

	// 3. Execute in Parallel
	maxConcurrency := 1
	if pConfig.MaxConcurrency > 0 {
		maxConcurrency = pConfig.MaxConcurrency
	}

	// Semaphore to limit concurrency
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex // Protects results and yield

	// Pre-allocate results to preserve order
	results := make([]any, len(items))
	// Track success to know if we should include the result
	successes := make([]bool, len(items))

	// Initialize progress bar UI
	prog := ui.NewParallelProgram(len(items), node.Name)

	// Channel to signal UI completion
	uiDone := make(chan struct{})

	// Run UI in a goroutine
	go func() {
		defer close(uiDone)
		if _, err := prog.Run(); err != nil {
			slog.Error("error running progress UI", "error", err)
		}
	}()

	// Wrap yield to be thread-safe and track cancellation
	yieldCancelled := false
	safeYield := func(event *session.Event, err error) bool {
		mu.Lock()
		defer mu.Unlock()
		if yieldCancelled {
			return false
		}

		// Log tool calls and errors to UI
		if event != nil && event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil {
					msg := fmt.Sprintf("Calling tool: %s", part.FunctionCall.Name)
					prog.Send(ui.ItemLogMsg(msg))
				}
				if part.FunctionResponse != nil {
					msg := fmt.Sprintf("Tool finished: %s", part.FunctionResponse.Name)
					prog.Send(ui.ItemLogMsg(msg))
				}
			}
		}
		if err != nil {
			msg := fmt.Sprintf("Error: %v", err)
			prog.Send(ui.ItemLogMsg(msg))
		}

		// Suppress StateDelta and LLMResponse to prevent console printing during parallel execution
		// We only want to collect the results, not print them as they happen
		if event != nil {
			if event.Actions.StateDelta != nil {
				event.Actions.StateDelta = nil
			}
			// Also suppress content streaming
			if event.LLMResponse.Content != nil {
				event.LLMResponse.Content = nil
			}
		}

		if !yield(event, err) {
			yieldCancelled = true
			return false
		}
		return true
	}

	// Track active workers
	var activeWorkers int32

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it any) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}

			// Update active count
			atomic.AddInt32(&activeWorkers, 1)
			prog.Send(ui.ActiveCountMsg(atomic.LoadInt32(&activeWorkers)))

			defer func() {
				// Update active count
				atomic.AddInt32(&activeWorkers, -1)
				prog.Send(ui.ActiveCountMsg(atomic.LoadInt32(&activeWorkers)))
				<-sem
			}()

			// Check for cancellation or stop flag
			select {
			case <-ctx.Done():
				return
			default:
				// Check for force_stop_parallel flag in parent state
				// Use a mutex if needed, but state.Get is thread-safe enough for this check
				if val, err := state.Get("force_stop_parallel"); err == nil {
					if b, ok := val.(bool); ok && b {
						return
					}
				}
			}

			scopedState := &ScopedState{
				Parent: state,
				Local:  make(map[string]any),
			}

			scopedState.Local[pConfig.As] = it
			if pConfig.IndexAs != "" {
				scopedState.Local[pConfig.IndexAs] = idx
			}

			// Workaround for "Severity" template error in ADK llmagent
			if _, err := scopedState.Get("Severity"); err != nil {
				scopedState.Local["Severity"] = "{{Severity}}"
			}

			// Create ephemeral session for isolation
			// This ensures each parallel branch has its own history
			// Include node name to avoid collisions between different parallel nodes in the same flow
			newSessionID := fmt.Sprintf("%s:%s:parallel-%d", ctx.Session().ID(), node.Name, idx)

			// Create a session using the service
			// We need to pass AppName and UserID if possible
			createReq := &session.CreateRequest{
				SessionID: newSessionID,
			}

			// If we can get real values:
			if s, ok := ctx.Session().(interface{ AppName() string }); ok {
				createReq.AppName = s.AppName()
			}
			if s, ok := ctx.Session().(interface{ UserID() string }); ok {
				createReq.UserID = s.UserID()
			}

			createResp, err := a.SessionService.Create(ctx, createReq)
			if err != nil {
				safeYield(nil, fmt.Errorf("failed to create ephemeral session: %w", err))
				return
			}

			scopedCtx := &ScopedContext{
				InvocationContext: ctx,
				state:             scopedState,
				session:           createResp.Session,
			}

			success := false
			if node.Type == "tool" {
				success = a.handleToolNode(scopedCtx, node, scopedState, safeYield)
			} else if node.Type == "llm" {
				success = a.executeLLMNode(scopedCtx, node, node.Name, scopedState, safeYield)
			} else {
				safeYield(nil, fmt.Errorf("unsupported type for parallel node: %s", node.Type))
				return
			}

			// Signal UI that item is finished
			prog.Send(ui.ItemFinishedMsg{})

			if !success {
				// If execution failed, don't try to get the result
				// Just return - the error has already been yielded
				return
			}

			val, err := scopedState.Get(outputKey)
			if err == nil {
				mu.Lock()
				results[idx] = val
				successes[idx] = true
				mu.Unlock()
			}
		}(i, item)
	}

	wg.Wait()

	// Ensure UI is done (though model handles auto-quit)
	// We can wait a tiny bit to ensure the final render happens if needed,
	// but usually wg.Wait() + the model's logic is enough.
	<-uiDone

	// Check if cancelled during execution (includes errors)
	if yieldCancelled {
		return false
	}

	// 4. Update Parent State with Aggregated Results
	// Filter results based on success to maintain density if needed,
	// OR keep them sparse?
	// The original sequential logic appended only on success.
	// So we should filter.
	var finalResults []any
	for i, s := range successes {
		if s {
			finalResults = append(finalResults, results[i])
		}
	}

	existingVal, _ := state.Get(outputKey)
	var final []any

	if existingVal != nil {
		if l, ok := existingVal.([]any); ok {
			final = l
		} else {
			final = []any{}
		}
	} else {
		final = []any{}
	}

	// Aggregate results based on output_action
	// If output_action is "append", flatten lists; otherwise keep as-is
	outputAction := "append" // Default behavior
	if node.OutputAction != "" {
		outputAction = node.OutputAction
	}

	for _, res := range finalResults {
		if outputAction == "append" {
			// Check if res is a JSON string that needs parsing
			if strRes, ok := res.(string); ok {
				// Try to parse as JSON
				cleaned := a.cleanAndFixJson(strRes)
				var parsed any
				if err := json.Unmarshal([]byte(cleaned), &parsed); err == nil {
					// Successfully parsed - check if it's a map with the output key
					if parsedMap, ok := parsed.(map[string]any); ok {
						// Check if the map contains the output key (e.g., "review_comment_validated")
						if val, ok := parsedMap[outputKey]; ok {
							// Extract the value from the nested structure
							if l, ok := val.([]any); ok {
								// It's a list - flatten it
								final = append(final, l...)
								continue
							} else {
								// It's a single value - append it
								final = append(final, val)
								continue
							}
						}
					}
					// If parsed but doesn't match expected structure, treat as regular value
					res = parsed
				}
				// If parsing failed, continue with string as-is
			}

			// Flatten lists when appending
			if l, ok := res.([]any); ok {
				// Flatten the list - append all items from the list
				final = append(final, l...)
			} else {
				// Not a list, append as-is
				final = append(final, res)
			}
		} else {
			// Keep results as-is (don't flatten)
			final = append(final, res)
		}
	}

	state.Set(outputKey, final)

	yield(&session.Event{
		Actions: session.EventActions{
			StateDelta: map[string]any{
				outputKey: final,
			},
		},
	}, nil)

	return true
}

// handleOutputNode handles output nodes
func (a *AstonishAgent) handleOutputNode(ctx agent.InvocationContext, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	var parts []string
	for _, msgPart := range node.UserMessage {
		// Check if part is a state variable
		if val, err := state.Get(msgPart); err == nil {
			parts = append(parts, ui.FormatAsYamlLike(val, 0))
		} else {
			// Not a state variable, use as literal
			parts = append(parts, msgPart)
		}
	}

	message := strings.Join(parts, "\n")

	// Emit message event with marker for frontend to preserve whitespace

	evt := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: message}},
				Role:  "model",
			},
		},
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"_output_node": true, // Marker for frontend to apply pre-wrap styling
			},
		},
	}

	return yield(evt, nil)
}
