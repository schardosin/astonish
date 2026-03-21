package openai_compat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/sashabaranov/go-openai"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"google.golang.org/adk/model"
)

// Provider implements model.LLM for OpenAI Compatible endpoints.
type Provider struct {
	*openai_provider.Provider
}

// NewProvider creates a new OpenAI Compatible provider.
func NewProvider(apiKey, baseURL, modelName string, debug bool) model.LLM {
	config := openai.DefaultConfig(apiKey)

	// Ensure baseURL has /v1 suffix
	if baseURL != "" {
		if !strings.HasSuffix(baseURL, "/v1") {
			if strings.HasSuffix(baseURL, "/") {
				baseURL = baseURL + "v1"
			} else {
				baseURL = baseURL + "/v1"
			}
		}
		config.BaseURL = baseURL
	}

	if debug {
		config.HTTPClient = &http.Client{
			Transport: &debugHTTPTransport{base: http.DefaultTransport},
		}
	}

	client := openai.NewClientWithConfig(config)

	return &Provider{
		Provider: openai_provider.NewProviderWithMaxTokens(client, modelName, true, 64000),
	}
}

// Name implements model.LLM.
func (p *Provider) Name() string {
	return p.Provider.Name()
}

// GenerateContent implements model.LLM.
func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return p.Provider.GenerateContent(ctx, req, streaming)
}

// ListModels returns available models from an OpenAI compatible endpoint.
func ListModels(ctx context.Context, apiKey, baseURL string) ([]string, error) {
	config := openai.DefaultConfig(apiKey)

	if baseURL != "" {
		if !strings.HasSuffix(baseURL, "/v1") {
			if strings.HasSuffix(baseURL, "/") {
				baseURL = baseURL + "v1"
			} else {
				baseURL = baseURL + "/v1"
			}
		}
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)

	resp, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	var models []string
	for _, m := range resp.Models {
		models = append(models, m.ID)
	}
	return models, nil
}

// GetRequiredFields returns the required configuration fields for this provider.
func GetRequiredFields() []string {
	return []string{"api_key", "base_url"}
}

// debugHTTPTransport wraps an http.RoundTripper to log request and response
// details when debug mode is enabled. It captures:
//   - Request URL and method
//   - Request body (for POST requests)
//   - Response status code
//   - Response body (for non-2xx responses)
type debugHTTPTransport struct {
	base http.RoundTripper
}

func (t *debugHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Printf("[DEBUG HTTP] %s %s\n", req.Method, req.URL.String())

	// Log request headers (redact Authorization)
	for key, vals := range req.Header {
		if strings.EqualFold(key, "Authorization") {
			fmt.Printf("[DEBUG HTTP]   %s: [REDACTED]\n", key)
		} else {
			fmt.Printf("[DEBUG HTTP]   %s: %s\n", key, strings.Join(vals, ", "))
		}
	}

	// Capture request body
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("debug transport: failed to read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		fmt.Printf("[DEBUG HTTP] Request body (%d bytes):\n%s\n", len(bodyBytes), string(bodyBytes))
	}

	// Perform the actual request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		fmt.Printf("[DEBUG HTTP] Transport error: %v\n", err)
		return resp, err
	}

	fmt.Printf("[DEBUG HTTP] Response: %d %s\n", resp.StatusCode, resp.Status)

	// For non-2xx responses, capture the response body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			fmt.Printf("[DEBUG HTTP] Failed to read error response body: %v\n", readErr)
		} else {
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			fmt.Printf("[DEBUG HTTP] Error response body:\n%s\n", string(bodyBytes))
		}
	}

	return resp, nil
}
