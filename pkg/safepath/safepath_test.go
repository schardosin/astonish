package safepath

import (
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple name", "my-agent", false},
		{"valid with underscore", "my_agent_v2", false},
		{"valid with dots", "agent.v1", false},
		{"empty", "", true},
		{"forward slash", "foo/bar", true},
		{"backslash", "foo\\bar", true},
		{"dot-dot traversal", "..", true},
		{"single dot", ".", true},
		{"embedded traversal", "foo..bar", true},
		{"path traversal prefix", "../etc/passwd", true},
		{"null byte", "foo\x00bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestContainedWithin(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		baseDir string
		wantErr bool
	}{
		{"contained file", "/base/dir/file.txt", "/base/dir", false},
		{"contained subdir", "/base/dir/sub/file.txt", "/base/dir", false},
		{"equal to base", "/base/dir", "/base/dir", false},
		{"traversal escape", "/base/dir/../other/file.txt", "/base/dir", true},
		{"sibling dir", "/base/other/file.txt", "/base/dir", true},
		{"prefix trick", "/base/dir-evil/file.txt", "/base/dir", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ContainedWithin(tt.path, tt.baseDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ContainedWithin(%q, %q) error = %v, wantErr %v", tt.path, tt.baseDir, err, tt.wantErr)
			}
		})
	}
}
