package bedrock

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Request represents the Bedrock request payload for Anthropic models.
type Request struct {
	AnthropicVersion string    `json:"anthropic_version"`
	MaxTokens        int       `json:"max_tokens"`
	Messages         []Message `json:"messages"`
	System           string    `json:"system,omitempty"`
	Temperature      float64   `json:"temperature,omitempty"`
	Tools            []Tool    `json:"tools,omitempty"`
}

// Message represents a message in the Bedrock conversation.
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or array of content blocks
}

// Tool represents a tool definition in Bedrock.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ContentBlock represents a block of content in a Bedrock message.
type ContentBlock struct {
	Type      string                  `json:"type"`
	Text      string                  `json:"text,omitempty"`
	ID        string                  `json:"id,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Input     *map[string]interface{} `json:"input,omitempty"` // Pointer so we can control when it's included
	ToolUseID string                  `json:"tool_use_id,omitempty"`
	Content   string                  `json:"content,omitempty"`
}

// Response represents the Bedrock response payload.
type Response struct {
	Content []ContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

// ConvertRequest converts an ADK LLMRequest to a Bedrock Request.
// maxTokens can be 0 to use the default (8192)
func ConvertRequest(req *model.LLMRequest, maxTokens int) (*Request, error) {
	if maxTokens <= 0 {
		maxTokens = 8192 // Default fallback
	}
	bedrockReq := &Request{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        maxTokens,
		Messages:         make([]Message, 0),
		Temperature:      0.7,
	}

	// Convert ADK messages to Bedrock messages
	for _, content := range req.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}

		// Check if this message contains function calls or responses
		hasFunctionCall := false
		hasFunctionResponse := false
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				hasFunctionCall = true
			}
			if part.FunctionResponse != nil {
				hasFunctionResponse = true
			}
		}

		if hasFunctionCall {
			// Convert function calls to tool_use blocks
			var contentBlocks []ContentBlock
			for _, part := range content.Parts {
				if part.FunctionCall != nil {
					// Ensure Input is never nil - Bedrock requires this field for tool_use
					input := part.FunctionCall.Args
					if input == nil {
						input = make(map[string]interface{})
					}
					contentBlocks = append(contentBlocks, ContentBlock{
						Type:  "tool_use",
						ID:    part.FunctionCall.ID,
						Name:  part.FunctionCall.Name,
						Input: &input, // Use pointer
					})
				}
			}
			bedrockReq.Messages = append(bedrockReq.Messages, Message{
				Role:    role,
				Content: contentBlocks,
			})
		} else if hasFunctionResponse {
			// Convert function responses to tool_result blocks
			var contentBlocks []ContentBlock
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					resultJSON, _ := json.Marshal(part.FunctionResponse.Response)
					contentBlocks = append(contentBlocks, ContentBlock{
						Type:      "tool_result",
						ToolUseID: part.FunctionResponse.ID,
						Content:   string(resultJSON),
					})
				}
			}
			bedrockReq.Messages = append(bedrockReq.Messages, Message{
				Role:    "user", // Tool results must be from user role
				Content: contentBlocks,
			})
		} else {
			// Regular text message
			var textBuilder strings.Builder
			for _, part := range content.Parts {
				if part.Text != "" {
					textBuilder.WriteString(part.Text)
				}
			}
			bedrockReq.Messages = append(bedrockReq.Messages, Message{
				Role:    role,
				Content: textBuilder.String(),
			})
		}
	}

	// Handle system instruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var sysBuilder strings.Builder
		for _, part := range req.Config.SystemInstruction.Parts {
			sysBuilder.WriteString(part.Text)
		}
		bedrockReq.System = sysBuilder.String()
	}

	// Handle tools
	if req.Config != nil && len(req.Config.Tools) > 0 {
		for _, tool := range req.Config.Tools {
			for _, funcDecl := range tool.FunctionDeclarations {
				bedrockTool := Tool{
					Name:        funcDecl.Name,
					Description: funcDecl.Description,
					InputSchema: make(map[string]interface{}),
				}

				// Convert JSON schema to Bedrock format
				if funcDecl.Parameters != nil {
					bedrockTool.InputSchema["type"] = "object"
					
					// Marshal and sanitize
					schemaBytes, _ := json.Marshal(funcDecl.Parameters) // Error checked at source usually
					var schemaMap map[string]interface{}
					if err := json.Unmarshal(schemaBytes, &schemaMap); err == nil {
						sanitizeSchema(schemaMap)
						if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
							bedrockTool.InputSchema["properties"] = props
						}
						if required, ok := schemaMap["required"].([]interface{}); ok {
							bedrockTool.InputSchema["required"] = required
						}
					}
				} else if funcDecl.ParametersJsonSchema != nil {
					bedrockTool.InputSchema["type"] = "object"

					// Marshal and sanitize
					schemaBytes, _ := json.Marshal(funcDecl.ParametersJsonSchema)
					var schemaMap map[string]interface{}
					if err := json.Unmarshal(schemaBytes, &schemaMap); err == nil {
						sanitizeSchema(schemaMap)
						if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
							bedrockTool.InputSchema["properties"] = props
						}
						if required, ok := schemaMap["required"].([]interface{}); ok {
							bedrockTool.InputSchema["required"] = required
						}
					}
				}

				bedrockReq.Tools = append(bedrockReq.Tools, bedrockTool)
			}
		}
	}

	return bedrockReq, nil
}

// sanitizeSchema recursively fixes schema types (upper -> lower) for Bedrock
func sanitizeSchema(schema map[string]interface{}) {
	// Fix type case (GenAI uses "STRING", JSON Schema uses "string")
	if t, ok := schema["type"].(string); ok {
		schema["type"] = strings.ToLower(t)
	}
	
	// Recurse into properties
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for _, prop := range props {
			if propMap, ok := prop.(map[string]interface{}); ok {
				sanitizeSchema(propMap)
			}
		}
	}
	
	// Recurse into array items
	if items, ok := schema["items"].(map[string]interface{}); ok {
		sanitizeSchema(items)
	}
}

// ParseResponse converts a Bedrock Response body to an ADK LLMResponse.
func ParseResponse(body []byte) (*model.LLMResponse, error) {
	var bedrockResp Response
	if err := json.Unmarshal(body, &bedrockResp); err != nil {
		return nil, fmt.Errorf("failed to decode bedrock response: %w", err)
	}

	// Convert back to ADK response
	var parts []*genai.Part
	for _, block := range bedrockResp.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, &genai.Part{Text: block.Text})
			}
		case "tool_use":
			// Convert tool_use to function call
			var args map[string]interface{}
			if block.Input != nil {
				args = *block.Input
			}
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   block.ID,
					Name: block.Name,
					Args: args,
				},
			})
		}
	}

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
	}, nil
}

// ParseStream parses a Bedrock SSE stream and yields ADK LLMResponses.
func ParseStream(reader io.Reader) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		bufReader := bufio.NewReader(reader)
		var currentToolUse *ContentBlock
		var jsonBuffer strings.Builder

		for {
			line, err := bufReader.ReadBytes('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				yield(nil, err)
				return
			}

			// Skip empty lines
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			// SSE format: "data: {...}"
			if bytes.HasPrefix(line, []byte("data: ")) {
				data := bytes.TrimPrefix(line, []byte("data: "))

				// Parse the JSON chunk - handle multiple event types
				var chunk struct {
					Type         string `json:"type"`
					Index        int    `json:"index"`
					ContentBlock *struct {
						Type  string                 `json:"type"`
						ID    string                 `json:"id"`
						Name  string                 `json:"name"`
						Input map[string]interface{} `json:"input"`
					} `json:"content_block"`
					Delta *struct {
						Type        string `json:"type"`
						Text        string `json:"text"`
						PartialJSON string `json:"partial_json"`
					} `json:"delta"`
				}

				if err := json.Unmarshal(data, &chunk); err != nil {
					// Skip malformed chunks
					continue
				}

				// Handle different chunk types
				switch chunk.Type {
				case "content_block_start":
					// Start of a new content block (text or tool_use)
					if chunk.ContentBlock != nil && chunk.ContentBlock.Type == "tool_use" {
						inputMap := make(map[string]interface{})
						currentToolUse = &ContentBlock{
							Type:  "tool_use",
							ID:    chunk.ContentBlock.ID,
							Name:  chunk.ContentBlock.Name,
							Input: &inputMap,
						}
						jsonBuffer.Reset()
					}

				case "content_block_delta":
					if chunk.Delta != nil {
						if chunk.Delta.Text != "" {
							// Text delta
							if !yield(&model.LLMResponse{
								Content: &genai.Content{
									Role:  "model",
									Parts: []*genai.Part{{Text: chunk.Delta.Text}},
								},
							}, nil) {
								return
							}
						} else if chunk.Delta.PartialJSON != "" && currentToolUse != nil {
							// Tool input delta - accumulate JSON string
							jsonBuffer.WriteString(chunk.Delta.PartialJSON)
						}
					}

				case "content_block_stop":
					// End of content block - if it's a tool use, yield it
					if currentToolUse != nil {
						// Parse accumulated JSON
						if jsonBuffer.Len() > 0 {
							var args map[string]interface{}
							if err := json.Unmarshal([]byte(jsonBuffer.String()), &args); err == nil {
								currentToolUse.Input = &args
							}
						}

						// Ensure Args is never nil - ADK/Bedrock requires this field
						var args map[string]interface{}
						if currentToolUse.Input != nil {
							args = *currentToolUse.Input
						}
						if args == nil {
							args = make(map[string]interface{})
						}

						if !yield(&model.LLMResponse{
							Content: &genai.Content{
								Role: "model",
								Parts: []*genai.Part{{
									FunctionCall: &genai.FunctionCall{
										ID:   currentToolUse.ID,
										Name: currentToolUse.Name,
										Args: args,
									},
								}},
							},
						}, nil) {
							return
						}
						currentToolUse = nil
						jsonBuffer.Reset()
					}
				}
			}
		}
	}
}
