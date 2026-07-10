package provider

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type mockLLM struct {
	name string
}

func (m *mockLLM) Name() string { return m.name }
func (m *mockLLM) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{{Text: "from " + m.name}}},
		}, nil)
	}
}

func TestSwappableLLM_Name(t *testing.T) {
	s := NewSwappableLLM(&mockLLM{name: "model-a"})
	if got := s.Name(); got != "model-a" {
		t.Errorf("Name() = %q, want %q", got, "model-a")
	}

	s.Swap(&mockLLM{name: "model-b"})
	if got := s.Name(); got != "model-b" {
		t.Errorf("after Swap, Name() = %q, want %q", got, "model-b")
	}
}

func TestSwappableLLM_GenerateContent(t *testing.T) {
	s := NewSwappableLLM(&mockLLM{name: "model-a"})

	// Generate from model-a
	var text string
	for resp, err := range s.GenerateContent(context.Background(), nil, false) {
		if err != nil {
			t.Fatal(err)
		}
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				text += p.Text
			}
		}
	}
	if text != "from model-a" {
		t.Errorf("got %q, want %q", text, "from model-a")
	}

	// Swap and generate from model-b
	s.Swap(&mockLLM{name: "model-b"})
	text = ""
	for resp, err := range s.GenerateContent(context.Background(), nil, false) {
		if err != nil {
			t.Fatal(err)
		}
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				text += p.Text
			}
		}
	}
	if text != "from model-b" {
		t.Errorf("got %q, want %q", text, "from model-b")
	}
}

func TestSwappableLLM_Inner(t *testing.T) {
	a := &mockLLM{name: "a"}
	b := &mockLLM{name: "b"}
	s := NewSwappableLLM(a)

	if s.Inner() != a {
		t.Error("Inner() should return initial LLM")
	}
	s.Swap(b)
	if s.Inner() != b {
		t.Error("Inner() should return swapped LLM")
	}
}

func TestSwappableLLM_ImplementsInterface(t *testing.T) {
	var _ model.LLM = (*SwappableLLM)(nil)
}

func TestSwap_SubsequentCallUsesNewLLM(t *testing.T) {
	// Given: a SwappableLLM wrapping model-a
	s := NewSwappableLLM(&mockLLM{name: "model-a"})

	// When: Swap replaces the inner LLM with model-b
	s.Swap(&mockLLM{name: "model-b"})

	// Then: the next GenerateContent call is served by model-b, not model-a
	var text string
	for resp, err := range s.GenerateContent(context.Background(), nil, false) {
		if err != nil {
			t.Fatal(err)
		}
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				text += p.Text
			}
		}
	}
	if text != "from model-b" {
		t.Errorf("after Swap, GenerateContent produced %q, want %q", text, "from model-b")
	}
	if s.Name() != "model-b" {
		t.Errorf("after Swap, Name() = %q, want %q", s.Name(), "model-b")
	}
}
