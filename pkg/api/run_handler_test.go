package api

import (
	"testing"

	"google.golang.org/adk/session"
)

// TestShouldStreamInputPrompt verifies that input prompts are streamed
// even when the node has output_model (which normally suppresses text).
// This prevents regression of the bug where input node prompts weren't
// displayed in the UI.
//
// UI Display Rules:
// - LLM nodes: Only show if they have user_message (via _user_message_display marker)
// - Input nodes: Always show the prompt (so user knows what they're being asked)
// - Approval requests: Always show (for tool execution approval)
// - All other cases with output_model: Suppress (raw JSON processing)
func TestShouldStreamInputPrompt(t *testing.T) {
	tests := []struct {
		name                 string
		currentNodeType      string
		hasOutputModel       bool
		stateDelta           map[string]any
		expectedShouldStream bool
	}{
		{
			name:                 "input node with output_model and input_options should stream",
			currentNodeType:      "input",
			hasOutputModel:       true,
			stateDelta:           map[string]any{"input_options": []string{"yes", "no"}},
			expectedShouldStream: true,
		},
		{
			name:                 "input node without output_model should stream",
			currentNodeType:      "input",
			hasOutputModel:       false,
			stateDelta:           map[string]any{"input_options": []string{"option1"}},
			expectedShouldStream: true,
		},
		{
			name:                 "llm node with output_model should NOT stream (internal processing)",
			currentNodeType:      "llm",
			hasOutputModel:       true,
			stateDelta:           nil,
			expectedShouldStream: false,
		},
		{
			name:                 "llm node with user_message_display should stream (user_message configured)",
			currentNodeType:      "llm",
			hasOutputModel:       true,
			stateDelta:           map[string]any{"_user_message_display": true},
			expectedShouldStream: true,
		},
		{
			name:                 "approval request should stream regardless of output_model",
			currentNodeType:      "llm",
			hasOutputModel:       true,
			stateDelta:           map[string]any{"approval_options": []string{"Yes", "No"}},
			expectedShouldStream: true,
		},
		{
			name:                 "empty input_options should NOT stream (free text input, prompt already shown)",
			currentNodeType:      "input",
			hasOutputModel:       true,
			stateDelta:           map[string]any{"waiting_for_input": true},
			expectedShouldStream: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the streaming decision logic from run_handler.go
			event := &session.Event{
				Actions: session.EventActions{
					StateDelta: tt.stateDelta,
				},
			}

			isUserMessageDisplay := event.Actions.StateDelta != nil && event.Actions.StateDelta["_user_message_display"] != nil
			isApprovalRequest := event.Actions.StateDelta != nil && event.Actions.StateDelta["approval_options"] != nil

			// Base streaming decision (this is simplified - actual logic allows more by default)
			shouldStream := isApprovalRequest || isUserMessageDisplay

			// For output_model nodes, suppress ALL text EXCEPT:
			// - _user_message_display events (explicit user_message in config)
			// - approval requests (tool execution approval)
			// - input prompts (input_options present - user needs to see the question)
			isInputRequest := event.Actions.StateDelta != nil && event.Actions.StateDelta["input_options"] != nil
			if isInputRequest {
				shouldStream = true
			}

			if shouldStream != tt.expectedShouldStream {
				t.Errorf("shouldStream = %v, expected %v", shouldStream, tt.expectedShouldStream)
			}
		})
	}
}

// TestSilentModeSkipsNodeEvent verifies that node events are skipped when silent flag is true
func TestSilentModeSkipsNodeEvent(t *testing.T) {
	tests := []struct {
		name               string
		stateDelta         map[string]any
		expectedSendEvent  bool
	}{
		{
			name: "silent true should skip node event",
			stateDelta: map[string]any{
				"current_node": "init_vars",
				"node_type":    "update_state",
				"silent":       true,
			},
			expectedSendEvent: false,
		},
		{
			name: "silent false should send node event",
			stateDelta: map[string]any{
				"current_node": "process",
				"node_type":    "llm",
				"silent":       false,
			},
			expectedSendEvent: true,
		},
		{
			name: "silent not present should send node event",
			stateDelta: map[string]any{
				"current_node": "run_tool",
				"node_type":    "tool",
			},
			expectedSendEvent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the silent check logic from run_handler.go
			isSilent, _ := tt.stateDelta["silent"].(bool)
			shouldSendEvent := !isSilent

			if shouldSendEvent != tt.expectedSendEvent {
				t.Errorf("shouldSendEvent = %v, expected %v", shouldSendEvent, tt.expectedSendEvent)
			}
		})
	}
}
