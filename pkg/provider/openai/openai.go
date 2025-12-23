package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/sashabaranov/go-openai"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Provider implements model.LLM for OpenAI.
type Provider struct {
	client              *openai.Client
	model               string
	supportsJSONMode    bool
	maxCompletionTokens int // If > 0, sets max_completion_tokens in the request
}

// NewProvider creates a new OpenAI provider.
func NewProvider(client *openai.Client, modelName string, supportsJSONMode bool) *Provider {
	return &Provider{
		client:           client,
		model:            modelName,
		supportsJSONMode: supportsJSONMode,
	}
}

// NewProviderWithMaxTokens creates a new OpenAI provider with explicit max_completion_tokens.
// This is needed for providers like OpenRouter where we fetch the limit from API metadata.
func NewProviderWithMaxTokens(client *openai.Client, modelName string, supportsJSONMode bool, maxCompletionTokens int) *Provider {
	return &Provider{
		client:              client,
		model:               modelName,
		supportsJSONMode:    supportsJSONMode,
		maxCompletionTokens: maxCompletionTokens,
	}
}

// GenerateContent implements model.LLM.
func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages := p.toOpenAIMessages(req)

		// Extract tools if present
		var tools []openai.Tool
		if req.Config != nil && len(req.Config.Tools) > 0 {
			for _, t := range req.Config.Tools {
				for _, fd := range t.FunctionDeclarations {
					tools = append(tools, openai.Tool{
						Type: openai.ToolTypeFunction,
						Function: &openai.FunctionDefinition{
							Name:        fd.Name,
							Description: fd.Description,
							Parameters:  fd.ParametersJsonSchema,
						},
					})
				}
			}
		}

		openAIReq := openai.ChatCompletionRequest{
			Model:    p.model,
			Messages: messages,
			Tools:    tools,
		}

		// Apply max_completion_tokens if configured
		// This is critical for OpenRouter to avoid their low defaults
		if p.maxCompletionTokens > 0 {
			openAIReq.MaxCompletionTokens = p.maxCompletionTokens
		}

		if req.Config != nil && len(req.Config.StopSequences) > 0 {
			openAIReq.Stop = req.Config.StopSequences
		}

		// Check for JSON mode request
		// Note: Some providers (Groq, Google) do not support JSON mode combined with tools.
		// If tools are present, we prioritize tools and disable JSON mode enforcement.
		if p.supportsJSONMode && req.Config != nil && req.Config.ResponseMIMEType == "application/json" && len(tools) == 0 {
			openAIReq.ResponseFormat = &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			}
		}

		if streaming {
			stream, err := p.client.CreateChatCompletionStream(ctx, openAIReq)
			if err != nil {
				yield(nil, err)
				return
			}
			defer stream.Close()

			// Accumulate tool calls for streaming
			toolCallAccumulator := make(map[int]*openai.ToolCall)

			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					yield(nil, err)
					return
				}

				// Handle tool call deltas
				if len(resp.Choices) > 0 {
					choice := resp.Choices[0]

					// Accumulate tool calls
					for _, tc := range choice.Delta.ToolCalls {
						if tc.Index != nil {
							idx := *tc.Index
							if _, exists := toolCallAccumulator[idx]; !exists {
								toolCallAccumulator[idx] = &openai.ToolCall{
									Index: tc.Index,
									Function: openai.FunctionCall{
										Name:      tc.Function.Name,
										Arguments: tc.Function.Arguments,
									},
									ID:   tc.ID,
									Type: tc.Type,
								}
							} else {
								// Update existing
								if tc.Function.Name != "" {
									toolCallAccumulator[idx].Function.Name += tc.Function.Name
								}
								if tc.Function.Arguments != "" {
									toolCallAccumulator[idx].Function.Arguments += tc.Function.Arguments
								}
								if tc.ID != "" {
									toolCallAccumulator[idx].ID = tc.ID
								}
							}
						}
					}

					// Check for finish reason
					if choice.FinishReason == openai.FinishReasonToolCalls ||
						(choice.FinishReason == openai.FinishReasonStop && len(toolCallAccumulator) > 0) {

						// Emit all accumulated tool calls
						var parts []*genai.Part

						// Sort by index to maintain order? Map iteration is random.
						// Let's just iterate.
						for _, tc := range toolCallAccumulator {
							var args map[string]any
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
								// Try to recover or just use empty
								args = make(map[string]any)
							}

							parts = append(parts, &genai.Part{
								FunctionCall: &genai.FunctionCall{
									Name: tc.Function.Name,
									Args: args,
									ID:   tc.ID,
								},
							})
						}

						if len(parts) > 0 {
							yield(&model.LLMResponse{
								Content: &genai.Content{
									Role:  "model",
									Parts: parts,
								},
							}, nil)
						}
						return
					}
				}

				llmResp := p.toLLMResponseStream(resp)
				// Only yield if there's content (ignore empty tool call deltas from toLLMResponseStream)
				if llmResp.Content != nil && len(llmResp.Content.Parts) > 0 {
					if !yield(llmResp, nil) {
						return
					}
				}
			}
		} else {
			resp, err := p.client.CreateChatCompletion(ctx, openAIReq)
			if err != nil {
				yield(nil, err)
				return
			}
			llmResp := p.toLLMResponse(resp)
			yield(llmResp, nil)
		}
	}
}

