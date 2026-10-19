package sap

import (
	"encoding/json"
	"fmt"

	"github.com/SAP/astonish/pkg/provider/bedrock"
)

const bedrockContinuationInstruction = "The response stream was interrupted by a transport failure. The preceding assistant text is the exact text already delivered. Continue from where it stopped without repeating it or restarting the task. Any tool calls from the interrupted response were discarded and were NOT executed; regenerate any still-needed calls with complete arguments. Tools with results earlier in the conversation have already executed; do not repeat those actions."

// Preserve the original serialized request fields and completed tool history.
// Only ordinary delivered text is carried across; unfinished tool JSON and
// private reasoning are never converted into a request or executable call.
// End with a user message: newer Claude models reject assistant prefilling.
func bedrockContinuationPayload(original []byte, text string) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(original, &request); err != nil {
		return nil, fmt.Errorf("decode Bedrock continuation request: %w", err)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(request["messages"], &messages); err != nil {
		return nil, fmt.Errorf("decode Bedrock continuation messages: %w", err)
	}
	if messages == nil {
		return nil, fmt.Errorf("decode Bedrock continuation messages: messages must be a non-null array")
	}
	for _, message := range []bedrock.Message{{Role: "assistant", Content: text}, {Role: "user", Content: bedrockContinuationInstruction}} {
		data, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, data)
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	request["messages"] = data
	return json.Marshal(request)
}
