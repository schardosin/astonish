package agent

import (
	"encoding/json"
	"fmt"
	"log"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// maxToolResponseBytes is the maximum size of a single tool response before
// it gets truncated. 50KB is generous enough for most useful tool outputs
// while preventing oversized responses (e.g., file_tree on /) from blowing
// up the model's context window and causing 400 errors.
const maxToolResponseBytes = 50 * 1024 // 50KB

// TruncateToolResponsesCallback returns a BeforeModelCallback that scans the
// conversation history for oversized FunctionResponse parts and truncates them.
// This is a safety net that prevents a single large tool response from causing
// a 400 Bad Request when the payload exceeds the model's limits.
//
// The truncation replaces the original response map with a trimmed version
// that includes the first portion of the content plus a truncation notice.
func TruncateToolResponsesCallback() llmagent.BeforeModelCallback {
	return func(_ adkagent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		if req == nil || len(req.Contents) == 0 {
			return nil, nil
		}

		for _, content := range req.Contents {
			if content == nil {
				continue
			}
			for _, part := range content.Parts {
				if part == nil || part.FunctionResponse == nil {
					continue
				}
				truncateFunctionResponse(part)
			}
		}

		return nil, nil // proceed with (potentially truncated) contents
	}
}

// truncateFunctionResponse checks if a FunctionResponse part's serialized
// size exceeds the limit and replaces it with a truncated version if so.
func truncateFunctionResponse(part *genai.Part) {
	fr := part.FunctionResponse
	if fr == nil || fr.Response == nil {
		return
	}

	// Serialize to check size
	data, err := json.Marshal(fr.Response)
	if err != nil {
		return
	}

	if len(data) <= maxToolResponseBytes {
		return // within limits
	}

	originalSize := len(data)
	toolName := fr.Name

	// Truncate the serialized JSON to the limit, then wrap in a new response
	truncated := string(data[:maxToolResponseBytes])

	log.Printf("[tool-truncate] Truncating %s response from %d to %d bytes",
		toolName, originalSize, maxToolResponseBytes)

	fr.Response = map[string]any{
		"output": truncated,
		"_truncated": fmt.Sprintf(
			"Response truncated from %d to %d bytes. Use more specific queries to get smaller results.",
			originalSize, maxToolResponseBytes,
		),
	}
}
