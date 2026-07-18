package llmerror

import "testing"

func TestIsNoFunctionCalling_Match(t *testing.T) {
	t.Parallel()
	body := `{"error":{"code":400,"message":"Unable to submit request because the model does not support function calling. Learn more: https://cloud.google.com/vertex-ai/generative-ai/docs/model-reference/gemini","status":"INVALID_ARGUMENT"}}`
	if !IsNoFunctionCalling(body) {
		t.Fatal("expected match")
	}
}

func TestIsNoFunctionCalling_CaseInsensitive(t *testing.T) {
	t.Parallel()
	if !IsNoFunctionCalling("DOES NOT SUPPORT FUNCTION CALLING") {
		t.Fatal("expected case-insensitive match")
	}
}

func TestIsNoFunctionCalling_Unrelated400(t *testing.T) {
	t.Parallel()
	body := `{"error":{"message":"Unknown name \"additionalProperties\" at 'tools'"}}`
	if IsNoFunctionCalling(body) {
		t.Error("expected no match for unrelated 400")
	}
}
