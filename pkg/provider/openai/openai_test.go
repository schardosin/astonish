package openai

import (
	"testing"

	openailib "github.com/sashabaranov/go-openai"
)

func TestMergeConsecutiveSameRole(t *testing.T) {
	tests := []struct {
		name     string
		input    []openailib.ChatCompletionMessage
		expected []openailib.ChatCompletionMessage
	}{
		{
			name:     "empty",
			input:    nil,
			expected: nil,
		},
		{
			name: "single message unchanged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
			},
		},
		{
			name: "alternating roles unchanged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "hi"},
				{Role: openailib.ChatMessageRoleUser, Content: "how are you"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "hi"},
				{Role: openailib.ChatMessageRoleUser, Content: "how are you"},
			},
		},
		{
			name: "consecutive user messages merged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "ready now"},
				{Role: openailib.ChatMessageRoleUser, Content: "continue"},
				{Role: openailib.ChatMessageRoleUser, Content: "please respond"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "ready now\ncontinue\nplease respond"},
			},
		},
		{
			name: "consecutive user messages between assistant responses",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleAssistant, Content: "ok"},
				{Role: openailib.ChatMessageRoleUser, Content: "msg1"},
				{Role: openailib.ChatMessageRoleUser, Content: "msg2"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "got it"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleAssistant, Content: "ok"},
				{Role: openailib.ChatMessageRoleUser, Content: "msg1\nmsg2"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "got it"},
			},
		},
		{
			name: "tool messages not merged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleTool, Content: "result1", ToolCallID: "call_1"},
				{Role: openailib.ChatMessageRoleTool, Content: "result2", ToolCallID: "call_2"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleTool, Content: "result1", ToolCallID: "call_1"},
				{Role: openailib.ChatMessageRoleTool, Content: "result2", ToolCallID: "call_2"},
			},
		},
		{
			name: "assistant tool_calls then text merged (Case 3)",
			input: []openailib.ChatCompletionMessage{
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn1"}},
					},
				},
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "done",
				},
			},
			expected: []openailib.ChatCompletionMessage{
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "done",
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn1"}},
					},
				},
			},
		},
		{
			name: "assistant text then tool_calls merged (Case 2 - streaming fix)",
			input: []openailib.ChatCompletionMessage{
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "Let me check that for you.",
				},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_abc123", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "shell_command", Arguments: `{"command":"free -h"}`}},
					},
				},
			},
			expected: []openailib.ChatCompletionMessage{
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "Let me check that for you.",
					ToolCalls: []openailib.ToolCall{
						{ID: "call_abc123", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "shell_command", Arguments: `{"command":"free -h"}`}},
					},
				},
			},
		},
		{
			name: "full streaming tool call round-trip",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleSystem, Content: "You are an assistant."},
				{Role: openailib.ChatMessageRoleUser, Content: "Check memory"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "I'll check the memory usage."},
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "",
					ToolCalls: []openailib.ToolCall{
						{ID: "resolve_credential:0", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "resolve_credential", Arguments: `{"name":"proxmox-ssh"}`}},
					},
				},
				{Role: openailib.ChatMessageRoleTool, Content: `{"password":"secret"}`, ToolCallID: "resolve_credential:0"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleSystem, Content: "You are an assistant."},
				{Role: openailib.ChatMessageRoleUser, Content: "Check memory"},
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "I'll check the memory usage.",
					ToolCalls: []openailib.ToolCall{
						{ID: "resolve_credential:0", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "resolve_credential", Arguments: `{"name":"proxmox-ssh"}`}},
					},
				},
				{Role: openailib.ChatMessageRoleTool, Content: `{"password":"secret"}`, ToolCallID: "resolve_credential:0"},
			},
		},
		{
			name: "two assistant tool_calls messages not merged",
			input: []openailib.ChatCompletionMessage{
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn1"}},
					},
				},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_2", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn2"}},
					},
				},
			},
			expected: []openailib.ChatCompletionMessage{
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn1"}},
					},
				},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_2", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn2"}},
					},
				},
			},
		},
		{
			name: "three consecutive assistant: text + text + tool_calls all merged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleAssistant, Content: "Thinking..."},
				{Role: openailib.ChatMessageRoleAssistant, Content: "Let me check."},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn1"}},
					},
				},
			},
			expected: []openailib.ChatCompletionMessage{
				{
					Role:    openailib.ChatMessageRoleAssistant,
					Content: "Thinking...\nLet me check.",
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "fn1"}},
					},
				},
			},
		},
		{
			name: "consecutive assistant text messages merged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleAssistant, Content: "thinking..."},
				{Role: openailib.ChatMessageRoleAssistant, Content: "here is my answer"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleAssistant, Content: "thinking...\nhere is my answer"},
			},
		},
		{
			name: "system messages not merged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleSystem, Content: "system1"},
				{Role: openailib.ChatMessageRoleSystem, Content: "system2"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleSystem, Content: "system1"},
				{Role: openailib.ChatMessageRoleSystem, Content: "system2"},
			},
		},
		{
			name: "empty content in consecutive messages",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: ""},
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
			},
			expected: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeConsecutiveSameRole(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d messages, got %d", len(tt.expected), len(result))
				return
			}
			for i := range result {
				if result[i].Role != tt.expected[i].Role {
					t.Errorf("message[%d]: expected role %q, got %q", i, tt.expected[i].Role, result[i].Role)
				}
				if result[i].Content != tt.expected[i].Content {
					t.Errorf("message[%d]: expected content %q, got %q", i, tt.expected[i].Content, result[i].Content)
				}
				if result[i].ToolCallID != tt.expected[i].ToolCallID {
					t.Errorf("message[%d]: expected tool_call_id %q, got %q", i, tt.expected[i].ToolCallID, result[i].ToolCallID)
				}
				if len(result[i].ToolCalls) != len(tt.expected[i].ToolCalls) {
					t.Errorf("message[%d]: expected %d tool_calls, got %d", i, len(tt.expected[i].ToolCalls), len(result[i].ToolCalls))
				}
			}
		})
	}
}

