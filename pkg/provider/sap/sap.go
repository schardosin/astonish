package sap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	goopenai "github.com/sashabaranov/go-openai"
	"github.com/schardosin/astonish/pkg/provider/openai"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Provider implements the model.LLM interface for SAP AI Core.
type Provider struct {
	openaiProvider *openai.Provider
	httpClient     *http.Client
	baseURL        string
	deploymentID   string
	modelName      string
	authConfig     *sapTransport
}

// NewProvider creates a new SAP AI Core provider.
func NewProvider(ctx context.Context, modelName string) (model.LLM, error) {
	clientID := os.Getenv("AICORE_CLIENT_ID")
	clientSecret := os.Getenv("AICORE_CLIENT_SECRET")
	authURL := os.Getenv("AICORE_AUTH_URL")
	baseURL := os.Getenv("AICORE_BASE_URL")
	resourceGroup := os.Getenv("AICORE_RESOURCE_GROUP")

	if clientID == "" || clientSecret == "" || authURL == "" || baseURL == "" {
		return nil, fmt.Errorf("missing SAP AI Core environment variables")
	}

	if !strings.HasSuffix(baseURL, "/v2") {
		if strings.HasSuffix(baseURL, "/") {
			baseURL += "v2"
		} else {
			baseURL += "/v2"
		}
	}

	// Resolve deployment ID
	deploymentID, err := ResolveDeploymentID(ctx, modelName)
	if err != nil {
		return nil, err
	}

	transport := &sapTransport{
		base:          http.DefaultTransport,
		clientID:      clientID,
		clientSecret:  clientSecret,
		authURL:       authURL,
		resourceGroup: resourceGroup,
	}

	// Initialize OpenAI provider for fallback/delegation
	// We construct the full URL for OpenAI provider: baseURL + /inference/deployments/{id}
	// go-openai appends /chat/completions
	deploymentURL := fmt.Sprintf("%s/inference/deployments/%s", baseURL, deploymentID)
	
	openaiConfig := goopenai.DefaultConfig(clientSecret) // Token is handled by transport, but config needs something
	openaiConfig.BaseURL = deploymentURL
	openaiConfig.HTTPClient = &http.Client{
		Transport: transport,
	}
	openaiConfig.APIType = goopenai.APITypeOpenAI

	// Create the wrapped OpenAI provider
	client := goopenai.NewClientWithConfig(openaiConfig)
	op := openai.NewProvider(client, modelName)

	return &Provider{
		openaiProvider: op,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
		baseURL:      baseURL,
		deploymentID: deploymentID,
		modelName:    modelName,
		authConfig:   transport,
	}, nil
}

func (p *Provider) Name() string {
	return p.modelName
}

func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	if strings.HasPrefix(p.modelName, "anthropic--") || strings.HasPrefix(p.modelName, "amazon--") {
		return p.generateBedrockContent(ctx, req, streaming)
	}
	return p.openaiProvider.GenerateContent(ctx, req, streaming)
}

type sapTransport struct {
	base          http.RoundTripper
	clientID      string
	clientSecret  string
	authURL       string
	resourceGroup string
	
	token     string
	expiresAt time.Time
	mu        sync.Mutex
}

func (t *sapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.getToken()
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("AI-Resource-Group", t.resourceGroup)
	
	// SAP AI Core requires specific path adjustment sometimes, but let's try standard OpenAI path first
	// Standard: BaseURL + /chat/completions
	// Constructed: .../inference/deployments/{id}/chat/completions
	// This should match SAP AI Core API

	return t.base.RoundTrip(req)
}

