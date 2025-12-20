package vertex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Request represents the Vertex AI request payload.
type Request struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type FunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"` // JSON Schema
}

type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"` // AUTO, ANY, NONE
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type GenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
	TopK            int     `json:"topK,omitempty"`
}

// Response represents the Vertex AI response payload.
type Response struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content      *Content `json:"content,omitempty"`
	FinishReason string   `json:"finishReason,omitempty"`
}

// ConvertRequest converts an ADK LLMRequest to a Vertex AI Request.
// maxOutputTokens can be 0 to use the default (8192)
func ConvertRequest(req *model.LLMRequest, maxOutputTokens int) (*Request, error) {
	if maxOutputTokens <= 0 {
		maxOutputTokens = 8192 // Default fallback
	}
	vertexReq := &Request{
		Contents: make([]Content, 0),
		GenerationConfig: &GenerationConfig{
			Temperature:     0.7, // Default
			MaxOutputTokens: maxOutputTokens,
		},
	}

	// Convert Contents
	for _, c := range req.Contents {
		parts := make([]Part, 0)
		for _, p := range c.Parts {
			part := Part{}
			if p.Text != "" {
				part.Text = p.Text
			}
			if p.FunctionCall != nil {
				part.FunctionCall = &FunctionCall{
					Name: p.FunctionCall.Name,
					Args: p.FunctionCall.Args,
				}
			}
			if p.FunctionResponse != nil {
				part.FunctionResponse = &FunctionResponse{
					Name:     p.FunctionResponse.Name,
					Response: p.FunctionResponse.Response,
				}
			}
			parts = append(parts, part)
		}
		vertexReq.Contents = append(vertexReq.Contents, Content{
			Role:  c.Role,
			Parts: parts,
		})
	}

	// Convert System Instruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		parts := make([]Part, 0)
		for _, p := range req.Config.SystemInstruction.Parts {
			parts = append(parts, Part{Text: p.Text})
		}
		vertexReq.SystemInstruction = &Content{
			Parts: parts,
		}
	}

	// Convert Tools
	if req.Config != nil && len(req.Config.Tools) > 0 {
		for _, t := range req.Config.Tools {
			fds := make([]FunctionDeclaration, 0)
			for _, fd := range t.FunctionDeclarations {
				// Sanitize parameters schema - Vertex AI rejects $schema
				params := fd.ParametersJsonSchema
				
				// Ensure params is a map so we can sanitize it
				// The schema can be various types (struct, map, etc.) from ADK
				schemaBytes, err := json.Marshal(params)
				if err == nil {
					var paramsMap map[string]interface{}
					if err := json.Unmarshal(schemaBytes, &paramsMap); err == nil {
						// Create a copy to avoid modifying the original
						newParams := make(map[string]interface{})
						for k, v := range paramsMap {
							if k != "$schema" && k != "additionalProperties" {
								newParams[k] = v
							}
						}
						params = newParams
					}
				}

				fds = append(fds, FunctionDeclaration{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  params,
				})
			}
			vertexReq.Tools = append(vertexReq.Tools, Tool{
				FunctionDeclarations: fds,
			})
		}
	}

	// Convert ToolConfig (FunctionCallingConfig)
	if req.Config != nil && req.Config.ToolConfig != nil {
		vertexReq.ToolConfig = &ToolConfig{
			FunctionCallingConfig: &FunctionCallingConfig{
				Mode: string(req.Config.ToolConfig.FunctionCallingConfig.Mode),
			},
		}
		if len(req.Config.ToolConfig.FunctionCallingConfig.AllowedFunctionNames) > 0 {
			vertexReq.ToolConfig.FunctionCallingConfig.AllowedFunctionNames = req.Config.ToolConfig.FunctionCallingConfig.AllowedFunctionNames
		}
	} else {
		// fmt.Println("[VERTEX DEBUG] No ToolConfig found in request")
	}

	return vertexReq, nil
}

// ParseResponse converts a Vertex AI Response body to an ADK LLMResponse.
func ParseResponse(body []byte) (*model.LLMResponse, error) {
	var vertexResp Response
	if err := json.Unmarshal(body, &vertexResp); err != nil {
		return nil, fmt.Errorf("failed to decode vertex response: %w", err)
	}

	if len(vertexResp.Candidates) == 0 {
		return &model.LLMResponse{}, nil
	}

	candidate := vertexResp.Candidates[0]
	if candidate.Content == nil {
		return &model.LLMResponse{}, nil
	}

	var parts []*genai.Part
	for _, p := range candidate.Content.Parts {
		part := &genai.Part{}
		if p.Text != "" {
			part.Text = p.Text
		}
		if p.FunctionCall != nil {
			part.FunctionCall = &genai.FunctionCall{
				Name: p.FunctionCall.Name,
				Args: p.FunctionCall.Args,
			}
		}
		parts = append(parts, part)
	}

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  candidate.Content.Role,
			Parts: parts,
		},
	}, nil
}

// ParseStream parses a Vertex AI SSE stream and yields ADK LLMResponses.
func ParseStream(reader io.Reader) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		bufReader := bufio.NewReader(reader)

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

				// Parse the JSON chunk
				var vertexResp Response
				if err := json.Unmarshal(data, &vertexResp); err != nil {
					// Skip malformed chunks
					continue
				}

				if len(vertexResp.Candidates) > 0 {
					candidate := vertexResp.Candidates[0]
					if candidate.Content != nil {
						var parts []*genai.Part
						for _, p := range candidate.Content.Parts {
							part := &genai.Part{}
							if p.Text != "" {
								part.Text = p.Text
							}
							if p.FunctionCall != nil {
								part.FunctionCall = &genai.FunctionCall{
									Name: p.FunctionCall.Name,
									Args: p.FunctionCall.Args,
								}
							}
							parts = append(parts, part)
						}

						if len(parts) > 0 {
							if !yield(&model.LLMResponse{
								Content: &genai.Content{
									Role:  candidate.Content.Role,
									Parts: parts,
								},
							}, nil) {
								return
							}
						}
					}
				}
			}
		}
	}
}