func TestEnsureNotEndingWithTool(t *testing.T) {
	tests := []struct {
		name        string
		input       []openailib.ChatCompletionMessage
		expectLen   int
		expectLast  string // expected role of last message
		expectExtra bool   // whether a synthetic user message was appended
	}{
		{
			name:      "empty messages unchanged",
			input:     nil,
			expectLen: 0,
		},
		{
			name: "ending with user unchanged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
			},
			expectLen:  1,
			expectLast: openailib.ChatMessageRoleUser,
		},
		{
			name: "ending with assistant unchanged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "hi there"},
			},
			expectLen:  2,
			expectLast: openailib.ChatMessageRoleAssistant,
		},
		{
			name: "ending with tool gets user appended",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "do something"},
				{Role: openailib.ChatMessageRoleAssistant, ToolCalls: []openailib.ToolCall{
					{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "my_tool", Arguments: "{}"}},
				}},
				{Role: openailib.ChatMessageRoleTool, Content: "result", ToolCallID: "call_1"},
			},
			expectLen:   4,
			expectLast:  openailib.ChatMessageRoleUser,
			expectExtra: true,
		},
		{
			name: "ending with tool after multiple tool results",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "do two things"},
				{Role: openailib.ChatMessageRoleAssistant, ToolCalls: []openailib.ToolCall{
					{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool_a", Arguments: "{}"}},
					{ID: "call_2", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool_b", Arguments: "{}"}},
				}},
				{Role: openailib.ChatMessageRoleTool, Content: "result_a", ToolCallID: "call_1"},
				{Role: openailib.ChatMessageRoleTool, Content: "result_b", ToolCallID: "call_2"},
			},
			expectLen:   5,
			expectLast:  openailib.ChatMessageRoleUser,
			expectExtra: true,
		},
		{
			name: "tool followed by user already present - unchanged",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "do something"},
				{Role: openailib.ChatMessageRoleAssistant, ToolCalls: []openailib.ToolCall{
					{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "my_tool", Arguments: "{}"}},
				}},
				{Role: openailib.ChatMessageRoleTool, Content: "result", ToolCallID: "call_1"},
				{Role: openailib.ChatMessageRoleUser, Content: "now what?"},
			},
			expectLen:  4,
			expectLast: openailib.ChatMessageRoleUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureNotEndingWithTool(tt.input)
			if len(result) != tt.expectLen {
				t.Fatalf("expected %d messages, got %d", tt.expectLen, len(result))
			}
			if tt.expectLen > 0 {
				last := result[len(result)-1]
				if last.Role != tt.expectLast {
					t.Errorf("expected last role %q, got %q", tt.expectLast, last.Role)
				}
				if tt.expectExtra && last.Content == "" {
					t.Error("expected synthetic user message to have non-empty content")
				}
			}
		})
	}
}

