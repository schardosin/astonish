package ui

import (
	"strings"
	"testing"
)

func TestRenderToolBox_EmptyArgs(t *testing.T) {
	t.Parallel()
	got := RenderToolBox("test_tool", map[string]interface{}{})
	if !strings.Contains(got, "test_tool") {
		t.Errorf("expected tool name in output, got %q", got)
	}
}

func TestRenderToolBox_SingleArg(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{"query": "hello world"}
	got := RenderToolBox("search", args)
	if !strings.Contains(got, "search") {
		t.Errorf("expected 'search' in output, got %q", got)
	}
	if !strings.Contains(got, "query") {
		t.Errorf("expected 'query' in output, got %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", got)
	}
}

func TestRenderToolBox_MultipleArgs(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{
		"topic":       "news",
		"max_results": 10,
	}
	got := RenderToolBox("fetch", args)
	if !strings.Contains(got, "topic") {
		t.Errorf("expected 'topic' in output, got %q", got)
	}
	if !strings.Contains(got, "max_results") {
		t.Errorf("expected 'max_results' in output, got %q", got)
	}
}

func TestRenderToolBox_TruncatesLongValues(t *testing.T) {
	t.Parallel()
	longVal := strings.Repeat("x", 300)
	args := map[string]interface{}{"data": longVal}
	got := RenderToolBox("tool", args)
	// Value should be truncated to 200 chars (197 + "...")
	if strings.Contains(got, longVal) {
		t.Errorf("long value should be truncated, but full value found in output")
	}
	if !strings.Contains(got, "...") {
		t.Errorf("expected '...' for truncated value, got %q", got)
	}
}

func TestRenderToolBox_NumberStyling(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{"count": 42}
	got := RenderToolBox("tool", args)
	// Should contain the number (styled differently but still present)
	if !strings.Contains(got, "42") {
		t.Errorf("expected '42' in output, got %q", got)
	}
}

func TestRenderToolBox_FloatStyling(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{"score": 3.14}
	got := RenderToolBox("tool", args)
	if !strings.Contains(got, "3.14") {
		t.Errorf("expected '3.14' in output, got %q", got)
	}
}

func TestRenderToolBox_SortedKeys(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{
		"zebra": "z",
		"alpha": "a",
	}
	got := RenderToolBox("tool", args)
	alphaIdx := strings.Index(got, "alpha")
	zebraIdx := strings.Index(got, "zebra")
	if alphaIdx == -1 || zebraIdx == -1 {
		t.Fatalf("expected both keys in output, got %q", got)
	}
	if alphaIdx >= zebraIdx {
		t.Errorf("expected 'alpha' before 'zebra' (sorted order), alphaIdx=%d zebraIdx=%d", alphaIdx, zebraIdx)
	}
}

func TestRenderToolBox_EndsWithNewline(t *testing.T) {
	t.Parallel()
	got := RenderToolBox("tool", map[string]interface{}{"k": "v"})
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected output to end with newline")
	}
}

func TestRenderStatusBadge_Success(t *testing.T) {
	t.Parallel()
	got := RenderStatusBadge("Command approved", true)
	if !strings.Contains(got, "Command approved") {
		t.Errorf("expected text in output, got %q", got)
	}
}

func TestRenderStatusBadge_Failure(t *testing.T) {
	t.Parallel()
	got := RenderStatusBadge("Command denied", false)
	if !strings.Contains(got, "Command denied") {
		t.Errorf("expected text in output, got %q", got)
	}
}

func TestRenderStatusBadge_EmptyText(t *testing.T) {
	t.Parallel()
	// Should not panic with empty text
	got := RenderStatusBadge("", true)
	if got == "" {
		t.Errorf("expected non-empty output even with empty text")
	}
}
