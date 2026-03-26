package agent

import (
	"fmt"
	"sync"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// minToolCallsForNudge is the minimum number of tool calls in a turn
// before the memory nudge activates. Trivial tasks (1-2 tool calls)
// rarely produce knowledge worth saving.
const minToolCallsForNudge = 3

// memoryNudgeText is appended to the last user message when the nudge
// fires. It reminds the model to save durable knowledge before concluding.
const memoryNudgeText = "\n\n[Memory Reminder] You used multiple tools to complete this task. " +
	"Before giving your final response, consider: did you discover any durable knowledge " +
	"(workarounds, file paths, API patterns, working commands after initial failures)? " +
	"If so, save it now using memory_save with a descriptive topic file. " +
	"If nothing new was learned, proceed with your response."

// MemoryNudgeCallback returns a BeforeModelCallback that reminds the LLM
// to save knowledge to persistent memory after non-trivial tasks.
//
// The nudge activates once per Run() invocation when all conditions are met:
//   - The conversation contains >= minToolCallsForNudge function calls
//   - None of those calls are memory_save (the model hasn't already saved)
//   - The nudge hasn't fired yet this turn (one-shot)
//
// When activated, a short reminder is appended to the last user message's
// Parts, using the same ephemeral injection pattern as EphemeralKnowledgeCallback.
// The model can then choose to call memory_save or proceed directly.
func MemoryNudgeCallback(debugMode bool) llmagent.BeforeModelCallback {
	var (
		fired bool
		mu    sync.Mutex
	)

	return func(_ adkagent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		if req == nil || len(req.Contents) == 0 {
			return nil, nil
		}

		mu.Lock()
		if fired {
			mu.Unlock()
			return nil, nil
		}
		mu.Unlock()

		// Scan conversation: count function calls, check for memory_save
		totalFunctionCalls := 0
		memorySaveCalled := false

		for _, content := range req.Contents {
			if content == nil {
				continue
			}
			for _, part := range content.Parts {
				if part == nil {
					continue
				}
				if part.FunctionCall != nil {
					totalFunctionCalls++
					if part.FunctionCall.Name == "memory_save" {
						memorySaveCalled = true
					}
				}
			}
		}

		// Skip if task is trivial or memory was already saved
		if totalFunctionCalls < minToolCallsForNudge || memorySaveCalled {
			return nil, nil
		}

		// Fire the nudge (one-shot)
		mu.Lock()
		if fired {
			mu.Unlock()
			return nil, nil
		}
		fired = true
		mu.Unlock()

		// Find the last user-role Content and append the nudge
		lastUserIdx := -1
		for i := len(req.Contents) - 1; i >= 0; i-- {
			if req.Contents[i] != nil && req.Contents[i].Role == "user" {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx < 0 {
			return nil, nil
		}

		nudgePart := &genai.Part{Text: memoryNudgeText}
		req.Contents[lastUserIdx].Parts = append(req.Contents[lastUserIdx].Parts, nudgePart)

		if debugMode {
			fmt.Printf("[Chat DEBUG] Memory nudge injected (total tool calls: %d)\n", totalFunctionCalls)
		}

		return nil, nil
	}
}