func (t *sapTransport) getToken() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && time.Now().Before(t.expiresAt) {
		return t.token, nil
	}

	// Fetch new token
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", t.clientID)
	data.Set("client_secret", t.clientSecret)

	authReq, err := http.NewRequest("POST", t.authURL+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	authReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(authReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get token: %s, body: %s", resp.Status, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	t.token = result.AccessToken
	t.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Add(-30 * time.Second) // Buffer

	return t.token, nil
}

// ResolveDeploymentID finds the deployment ID for a given model name.
func ResolveDeploymentID(ctx context.Context, modelName string) (string, error) {
	// We need a temporary transport to get the token
	t := &sapTransport{
		base:          http.DefaultTransport,
		clientID:      os.Getenv("AICORE_CLIENT_ID"),
		clientSecret:  os.Getenv("AICORE_CLIENT_SECRET"),
		authURL:       os.Getenv("AICORE_AUTH_URL"),
		resourceGroup: os.Getenv("AICORE_RESOURCE_GROUP"),
	}
	
	token, err := t.getToken()
	if err != nil {
		return "", fmt.Errorf("failed to get token for deployment lookup: %w", err)
	}

	baseURL := os.Getenv("AICORE_BASE_URL")
	if !strings.HasSuffix(baseURL, "/v2") {
		baseURL += "/v2"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/lm/deployments", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("AI-Resource-Group", t.resourceGroup)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to list deployments: %s, body: %s", resp.Status, string(body))
	}

	var result struct {
		Resources []struct {
			ID            string `json:"id"`
			Details struct {
				Resources struct {
					BackendDetails struct {
						Model struct {
							Name string `json:"name"`
						} `json:"model"`
					} `json:"backendDetails"`
				} `json:"resources"`
			} `json:"details"`
			Status string `json:"status"`
		} `json:"resources"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	for _, res := range result.Resources {
		if res.Status == "RUNNING" && res.Details.Resources.BackendDetails.Model.Name == modelName {
			return res.ID, nil
		}
	}

	return "", fmt.Errorf("no running deployment found for model: %s", modelName)
}

// ListModels fetches the list of available models from running deployments.
func ListModels(ctx context.Context, clientID, clientSecret, authURL, baseURL, resourceGroup string) ([]string, error) {
	t := &sapTransport{
		base:          http.DefaultTransport,
		clientID:      clientID,
		clientSecret:  clientSecret,
		authURL:       authURL,
		resourceGroup: resourceGroup,
	}

	token, err := t.getToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	if !strings.HasSuffix(baseURL, "/v2") {
		if strings.HasSuffix(baseURL, "/") {
			baseURL += "v2"
		} else {
			baseURL += "/v2"
		}
	}

	requestURL := fmt.Sprintf("%s/lm/deployments?status=RUNNING&resourceGroup=%s", baseURL, resourceGroup)
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("AI-Resource-Group", resourceGroup)
	// Token will be added by the transport

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deployments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s, body: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Resources []struct {
			Details struct {
				Resources struct {
					BackendDetails struct {
						Model struct {
							Name string `json:"name"`
						} `json:"model"`
					} `json:"backendDetails"`
				} `json:"resources"`
			} `json:"details"`
			Status string `json:"status"`
		} `json:"resources"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	modelSet := make(map[string]bool)
	for _, res := range result.Resources {
		if res.Status == "RUNNING" {
			name := res.Details.Resources.BackendDetails.Model.Name
			if name != "" {
				modelSet[name] = true
			}
		}
	}

	var models []string
	for m := range modelSet {
		models = append(models, m)
	}
	return models, nil
}

// Bedrock payload structures
type bedrockRequest struct {
	AnthropicVersion string           `json:"anthropic_version"`
	MaxTokens        int              `json:"max_tokens"`
	Messages         []bedrockMessage `json:"messages"`
	System           string           `json:"system,omitempty"`
	Temperature      float64          `json:"temperature,omitempty"`
	Tools            []bedrockTool    `json:"tools,omitempty"`
}

type bedrockMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or array of content blocks
}

type bedrockTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type bedrockContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
}

