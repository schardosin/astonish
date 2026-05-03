package astonish

import (
	"testing"
)

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateStr(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestGenerateTempPassword(t *testing.T) {
	p1 := generateTempPassword()
	p2 := generateTempPassword()

	if len(p1) < 8 {
		t.Errorf("generateTempPassword() returned %d chars, want >= 8", len(p1))
	}
	if p1 == p2 {
		t.Error("generateTempPassword() returned same value twice")
	}
}