// Name implements model.LLM.
func (p *Provider) Name() string {
	return p.model
}

func (p *Provider) toOpenAIMessages(req *model.LLMRequest) []openai.ChatCompletionMessage {
	var messages []openai.ChatCompletionMessage

	// System instruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var sb strings.Builder
		for _, part := range req.Config.SystemInstruction.Parts {
			sb.WriteString(part.Text)
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: sb.String(),
		})
	}

	// Track tool call IDs to map responses back to calls
	// Map from function name to tool call ID
	lastToolCallIDs := make(map[string]string)

	for _, c := range req.Contents {
		role := openai.ChatMessageRoleUser
		if c.Role == "model" {
			role = openai.ChatMessageRoleAssistant
		} else if c.Role == "function" {
			role = openai.ChatMessageRoleTool
		}

		// Check if it contains FunctionResponse, override role to Tool
		// This is necessary because ADK might label it as 'user'
		for _, part := range c.Parts {
			if part.FunctionResponse != nil {
				role = openai.ChatMessageRoleTool
				break
			}
		}

		if role == openai.ChatMessageRoleTool {
			// Handle tool outputs
			for _, part := range c.Parts {
				if part.FunctionResponse != nil {
					var content string
					// Marshal response to JSON string for better LLM comprehension
					contentBytes, err := json.Marshal(part.FunctionResponse.Response)
					if err != nil {
						// Fallback to string representation if marshaling fails
						content = fmt.Sprintf("%v", part.FunctionResponse.Response)
					} else {
						content = string(contentBytes)
					}

					// Response is map[string]any
					m := part.FunctionResponse.Response
					if res, ok := m["result"]; ok {
						// If there's a specific "result" key, prefer that, but still JSON encode it if it's complex
						if resStr, ok := res.(string); ok {
							content = resStr
						} else {
							resBytes, _ := json.Marshal(res)
							content = string(resBytes)
						}
					}

					// Use real ID if available
					id := part.FunctionResponse.ID
					if id == "" {
						// Fallback: Try to find matching ID from previous calls
						if lastID, ok := lastToolCallIDs[part.FunctionResponse.Name]; ok {
							id = lastID
						} else {
							id = "call_" + part.FunctionResponse.Name
						}
					}

					messages = append(messages, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    content,
						ToolCallID: id,
					})
				}
			}
		} else {
			// User or Assistant
			var sb strings.Builder
			var toolCalls []openai.ToolCall

			for _, part := range c.Parts {
				if part.Text != "" {
					sb.WriteString(part.Text)
				}
				if part.FunctionCall != nil {
					// Marshal args to JSON string
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)

					// Use real ID if available
					id := part.FunctionCall.ID
					if id == "" {
						id = "call_" + part.FunctionCall.Name
					}

					// Store ID for matching response
					lastToolCallIDs[part.FunctionCall.Name] = id

					toolCalls = append(toolCalls, openai.ToolCall{
						ID:   id,
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(argsBytes),
						},
					})
				}
			}

			msg := openai.ChatCompletionMessage{
				Role:    role,
				Content: sb.String(),
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			messages = append(messages, msg)
		}
	}
	return messages
}

func (p *Provider) toLLMResponse(resp openai.ChatCompletionResponse) *model.LLMResponse {
	if len(resp.Choices) == 0 {
		return &model.LLMResponse{}
	}
	choice := resp.Choices[0]

	var parts []*genai.Part
	if choice.Message.Content != "" {
		parts = append(parts, &genai.Part{Text: choice.Message.Content})
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			args = make(map[string]any)
		}
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: args,
				ID:   tc.ID,
			},
		})
	}

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
	}
}

func (p *Provider) toLLMResponseStream(resp openai.ChatCompletionStreamResponse) *model.LLMResponse {
	if len(resp.Choices) == 0 {
		return &model.LLMResponse{}
	}
	choice := resp.Choices[0]

	var parts []*genai.Part
	if choice.Delta.Content != "" {
		parts = append(parts, &genai.Part{Text: choice.Delta.Content})
	}

	// Note: We do NOT handle tool calls here anymore, as they are accumulated in GenerateContent

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
	}
}
