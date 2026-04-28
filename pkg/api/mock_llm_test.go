package api

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// MockLLM implements model.LLM for integration testing.
// It returns pre-programmed responses in order, supporting text, function calls,
// streaming, errors, and usage metadata. Thread-safe for concurrent access.
//
// When funcLLM is set (turnIndex == -1), it delegates to the funcLLM for
// custom GenerateContent behavior (used by BlockingLLM, TruncationMockLLM).
type MockLLM struct {
	mu        sync.Mutex
	turns     []*MockTurn         // ordered queue of responses
	turnIndex int                 // next turn to return; -1 = use funcLLM
	Calls     []*model.LLMRequest // recorded calls for assertion
	funcLLM   funcLLM             // custom behavior (nil = use turns)
}

// MockTurn represents a single LLM response turn.
// For streaming, use multiple MockTurns with Partial=true followed by a final one.
type MockTurn struct {
	Parts         []*genai.Part
	UsageMetadata *genai.GenerateContentResponseUsageMetadata
	Partial       bool
	TurnComplete  bool
	ErrorCode     string
	ErrorMessage  string
}

var _ model.LLM = (*MockLLM)(nil)

func (m *MockLLM) Name() string { return "mock_llm" }

func (m *MockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	m.mu.Lock()
	m.Calls = append(m.Calls, req)

	// Delegate to custom funcLLM if set
	if m.funcLLM != nil {
		m.mu.Unlock()
		return m.funcLLM.GenerateContent(ctx, req, stream)
	}
	m.mu.Unlock()

	return func(yield func(*model.LLMResponse, error) bool) {
		m.mu.Lock()

		if m.turnIndex >= len(m.turns) {
			m.mu.Unlock()
			yield(nil, fmt.Errorf("MockLLM: no more turns available (index=%d, total=%d)", m.turnIndex, len(m.turns)))
			return
		}

		// For streaming, yield consecutive partial turns then the final one.
		// For non-streaming, yield a single turn.
		if stream {
			// Yield all turns from current index that are marked Partial,
			// then yield the next non-partial turn (the final one for this call).
			for m.turnIndex < len(m.turns) {
				turn := m.turns[m.turnIndex]
				m.turnIndex++

				// Handle error turns in streaming mode
				if turn.ErrorCode != "" || turn.ErrorMessage != "" {
					m.mu.Unlock()
					yield(nil, fmt.Errorf("%s: %s", turn.ErrorCode, turn.ErrorMessage))
					return
				}

				resp := turnToResponse(turn)
				m.mu.Unlock()
				if !yield(resp, nil) {
					return
				}
				m.mu.Lock()

				if !turn.Partial {
					break
				}
			}
			m.mu.Unlock()
		} else {
			turn := m.turns[m.turnIndex]
			m.turnIndex++
			m.mu.Unlock()

			if turn.ErrorCode != "" || turn.ErrorMessage != "" {
				yield(nil, fmt.Errorf("%s: %s", turn.ErrorCode, turn.ErrorMessage))
				return
			}

			resp := turnToResponse(turn)
			yield(resp, nil)
		}
	}
}

// turnToResponse converts a MockTurn into a model.LLMResponse.
func turnToResponse(turn *MockTurn) *model.LLMResponse {
	content := &genai.Content{
		Role:  "model",
		Parts: turn.Parts,
	}
	return &model.LLMResponse{
		Content:       content,
		Partial:       turn.Partial,
		TurnComplete:  turn.TurnComplete,
		UsageMetadata: turn.UsageMetadata,
	}
}

// --- Turn Builders ---

// TextTurn creates a simple text response turn.
func TextTurn(text string) *MockTurn {
	return &MockTurn{
		Parts:        []*genai.Part{{Text: text}},
		TurnComplete: true,
	}
}

// TextTurnWithUsage creates a text turn with usage metadata.
func TextTurnWithUsage(text string, inputTokens, outputTokens, totalTokens int32) *MockTurn {
	return &MockTurn{
		Parts:        []*genai.Part{{Text: text}},
		TurnComplete: true,
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     inputTokens,
			CandidatesTokenCount: outputTokens,
			TotalTokenCount:      totalTokens,
		},
	}
}

