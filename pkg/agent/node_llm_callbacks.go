package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// buildApprovalCallback creates the BeforeToolCallback for approval-gated tools.
// Events are buffered in cbBuf (not yielded directly) because ADK may invoke
// this callback from a goroutine, and yield is not goroutine-safe.
func (a *AstonishAgent) buildApprovalCallback(node *config.Node, state session.State, cbBuf *callbackEventBuffer) llmagent.BeforeToolCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		toolName := t.Name()

		// DEBUG: Log tool execution attempt
		if a.DebugMode {
			argsJSON, _ := json.MarshalIndent(args, "", "  ")
			slog.Debug("tool execution attempt", "tool", toolName, "arguments", string(argsJSON))
		}

		// Node-scoped approval key
		approvalKey := fmt.Sprintf("approval:%s:%s", node.Name, toolName)

		// Check if we already have approval for this tool
		approvedVal, _ := state.Get(approvalKey)

		approved := false
		if b, ok := approvedVal.(bool); ok && b {
			approved = true
		}

		if a.DebugMode {
			slog.Debug("approval check", "approved", approved, "approval_key", approvalKey)
		}

		// Fallback: Check history for explicit user approval if state is missing
		if !approved && a.SessionService != nil {
			// Retrieve session from service to get events
			sessResp, err := a.SessionService.Get(ctx, &session.GetRequest{
				AppName:   ctx.AppName(),
				UserID:    ctx.UserID(),
				SessionID: ctx.SessionID(),
			})

			if err == nil && sessResp != nil && sessResp.Session != nil {
				events := sessResp.Session.Events()
				if events.Len() >= 2 {
					lastEvent := events.At(events.Len() - 1)
					prevEvent := events.At(events.Len() - 2)

					// Check if last event is user "Yes"
					isUserYes := false
					if lastEvent.Author == "user" && lastEvent.LLMResponse.Content != nil && len(lastEvent.LLMResponse.Content.Parts) > 0 {
						text := strings.TrimSpace(lastEvent.LLMResponse.Content.Parts[0].Text)
						if strings.EqualFold(text, "Yes") {
							isUserYes = true
						}
					}

					if isUserYes {
						// Check if prev event was an approval request
						if prevEvent.Actions.StateDelta != nil {
							if val, ok := prevEvent.Actions.StateDelta["awaiting_approval"]; ok {
								if b, ok := val.(bool); ok && b {
									// Check if it was for THIS tool
									if toolVal, ok := prevEvent.Actions.StateDelta["approval_tool"]; ok {
										if tName, ok := toolVal.(string); ok && tName == toolName {
											approved = true
											if a.DebugMode {
												slog.Debug("approved via history check")
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}

		if approved {
			// Consume approval - each execution requires new approval
			state.Set(approvalKey, false)
			if a.DebugMode {
				slog.Debug("tool approved, allowing execution to proceed")
			}
			// Allow tool to run - returning nil means "proceed with actual execution"
			// The actual tool result will be captured by the ADK framework
			return nil, nil
		}

		if a.DebugMode {
			slog.Debug("tool not approved, requesting user approval")
		}

		// Check if we're already awaiting approval for another tool
		// This handles parallel tool calls - only show first prompt
		if alreadyAwaitingVal, _ := state.Get("awaiting_approval"); alreadyAwaitingVal != nil {
			if alreadyAwaiting, ok := alreadyAwaitingVal.(bool); ok && alreadyAwaiting {
				if a.DebugMode {
					slog.Debug("already awaiting approval for another tool, skipping", "tool", toolName)
				}
				// Return placeholder silently - don't emit another approval request
				return map[string]any{
					"status": "pending_approval",
					"info":   "Waiting for user approval on a previous tool call.",
				}, nil
			}
		}

		// No approval - request it
		// We set 'force_pause' to tell the outer loop to stop immediately
		state.Set("force_pause", true)
		state.Set("awaiting_approval", true)
		state.Set("approval_tool", toolName)
		state.Set("approval_args", args)

		// Buffer approval request event (NOT yield — runs in ADK goroutine)
		prompt := a.formatToolApprovalRequest(toolName, args)
		cbBuf.append(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: prompt}},
					Role:  "model",
				},
			},
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"awaiting_approval": true,
					"approval_tool":     toolName,
					"approval_options":  []string{"Yes", "No"},
				},
			},
		})

		// Return a placeholder result
		// This string enters the LLM context. When we resume, we will overwrite this
		// context or instruct the LLM to ignore it and retry.
		placeholderResult := map[string]any{
			"status": "pending_approval",
			"info":   "Execution paused for user approval. When you see this message in history, it means the user has APPROVED the execution. You MUST retry the tool call immediately.",
		}

		if a.DebugMode {
			placeholderJSON, _ := json.MarshalIndent(placeholderResult, "", "  ")
			slog.Debug("returning placeholder result", "result", string(placeholderJSON))
		}

		return placeholderResult, nil
	}
}