func TestPatchOrphanedToolCalls(t *testing.T) {
	tests := []struct {
		name      string
		input     []openailib.ChatCompletionMessage
		expectLen int
		// expectOrphanPatched: the tool_call_id that should have a synthetic response
		expectOrphanPatched []string
	}{
		{
			name:      "no messages",
			input:     nil,
			expectLen: 0,
		},
		{
			name: "no orphans - all tool_calls have responses",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "shell_command", Arguments: "{}"}},
					},
				},
				{Role: openailib.ChatMessageRoleTool, ToolCallID: "call_1", Content: "ok"},
			},
			expectLen:           3,
			expectOrphanPatched: nil,
		},
		{
			name: "orphan at end - no tool response",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "browser_snapshot", Arguments: "{}"}},
					},
				},
			},
			expectLen:           3, // original 2 + 1 synthetic tool response
			expectOrphanPatched: []string{"call_1"},
		},
		{
			name: "multiple orphans in one assistant message",
			input: []openailib.ChatCompletionMessage{
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_a", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool1", Arguments: "{}"}},
						{ID: "call_b", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool2", Arguments: "{}"}},
						{ID: "call_c", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool3", Arguments: "{}"}},
					},
				},
			},
			expectLen:           4, // 1 assistant + 3 synthetic
			expectOrphanPatched: []string{"call_a", "call_b", "call_c"},
		},
		{
			name: "partial orphan - one answered one not",
			input: []openailib.ChatCompletionMessage{
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_1", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool1", Arguments: "{}"}},
						{ID: "call_2", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "tool2", Arguments: "{}"}},
					},
				},
				{Role: openailib.ChatMessageRoleTool, ToolCallID: "call_1", Content: "ok"},
			},
			expectLen:           3, // assistant + synthetic for call_2 + existing tool for call_1
			expectOrphanPatched: []string{"call_2"},
		},
		{
			name: "orphan in middle of conversation",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "do something"},
				{
					Role: openailib.ChatMessageRoleAssistant,
					ToolCalls: []openailib.ToolCall{
						{ID: "call_old", Type: openailib.ToolTypeFunction, Function: openailib.FunctionCall{Name: "hung_tool", Arguments: "{}"}},
					},
				},
				// No tool response — then user sent another message
				{Role: openailib.ChatMessageRoleUser, Content: "?"},
			},
			expectLen:           4, // user + assistant + synthetic tool + user
			expectOrphanPatched: []string{"call_old"},
		},
		{
			name: "no tool_calls in assistant - untouched",
			input: []openailib.ChatCompletionMessage{
				{Role: openailib.ChatMessageRoleUser, Content: "hello"},
				{Role: openailib.ChatMessageRoleAssistant, Content: "hi there"},
			},
			expectLen:           2,
			expectOrphanPatched: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := patchOrphanedToolCalls(tt.input)
			if len(result) != tt.expectLen {
				t.Fatalf("expected %d messages, got %d", tt.expectLen, len(result))
			}

			// Verify that the expected orphan IDs now have tool responses
			for _, expectedID := range tt.expectOrphanPatched {
				found := false
				for _, msg := range result {
					if msg.Role == openailib.ChatMessageRoleTool && msg.ToolCallID == expectedID {
						found = true
						if msg.Content == "" {
							t.Errorf("synthetic tool response for %q should have content", expectedID)
						}
						break
					}
				}
				if !found {
					t.Errorf("expected synthetic tool response for orphan %q, not found", expectedID)
				}
			}
		})
	}
}
