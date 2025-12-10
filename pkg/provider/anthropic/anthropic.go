package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

const (
	apiURL     = "https://api.anthropic.com/v1/messages"
	apiVersion = "2023-06-01"
)

// Provider implements model.LLM for Anthropic.
type Provider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewProvider creates a new Anthropic provider.
func NewProvider(apiKey, modelName string) *Provider {
	return &Provider{
		apiKey: apiKey,
		model:  modelName,
		client: &http.Client{},
	}
}

// Name implements model.LLM.
func (p *Provider) Name() string {
	return p.model
}

// GenerateContent implements model.LLM.
func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Convert ADK request to Anthropic request
		anthropicReq, err := p.toAnthropicRequest(req, streaming)
		if err != nil {
			yield(nil, err)
			return
		}

		reqBody, err := json.Marshal(anthropicReq)
		if err != nil {
			yield(nil, err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(reqBody))
		if err != nil {
			yield(nil, err)
			return
		}

		httpReq.Header.Set("x-api-key", p.apiKey)
		httpReq.Header.Set("anthropic-version", apiVersion)
		httpReq.Header.Set("content-type", "application/json")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			yield(nil, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("anthropic api error: %s - %s", resp.Status, string(body)))
			return
		}

		if streaming {
			p.handleStream(resp.Body, yield)
		} else {
			p.handleResponse(resp.Body, yield)
		}
	}
}

func (p *Provider) handleResponse(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	var resp Response
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		yield(nil, err)
		return
	}

	// Convert Anthropic response to ADK response
	llmResp := &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
		},
	}

	for _, content := range resp.Content {
		if content.Type == "text" {
			llmResp.Content.Parts = append(llmResp.Content.Parts, &genai.Part{Text: content.Text})
		} else if content.Type == "tool_use" {
			var args map[string]any
			if err := json.Unmarshal(content.Input, &args); err != nil {
				// Handle error or use empty map
				args = make(map[string]any)
			}
			
			llmResp.Content.Parts = append(llmResp.Content.Parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					Name: content.Name,
					Args: args,
					ID:   content.ID,
				},
			})
		}
	}

	yield(llmResp, nil)
}

func (p *Provider) handleStream(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	scanner := bufio.NewScanner(body)
	
	// State for accumulating tool calls
	var currentToolID string
	var currentToolName string
	var currentToolInput strings.Builder
	var isCollectingTool bool
	var currentBlockIndex int

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				isCollectingTool = true
				currentToolID = event.ContentBlock.ID
				currentToolName = event.ContentBlock.Name
				currentToolInput.Reset()
				currentBlockIndex = event.Index
			}
		
		case "content_block_delta":
			if event.Delta != nil {
				if event.Delta.Type == "text_delta" {
					yield(&model.LLMResponse{
						Content: &genai.Content{
							Role: "model",
							Parts: []*genai.Part{
								{Text: event.Delta.Text},
							},
						},
					}, nil)
				} else if event.Delta.Type == "input_json_delta" && isCollectingTool {
					currentToolInput.WriteString(event.Delta.PartialJson)
				}
			}

		case "content_block_stop":
			if isCollectingTool && event.Index == currentBlockIndex {
				// Tool call complete
				var args map[string]any
				if err := json.Unmarshal([]byte(currentToolInput.String()), &args); err != nil {
					// Handle error
					args = make(map[string]any)
				}

				yield(&model.LLMResponse{
					Content: &genai.Content{
						Role: "model",
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: currentToolName,
									Args: args,
									ID:   currentToolID,
								},
							},
						},
					},
				}, nil)

				isCollectingTool = false
				currentToolInput.Reset()
			}
		}
	}
}

func (p *Provider) toAnthropicRequest(req *model.LLMRequest, streaming bool) (*Request, error) {
	var messages []Message
	var system string

	// Extract system instruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var sb strings.Builder
		for _, part := range req.Config.SystemInstruction.Parts {
			sb.WriteString(part.Text)
		}
		system = sb.String()
	}

	for _, c := range req.Contents {
		role := "user"
		if c.Role == "model" {
			role = "assistant"
		}
		
		var content []Content
		for _, part := range c.Parts {
			if part.Text != "" {
				content = append(content, Content{
					Type: "text",
					Text: part.Text,
				})
			}
			
			if part.FunctionCall != nil {
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				content = append(content, Content{
					Type:  "tool_use",
					ID:    part.FunctionCall.ID,
					Name:  part.FunctionCall.Name,
					Input: argsBytes,
				})
			}
			
			if part.FunctionResponse != nil {
				// Tool results are user messages
				role = "user" 
				
				resBytes, err := json.Marshal(part.FunctionResponse.Response)
				if err != nil {
					resBytes = []byte(fmt.Sprintf("%v", part.FunctionResponse.Response))
				}
				
				content = append(content, Content{
					Type:      "tool_result",
					ToolUseID: part.FunctionResponse.ID,
					Content:   string(resBytes),
				})
			}
		}
		
		if len(content) > 0 {
			messages = append(messages, Message{
				Role:    role,
				Content: content,
			})
		}
	}
	
	// Map tools
	var tools []Tool
	if req.Config != nil && len(req.Config.Tools) > 0 {
		for _, t := range req.Config.Tools {
			for _, fd := range t.FunctionDeclarations {
				schemaBytes, _ := json.Marshal(fd.ParametersJsonSchema)
				tools = append(tools, Tool{
					Name:        fd.Name,
					Description: fd.Description,
					InputSchema: schemaBytes,
				})
			}
		}
	}

	return &Request{
		Model:     p.model,
		Messages:  messages,
		System:    system,
		MaxTokens: 64000,
		Stream:    streaming,
		Tools:     tools,
	}, nil
}

// Structs for Anthropic API

type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	System    string    `json:"system,omitempty"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
	Tools     []Tool    `json:"tools,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Message struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

type Content struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`        // For tool_use
	Name      string          `json:"name,omitempty"`      // For tool_use
	Input     json.RawMessage `json:"input,omitempty"`     // For tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // For tool_result
	Content   string          `json:"content,omitempty"`   // For tool_result (can be string or list of content blocks)
	IsError   bool            `json:"is_error,omitempty"`  // For tool_result
}

type Response struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

type StreamEvent struct {
	Type         string       `json:"type"`
	Delta        *StreamDelta `json:"delta,omitempty"`
	ContentBlock *Content     `json:"content_block,omitempty"` // For content_block_start
	Index        int          `json:"index,omitempty"`
}

type StreamDelta struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	PartialJson string          `json:"partial_json,omitempty"` // For tool_use input delta
}
