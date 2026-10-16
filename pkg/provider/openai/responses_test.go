package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	openailib "github.com/sashabaranov/go-openai"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestNewOpenAIProviderSelectsResponsesAPI(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-6-astra", want: true},
		{model: "gpt-6.1-sol", want: true},
		{model: "GPT-6.1-SOL-20261001", want: true},
		{model: "gpt-5.4", want: false},
		{model: "gpt-4o", want: false},
	}
	client := openailib.NewClient("test")
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider := NewOpenAIProvider(client, tt.model, true)
			if provider.useResponsesAPI != tt.want {
				t.Fatalf("useResponsesAPI = %v, want %v", provider.useResponsesAPI, tt.want)
			}
		})
	}
}

func TestGPT6FunctionToolsUseResponsesAPI(t *testing.T) {
	var path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_1","status":"completed","model":"gpt-6.1-sol",
			"output":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"Astonish\"}"}],
			"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
		}`))
	}))
	defer server.Close()

	config := openailib.DefaultConfig("test")
	config.BaseURL = server.URL
	provider := NewOpenAIProvider(openailib.NewClientWithConfig(config), "gpt-6.1-sol", true)
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("look this up", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "lookup",
				ParametersJsonSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"query": map[string]any{"type": "string"}},
				},
			}},
		}}},
	}

	var got *model.LLMResponse
	for resp, err := range provider.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
		got = resp
	}
	if path != "/responses" {
		t.Fatalf("request path = %q, want /responses", path)
	}
	if _, exists := body["reasoning_effort"]; exists {
		t.Fatal("Responses request must not contain chat reasoning_effort")
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one function tool", body["tools"])
	}
	if got == nil || got.Content == nil || len(got.Content.Parts) != 1 || got.Content.Parts[0].FunctionCall == nil {
		t.Fatalf("response = %#v, want function call", got)
	}
	call := got.Content.Parts[0].FunctionCall
	if call.ID != "call_1" || call.Name != "lookup" || call.Args["query"] != "Astonish" {
		t.Fatalf("function call = %#v", call)
	}
}

func TestOpenAICompatibleGPT6StaysOnChatCompletions(t *testing.T) {
	client := openailib.NewClient("test")
	provider := NewProvider(client, "gpt-6-compatible", true)
	if provider.useResponsesAPI {
		t.Fatal("OpenAI-compatible provider unexpectedly enabled Responses API")
	}
}