// ToolCallTurn creates a turn that requests a function call.
func ToolCallTurn(name string, args map[string]any) *MockTurn {
	return &MockTurn{
		Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: name, Args: args}},
		},
		TurnComplete: true,
	}
}

// MultiToolCallTurn creates a turn with multiple function calls.
func MultiToolCallTurn(calls ...*genai.FunctionCall) *MockTurn {
	parts := make([]*genai.Part, len(calls))
	for i, call := range calls {
		parts[i] = &genai.Part{FunctionCall: call}
	}
	return &MockTurn{
		Parts:        parts,
		TurnComplete: true,
	}
}

// StreamChunk creates a partial streaming text turn.
func StreamChunk(text string) *MockTurn {
	return &MockTurn{
		Parts:        []*genai.Part{{Text: text}},
		Partial:      true,
		TurnComplete: false,
	}
}

// StreamFinal creates the final streaming text turn (non-partial).
func StreamFinal(text string) *MockTurn {
	return &MockTurn{
		Parts:        []*genai.Part{{Text: text}},
		Partial:      false,
		TurnComplete: true,
	}
}

// ErrorTurn creates a turn that simulates an LLM error.
func ErrorTurn(code, message string) *MockTurn {
	return &MockTurn{
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

// EmptyTurn creates a turn with no content (triggers empty response fallback).
func EmptyTurn() *MockTurn {
	return &MockTurn{
		Parts:        []*genai.Part{},
		TurnComplete: true,
	}
}

// NewMockLLM creates a MockLLM with the given turns.
func NewMockLLM(turns ...*MockTurn) *MockLLM {
	return &MockLLM{
		turns: turns,
	}
}

// --- Specialized Mock LLMs ---

// TruncationMockLLM is a mock used internally by NewTruncationRetryLLM.
type TruncationMockLLM struct {
	mu           sync.Mutex
	callCount    int
	partialText  string
	retryContent string
}

// NewTruncationRetryLLM creates a MockLLM that simulates stream truncation
// on the first call and returns a successful response on the second (retry).
func NewTruncationRetryLLM(partialText, retryText string) *MockLLM {
	inner := &TruncationMockLLM{
		partialText:  partialText,
		retryContent: retryText,
	}
	return &MockLLM{
		turns: nil, // unused — GenerateContent is overridden via the wrapper
		Calls: nil,
		mu:    sync.Mutex{},
		// We embed the custom behavior by replacing the MockLLM with a wrapper
		// that delegates to TruncationMockLLM. However, since MockLLM's
		// GenerateContent checks turns, we need to use a different approach.
		// Instead, we'll construct a proper MockLLM by using the custom func LLM.
		turnIndex: -1, // sentinel: use funcLLM path
		funcLLM:   inner,
	}
}

// BlockingLLM is a MockLLM that blocks until its context is cancelled.
// Used for testing cancellation behavior.
type BlockingLLM = MockLLM

// NewBlockingLLM creates a MockLLM that blocks on GenerateContent until cancelled.
func NewBlockingLLM() *MockLLM {
	blocking := &blockingLLMInner{}
	return &MockLLM{
		turnIndex: -1, // sentinel: use funcLLM path
		funcLLM:   blocking,
	}
}

// funcLLM is an internal interface for custom GenerateContent behavior.
type funcLLM interface {
	GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error]
}

func (t *TruncationMockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		t.mu.Lock()
		t.callCount++
		call := t.callCount
		t.mu.Unlock()

		if call == 1 {
			// First call: yield partial text then truncation error
			if t.partialText != "" {
				yield(&model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: t.partialText}},
					},
					Partial: true,
				}, nil)
			}
			yield(nil, fmt.Errorf("LLM stream ended without a finish_reason — the response was likely truncated"))
		} else {
			// Subsequent calls: successful response
			yield(&model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: t.retryContent}},
				},
				TurnComplete: true,
			}, nil)
		}
	}
}

// blockingLLMInner blocks until context is cancelled.
type blockingLLMInner struct{}

func (b *blockingLLMInner) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		<-ctx.Done()
		// Context cancelled — yield nothing, just return
	}
}
