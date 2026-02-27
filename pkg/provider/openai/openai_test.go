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
			name: "assistant with tool_calls not merged",
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
