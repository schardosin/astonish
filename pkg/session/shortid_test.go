package session

import (
	"testing"
)

func TestShortID(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		allIDs []string
		want   string
	}{
		{
			name:   "unique at min length",
			id:     "abcdefghijklmnop",
			allIDs: []string{"abcdefghijklmnop", "zyxwvuts12345678"},
			want:   "abcdefgh", // 8-char min is sufficient
		},
		{
			name:   "needs longer prefix",
			id:     "email:direct:alice@example.com",
			allIDs: []string{"email:direct:alice@example.com", "email:direct:bob@example.com"},
			want:   "email:direct:a", // diverges at char 14
		},
		{
			name:   "identical prefix needs more chars",
			id:     "abcdefghij_first",
			allIDs: []string{"abcdefghij_first", "abcdefghij_second"},
			want:   "abcdefghij_f",
		},
		{
			name:   "single ID returns min prefix",
			id:     "abcdefghijklmnop",
			allIDs: []string{"abcdefghijklmnop"},
			want:   "abcdefgh",
		},
		{
			name:   "short ID returned as-is",
			id:     "abc",
			allIDs: []string{"abc", "xyz"},
			want:   "abc",
		},
		{
			name:   "exactly min length",
			id:     "abcdefgh",
			allIDs: []string{"abcdefgh", "zyxwvuts"},
			want:   "abcdefgh",
		},
		{
			name:   "full ID needed when one is prefix of another",
			id:     "abcdefghij",
			allIDs: []string{"abcdefghij", "abcdefghijk"},
			want:   "abcdefghij",
		},
		{
			name:   "empty list",
			id:     "abcdefghij",
			allIDs: []string{},
			want:   "abcdefgh",
		},
		{
			name:   "channel session IDs distinguished",
			id:     "telegram:group:12345",
			allIDs: []string{"telegram:group:12345", "telegram:group:67890"},
			want:   "telegram:group:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShortID(tt.id, tt.allIDs)
			if got != tt.want {
				t.Errorf("ShortID(%q, %v) = %q, want %q", tt.id, tt.allIDs, got, tt.want)
			}
		})
	}
}

func TestShortIDs(t *testing.T) {
	allIDs := []string{
		"email:direct:alice@example.com",
		"email:direct:bob@example.com",
		"telegram:group:12345",
		"abc123defgh",
	}

	result := ShortIDs(allIDs)

	if len(result) != len(allIDs) {
		t.Fatalf("ShortIDs returned %d entries, want %d", len(result), len(allIDs))
	}

	// All short IDs should be present
	for _, id := range allIDs {
		if _, ok := result[id]; !ok {
			t.Errorf("ShortIDs missing entry for %q", id)
		}
	}

	// email IDs should be different from each other
	aliceShort := result["email:direct:alice@example.com"]
	bobShort := result["email:direct:bob@example.com"]
	if aliceShort == bobShort {
		t.Errorf("alice and bob got same short ID: %q", aliceShort)
	}

	// telegram ID should be short (unique at min length)
	teleShort := result["telegram:group:12345"]
	if len(teleShort) > 8 {
		t.Errorf("telegram short ID longer than expected: %q (len %d)", teleShort, len(teleShort))
	}
}

func TestSafeShortID(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		maxLen int
		want   string
	}{
		{
			name:   "longer than max",
			id:     "email:direct:alice@example.com",
			maxLen: 16,
			want:   "email:direct:ali",
		},
		{
			name:   "shorter than max",
			id:     "short",
			maxLen: 16,
			want:   "short",
		},
		{
			name:   "exact max length",
			id:     "abcdefghijklmnop",
			maxLen: 16,
			want:   "abcdefghijklmnop",
		},
		{
			name:   "zero maxLen defaults to 16",
			id:     "email:direct:alice@example.com",
			maxLen: 0,
			want:   "email:direct:ali",
		},
		{
			name:   "negative maxLen defaults to 16",
			id:     "email:direct:alice@example.com",
			maxLen: -5,
			want:   "email:direct:ali",
		},
		{
			name:   "empty string",
			id:     "",
			maxLen: 16,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeShortID(tt.id, tt.maxLen)
			if got != tt.want {
				t.Errorf("SafeShortID(%q, %d) = %q, want %q", tt.id, tt.maxLen, got, tt.want)
			}
		})
	}
}
