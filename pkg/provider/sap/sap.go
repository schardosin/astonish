package sap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	goopenai "github.com/sashabaranov/go-openai"
	"github.com/schardosin/astonish/pkg/provider/bedrock"
	"github.com/schardosin/astonish/pkg/provider/openai"
	"github.com/schardosin/astonish/pkg/provider/vertex"
	"google.golang.org/adk/model"
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
	if strings.HasPrefix(p.modelName, "gemini-") {
		return p.generateVertexContent(ctx, req, streaming)
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

	// Inject api-version for Azure OpenAI models if missing
	if strings.Contains(req.URL.Path, "/chat/completions") {
		q := req.URL.Query()
		if q.Get("api-version") == "" {
			q.Set("api-version", "2024-02-01")
			req.URL.RawQuery = q.Encode()
		}
	}

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

// ModelIDMap maps friendly model names to SAP AI Core model names
var ModelIDMap = map[string]string{
	"gpt-4o":        "gpt-4o",
	"gpt-4o-mini":   "gpt-4o-mini",
	"gpt-4":         "gpt-4",
	"gpt-3.5-turbo": "gpt-3.5-turbo",
	"o1":            "o1",
	"o4-mini":       "o4-mini",
	"gpt-5":         "gpt-5",
}

// ResolveDeploymentID finds the deployment ID for a given model name.
func ResolveDeploymentID(ctx context.Context, modelName string) (string, error) {
	// Check map first
	if mapped, ok := ModelIDMap[modelName]; ok {
		modelName = mapped
	}

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
			log.Printf("[DEBUG] Found running deployment for model '%s': %s\n", modelName, res.ID)
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

func (p *Provider) generateBedrockContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Convert request using bedrock protocol
		bedrockReq, err := bedrock.ConvertRequest(req)
		if err != nil {
			yield(nil, err)
			return
		}

		payload, err := json.Marshal(bedrockReq)
		if err != nil {
			yield(nil, err)
			return
		}
		
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
			// Handle streaming response using bedrock protocol
			for resp, err := range bedrock.ParseStream(resp.Body) {
				if !yield(resp, err) {
					return
				}
			}
		} else {
			// Non-streaming response
			body, _ := io.ReadAll(resp.Body)
			llmResp, err := bedrock.ParseResponse(body)
			if err != nil {
				yield(nil, err)
				return
			}
			yield(llmResp, nil)
		}
	}
}

func (p *Provider) generateVertexContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Convert request using vertex protocol
		vertexReq, err := vertex.ConvertRequest(req)
		if err != nil {
			yield(nil, err)
			return
		}

		payload, err := json.Marshal(vertexReq)
		if err != nil {
			yield(nil, err)
			return
		}
		
		var url string
		if streaming {
			// Vertex AI streaming endpoint: /models/{model}:streamGenerateContent?alt=sse
			url = fmt.Sprintf("%s/inference/deployments/%s/models/%s:streamGenerateContent?alt=sse", p.baseURL, p.deploymentID, p.modelName)
		} else {
			// Vertex AI non-streaming endpoint: /models/{model}:generateContent
			url = fmt.Sprintf("%s/inference/deployments/%s/models/%s:generateContent", p.baseURL, p.deploymentID, p.modelName)
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
			yield(nil, fmt.Errorf("vertex request failed: %s, body: %s", resp.Status, string(body)))
			return
		}

		if streaming {
			// Handle streaming response using vertex protocol
			for resp, err := range vertex.ParseStream(resp.Body) {
				if !yield(resp, err) {
					return
				}
			}
		} else {
			// Non-streaming response
			body, _ := io.ReadAll(resp.Body)
			llmResp, err := vertex.ParseResponse(body)
			if err != nil {
				yield(nil, err)
				return
			}
			yield(llmResp, nil)
		}
	}
}