type bedrockResponse struct {
	Content []bedrockContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

func (p *Provider) generateBedrockContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Construct Bedrock payload
		bedrockReq := bedrockRequest{
			AnthropicVersion: "bedrock-2023-05-31",
			MaxTokens:        4096, // Default max tokens
			Messages:         make([]bedrockMessage, 0),
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
				var contentBlocks []bedrockContentBlock
				for _, part := range content.Parts {
					if part.FunctionCall != nil {
						contentBlocks = append(contentBlocks, bedrockContentBlock{
							Type:  "tool_use",
							ID:    part.FunctionCall.ID,
							Name:  part.FunctionCall.Name,
							Input: part.FunctionCall.Args,
						})
					}
				}
				bedrockReq.Messages = append(bedrockReq.Messages, bedrockMessage{
					Role:    role,
					Content: contentBlocks,
				})
			} else if hasFunctionResponse {
				// Convert function responses to tool_result blocks
				var contentBlocks []bedrockContentBlock
				for _, part := range content.Parts {
					if part.FunctionResponse != nil {
						resultJSON, _ := json.Marshal(part.FunctionResponse.Response)
						contentBlocks = append(contentBlocks, bedrockContentBlock{
							Type:      "tool_result",
							ToolUseID: part.FunctionResponse.ID,
							Content:   string(resultJSON),
						})
					}
				}
				bedrockReq.Messages = append(bedrockReq.Messages, bedrockMessage{
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
				bedrockReq.Messages = append(bedrockReq.Messages, bedrockMessage{
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
					bedrockTool := bedrockTool{
						Name:        funcDecl.Name,
						Description: funcDecl.Description,
						InputSchema: make(map[string]interface{}),
					}
					
					// Convert JSON schema to Bedrock format
					if funcDecl.ParametersJsonSchema != nil {
						bedrockTool.InputSchema["type"] = "object"
						if schemaMap, ok := funcDecl.ParametersJsonSchema.(map[string]interface{}); ok {
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

		payload, err := json.Marshal(bedrockReq)
		if err != nil {
			yield(nil, err)
			return
		}

		// Choose endpoint based on streaming
		var url string
		if streaming {
			url = fmt.Sprintf("%s/inference/deployments/%s/invoke-with-response-stream", p.baseURL, p.deploymentID)
		} else {
			url = fmt.Sprintf("%s/inference/deployments/%s/invoke", p.baseURL, p.deploymentID)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			yield(nil, err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("AI-Resource-Group", p.authConfig.resourceGroup)
		// Token is added by transport

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			yield(nil, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("bedrock request failed: %s, body: %s", resp.Status, string(body)))
			return
		}

		if streaming {
			// Handle streaming response (SSE format)
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
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
					var chunk struct {
						Type  string `json:"type"`
						Delta struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"delta"`
					}
					
					if err := json.Unmarshal(data, &chunk); err != nil {
						// Skip malformed chunks
						continue
					}

					// Handle different chunk types
					if chunk.Type == "content_block_delta" && chunk.Delta.Text != "" {
						if !yield(&model.LLMResponse{
							Content: &genai.Content{
								Role:  "model",
								Parts: []*genai.Part{{Text: chunk.Delta.Text}},
							},
						}, nil) {
							return
						}
					}
					// Note: Tool use in streaming is handled in content_block_start events
					// For now, we'll handle tool calls in non-streaming mode
				}
			}
		} else {
			// Non-streaming response
			body, _ := io.ReadAll(resp.Body)
			var bedrockResp bedrockResponse
			if err := json.Unmarshal(body, &bedrockResp); err != nil {
				yield(nil, fmt.Errorf("failed to decode bedrock response: %w", err))
				return
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
					parts = append(parts, &genai.Part{
						FunctionCall: &genai.FunctionCall{
							ID:   block.ID,
							Name: block.Name,
							Args: block.Input,
						},
					})
				}
			}

			yield(&model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: parts,
				},
			}, nil)
		}
	}
}