// buildAfterToolCallback creates the AfterToolCallback for debugging and raw_tool_output handling.
// Events are buffered in cbBuf (not yielded directly) because ADK may invoke
// this callback from a goroutine, and yield is not goroutine-safe.
func (a *AstonishAgent) buildAfterToolCallback(node *config.Node, state session.State, cbBuf *callbackEventBuffer) llmagent.AfterToolCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
		// Redact credential values from all tool outputs before the LLM sees them.
		// resolve_credential now returns {{CREDENTIAL:...}} placeholders instead
		// of raw values, so no exemption is needed.
		if a.Redactor != nil && result != nil {
			result = a.Redactor.RedactMap(result)
		}

		if err != nil {
			// Tool failed, let the LLM see the error
			return result, err
		}

		toolName := t.Name()

		// DEBUG: Log successful tool execution
		if a.DebugMode {
			resultJSON, _ := json.MarshalIndent(result, "", "  ")
			slog.Debug("after tool callback", "tool", toolName, "result", string(resultJSON))
		}

		// Handle raw_tool_output: Store actual result in state, return sanitized message to LLM
		// This prevents large tool outputs from polluting the LLM's context window
		// Handle raw_tool_output: Store actual result in state, return sanitized message to LLM
		// This prevents large tool outputs from polluting the LLM's context window
		if len(node.RawToolOutput) == 1 {
			// IMPORTANT: Skip storing if this is an approval message, not actual tool output
			// When a tool requires approval, the "result" contains pending_approval status
			// We should NOT store this as the raw_tool_output - wait for actual tool result
			if status, hasStatus := result["status"]; hasStatus {
				if statusStr, ok := status.(string); ok && statusStr == "pending_approval" {
					// This is an approval message, not the actual tool result
					// Skip storing and let the approval flow handle things
					if a.DebugMode {
						slog.Debug("raw_tool_output: skipping storage, result is pending_approval message")
					}
					return result, nil
				}
			}

			// Get the state key to store the raw output
			var stateKey string
			for k := range node.RawToolOutput {
				stateKey = k
				break
			}

			if a.DebugMode {
				resultSummary := "empty"
				if len(result) > 0 {
					keys := make([]string, 0, len(result))
					for k := range result {
						keys = append(keys, k)
					}
					resultSummary = fmt.Sprintf("keys=%v", keys)
				}
				slog.Debug("raw_tool_output: storing result", "state_key", stateKey, "result_summary", resultSummary)
			}

			// Store the actual tool result in state (in-memory)
			if err := state.Set(stateKey, result); err != nil {
				return result, fmt.Errorf("failed to set raw_tool_output state key %s: %w", stateKey, err)
			}

			// Create the StateDelta event
			stateEvent := &session.Event{
				Actions: session.EventActions{
					StateDelta: map[string]any{
						stateKey: result,
					},
				},
			}

			// Buffer StateDelta event (NOT yield — runs in ADK goroutine)
			cbBuf.append(stateEvent)

			// ALSO: Directly append event to session service for SYNCHRONOUS persistence
			// This ensures the state is available in subsequent Run invocations
			// even if async event processing hasn't completed
			if a.SessionService != nil {
				// Get session from context
				if invCtx, ok := ctx.(agent.InvocationContext); ok {
					sess := invCtx.Session()
					// Try to append synchronously
					if appendErr := a.SessionService.AppendEvent(ctx, sess, stateEvent); appendErr != nil {
						// Log warning only in DebugMode - this is a fallback, yield should still work
						if a.DebugMode {
							slog.Debug("raw_tool_output: failed to directly append event", "error", appendErr)
						}
					} else if a.DebugMode {
						slog.Debug("raw_tool_output: directly appended event to session service", "state_key", stateKey)
					}
				}
			}

			if a.DebugMode {
				slog.Debug("raw_tool_output: emitted state delta", "state_key", stateKey)
			}

			// Return sanitized message to LLM instead of raw data
			// This matches Python's behavior: LLM doesn't see the actual data
			sanitizedResult := map[string]any{
				"status":  "success",
				"message": fmt.Sprintf("Tool '%s' executed successfully. Its output has been directly stored in the agent's state under the key '%s'.", toolName, stateKey),
			}

			if a.DebugMode {
				slog.Debug("raw_tool_output: returning sanitized result to llm", "result", sanitizedResult)
			}

			return sanitizedResult, nil
		}

		return result, nil
	}
}
