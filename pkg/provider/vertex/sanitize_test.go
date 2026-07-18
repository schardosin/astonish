package vertex

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestConvertRequest_NestedAdditionalPropertiesStripped(t *testing.T) {
	t.Parallel()
	// Shape mirrors github.com/google/jsonschema-go output for nested objects /
	// maps / pointer fields used by ADK functiontool schemas.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
			"offset": map[string]any{
				"type": []any{"null", "integer"},
			},
			"opts": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"value": map[string]any{"type": "integer"},
					},
					"additionalProperties": false,
					"required":             []any{"name", "value"},
				},
			},
		},
		"required":             []any{"path"},
		"additionalProperties": false,
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "Hi"}}}},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{
				FunctionDeclarations: []*genai.FunctionDeclaration{{
					Name:                 "demo_tool",
					Description:          "demo",
					ParametersJsonSchema: schema,
				}},
			}},
		},
	}
	vReq, err := ConvertRequest(req, 65536)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(vReq.Tools[0].FunctionDeclarations[0].Parameters)
	s := string(b)
	if strings.Contains(s, "additionalProperties") {
		t.Errorf("nested additionalProperties still present: %s", s)
	}
	if strings.Contains(s, "$schema") {
		t.Errorf("$schema still present: %s", s)
	}
	if strings.Contains(s, `"type":["null","integer"]`) || strings.Contains(s, `"type": ["null", "integer"]`) {
		t.Errorf("nullable type union not normalized: %s", s)
	}
	if !strings.Contains(s, `"type":"integer"`) && !strings.Contains(s, `"type": "integer"`) {
		t.Errorf("expected offset type normalized to integer, got: %s", s)
	}
}

func TestConvertRequest_SkipsEmptyParts(t *testing.T) {
	t.Parallel()
	req := &model.LLMRequest{
		Contents: []*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: ""},
				{Text: "Hi"},
				{},
			},
		}},
	}
	vReq, err := ConvertRequest(req, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(vReq.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(vReq.Contents))
	}
	if len(vReq.Contents[0].Parts) != 1 {
		t.Fatalf("expected 1 non-empty part, got %d", len(vReq.Contents[0].Parts))
	}
	if vReq.Contents[0].Parts[0].Text != "Hi" {
		t.Errorf("expected Hi, got %q", vReq.Contents[0].Parts[0].Text)
	}
}
