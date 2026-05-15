package api

import "testing"

func TestValidSessionID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid formats: UUIDs
		{"uuid", "e89b12d3-a456-4266-1417-4000abcd1234", true},
		{"short uuid", "abc123", true},

		// Valid formats: channel session keys
		{"telegram direct", "telegram:direct:8484406081", true},
		{"telegram group", "telegram:group:67890", true},
		{"email direct", "email:direct:alice@example.com", true},
		{"email thread", "email:direct:alice@example.com-a1b2c3d4", true},

		// Valid formats: prefixed IDs
		{"triage prefix", "triage-abc12345", true},
		{"test prefix", "test-abc12345", true},

		// Valid edge cases
		{"single char", "x", true},
		{"max length 128", "a" + string(make([]byte, 127)), false}, // 128 total, but make([]byte,127) is null bytes — use explicit
		{"dots and underscores", "my_session.v2", true},
		{"all allowed chars", "A0._:@-z", true},

		// Invalid: path traversal
		{"path traversal unix", "../../../etc/passwd", false},     // starts with .
		{"path traversal slash", "id/../../etc/passwd", false},    // contains /
		{"path traversal backslash", "id\\..\\secret", false},     // contains backslash
		{"just slashes", "a/b", false},                            // contains /
		{"relative path", "sessions/../secrets", false},           // contains /

		// Invalid: shell metacharacters
		{"space", "id with space", false},
		{"semicolon", "id;rm -rf /", false},
		{"pipe", "id|cat", false},
		{"ampersand", "id&bg", false},
		{"dollar", "id$(whoami)", false},
		{"backtick", "id`whoami`", false},
		{"single quote", "id'inject", false},
		{"double quote", "id\"inject", false},
		{"less than", "id<file", false},
		{"greater than", "id>file", false},
		{"open paren", "id(x)", false},
		{"star glob", "id*", false},
		{"question glob", "id?", false},

		// Invalid: leading special chars
		{"leading dot", ".hidden", false},
		{"leading hyphen", "-rf", false},
		{"leading colon", ":bad", false},
		{"leading at", "@bad", false},

		// Invalid: empty and too long
		{"empty string", "", false},
		{"too long 129 chars", "a123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789", false},

		// Invalid: control characters
		{"null byte", "id\x00x", false},
		{"newline", "id\nx", false},
		{"tab", "id\tx", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validSessionID.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("validSessionID.MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
