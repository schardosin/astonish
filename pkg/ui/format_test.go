package ui

import (
	"strings"
	"testing"
)

func TestFormatAsYamlLike_Nil(t *testing.T) {
	t.Parallel()
	got := FormatAsYamlLike(nil, 0)
	if got != "null" {
		t.Errorf("expected 'null', got %q", got)
	}
}

func TestFormatAsYamlLike_Primitives(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  interface{}
		expect string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool_true", true, "true"},
		{"bool_false", false, "false"},
		{"empty_string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatAsYamlLike(tt.input, 0)
			if got != tt.expect {
				t.Errorf("FormatAsYamlLike(%v, 0) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestFormatAsYamlLike_Pointer(t *testing.T) {
	t.Parallel()

	t.Run("nil_pointer", func(t *testing.T) {
		t.Parallel()
		var p *int
		got := FormatAsYamlLike(p, 0)
		if got != "null" {
			t.Errorf("expected 'null' for nil pointer, got %q", got)
		}
	})

	t.Run("non_nil_pointer", func(t *testing.T) {
		t.Parallel()
		v := 42
		got := FormatAsYamlLike(&v, 0)
		if got != "42" {
			t.Errorf("expected '42' for pointer to 42, got %q", got)
		}
	})
}

func TestFormatAsYamlLike_EmptySlice(t *testing.T) {
	t.Parallel()
	got := FormatAsYamlLike([]interface{}{}, 0)
	if got != "[]" {
		t.Errorf("expected '[]', got %q", got)
	}
}

func TestFormatAsYamlLike_EmptyMap(t *testing.T) {
	t.Parallel()
	got := FormatAsYamlLike(map[string]interface{}{}, 0)
	if got != "{}" {
		t.Errorf("expected '{}', got %q", got)
	}
}

func TestFormatAsYamlLike_SliceOfPrimitives(t *testing.T) {
	t.Parallel()
	input := []interface{}{"a", "b", "c"}
	got := FormatAsYamlLike(input, 0)
	// Each item should appear as "- item"
	if !strings.Contains(got, "- a") {
		t.Errorf("expected '- a' in output, got %q", got)
	}
	if !strings.Contains(got, "- b") {
		t.Errorf("expected '- b' in output, got %q", got)
	}
	if !strings.Contains(got, "- c") {
		t.Errorf("expected '- c' in output, got %q", got)
	}
}

func TestFormatAsYamlLike_SimpleMap(t *testing.T) {
	t.Parallel()
	// Use a single-key map to avoid ordering issues
	input := map[string]interface{}{"name": "alice"}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "name: alice") {
		t.Errorf("expected 'name: alice' in output, got %q", got)
	}
}

func TestFormatAsYamlLike_NestedMap(t *testing.T) {
	t.Parallel()
	input := map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": "value",
		},
	}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "outer:") {
		t.Errorf("expected 'outer:' in output, got %q", got)
	}
	if !strings.Contains(got, "inner: value") {
		t.Errorf("expected 'inner: value' in output, got %q", got)
	}
}

func TestFormatAsYamlLike_MapWithSlice(t *testing.T) {
	t.Parallel()
	input := map[string]interface{}{
		"items": []interface{}{"x", "y"},
	}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "items:") {
		t.Errorf("expected 'items:' in output, got %q", got)
	}
	if !strings.Contains(got, "- x") {
		t.Errorf("expected '- x' in output, got %q", got)
	}
	if !strings.Contains(got, "- y") {
		t.Errorf("expected '- y' in output, got %q", got)
	}
}

func TestFormatAsYamlLike_Struct(t *testing.T) {
	t.Parallel()
	type TestStruct struct {
		Name string
		Age  int
	}
	input := TestStruct{Name: "bob", Age: 30}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "Name: bob") {
		t.Errorf("expected 'Name: bob' in output, got %q", got)
	}
	if !strings.Contains(got, "Age: 30") {
		t.Errorf("expected 'Age: 30' in output, got %q", got)
	}
}

func TestFormatAsYamlLike_StructUnexportedSkipped(t *testing.T) {
	t.Parallel()
	type TestStruct struct {
		Public  string
		private string //nolint:unused
	}
	input := TestStruct{Public: "visible"}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "Public: visible") {
		t.Errorf("expected 'Public: visible' in output, got %q", got)
	}
	// Unexported field should not appear
	if strings.Contains(got, "private") {
		t.Errorf("unexported field should not appear, got %q", got)
	}
}

func TestFormatAsYamlLike_IndentAddsSpaces(t *testing.T) {
	t.Parallel()
	input := map[string]interface{}{"key": "val"}
	got := FormatAsYamlLike(input, 2)
	// indent=2 means 4 spaces prefix
	if !strings.HasPrefix(got, "    ") {
		t.Errorf("expected 4-space indent prefix at indent=2, got %q", got)
	}
}

func TestFormatAsYamlLike_SliceOfMaps(t *testing.T) {
	t.Parallel()
	input := []interface{}{
		map[string]interface{}{"id": 1},
		map[string]interface{}{"id": 2},
	}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "- id: 1") {
		t.Errorf("expected '- id: 1' in output, got %q", got)
	}
	if !strings.Contains(got, "- id: 2") {
		t.Errorf("expected '- id: 2' in output, got %q", got)
	}
}

func TestFormatAsYamlLike_Array(t *testing.T) {
	t.Parallel()
	// Test with a Go array (not slice)
	input := [3]string{"x", "y", "z"}
	got := FormatAsYamlLike(input, 0)
	if !strings.Contains(got, "- x") {
		t.Errorf("expected '- x' in output, got %q", got)
	}
}
