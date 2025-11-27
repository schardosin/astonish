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
	client *openai.Client
	model  string
}

// NewProvider creates a new OpenAI provider.
func NewProvider(client *openai.Client, modelName string) *Provider {
	return &Provider{
		client: client,
		model:  modelName,
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

		if streaming {
			stream, err := p.client.CreateChatCompletionStream(ctx, openAIReq)
			if err != nil {
				yield(nil, err)
				return
			}
			defer stream.Close()

			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					yield(nil, err)
					return
				}

				llmResp := p.toLLMResponseStream(resp)
				if !yield(llmResp, nil) {
					return
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

	for _, c := range req.Contents {
		role := openai.ChatMessageRoleUser
		if c.Role == "model" {
			role = openai.ChatMessageRoleAssistant
		} else if c.Role == "function" {
			role = openai.ChatMessageRoleTool
		}

		if role == openai.ChatMessageRoleTool {
			// Handle tool outputs
			for _, part := range c.Parts {
				if part.FunctionResponse != nil {
					content := fmt.Sprintf("%v", part.FunctionResponse.Response)
					// Response is map[string]any
					m := part.FunctionResponse.Response
					if res, ok := m["result"]; ok {
						content = fmt.Sprintf("%v", res)
					}
					
					messages = append(messages, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    content,
						ToolCallID: "call_" + part.FunctionResponse.Name, // We need real IDs, but for now...
						// TODO: We need to store and retrieve ToolCallIDs to make this work properly with OpenAI
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
					
					toolCalls = append(toolCalls, openai.ToolCall{
						ID:   "call_" + part.FunctionCall.Name, // Dummy ID
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name: part.FunctionCall.Name,
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
		// TODO: Unmarshal arguments
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: map[string]any{}, // Placeholder
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
	
	for _, tc := range choice.Delta.ToolCalls {
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: map[string]any{}, // Placeholder
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
