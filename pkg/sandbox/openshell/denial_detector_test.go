package openshell

import (
	"testing"
)

func TestSuggestBroaderPattern(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"identity-3.qa-de-1.cloud.sap", "**.cloud.sap"},
		{"api.internal.mycompany.com", "**.mycompany.com"},
		{"sub.example.com", "*.example.com"},
		{"example.com", ""},
		{"localhost", ""},
		{"a.b.c.d.example.org", "**.example.org"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := suggestBroaderPattern(tt.host)
			if got != tt.want {
				t.Errorf("suggestBroaderPattern(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}
