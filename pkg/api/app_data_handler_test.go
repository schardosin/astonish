package api

import (
	"testing"
)

func TestCredentialSuffixParsing(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantURL    string
		wantCred   string
	}{
		{
			name:     "no credential",
			url:      "https://api.example.com/data",
			wantURL:  "https://api.example.com/data",
			wantCred: "",
		},
		{
			name:     "simple credential",
			url:      "https://api.example.com/data@my-api-key",
			wantURL:  "https://api.example.com/data",
			wantCred: "my-api-key",
		},
		{
			name:     "credential with underscores",
			url:      "https://api.example.com/v2/users@github_token",
			wantURL:  "https://api.example.com/v2/users",
			wantCred: "github_token",
		},
		{
			name:     "URL with @ in basic auth (should not match)",
			url:      "https://user:pass@api.example.com/data",
			wantURL:  "https://user:pass@api.example.com/data",
			wantCred: "",
		},
		{
			name:     "URL with @ in path (should not match — has / after @)",
			url:      "https://api.example.com/@user/repos",
			wantURL:  "https://api.example.com/@user/repos",
			wantCred: "",
		},
		{
			name:     "URL with query params and credential",
			url:      "https://api.example.com/data?format=json@my-cred",
			wantURL:  "https://api.example.com/data?format=json",
			wantCred: "my-cred",
		},
		{
			name:     "credential starts with uppercase",
			url:      "https://api.example.com/data@MyCredential",
			wantURL:  "https://api.example.com/data",
			wantCred: "MyCredential",
		},
		{
			name:     "@ but no valid name after (digit start)",
			url:      "https://api.example.com/data@123",
			wantURL:  "https://api.example.com/data@123",
			wantCred: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.url
			var credentialName string
			if m := credentialSuffixRe.FindStringSubmatchIndex(url); m != nil {
				credentialName = url[m[2]:m[3]]
				url = url[:m[0]]
			}

			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if credentialName != tt.wantCred {
				t.Errorf("credential = %q, want %q", credentialName, tt.wantCred)
			}
		})
	}
}

func TestResolveHTTPSource_NoCredential(t *testing.T) {
	// Test that a basic request without credential still works.
	// We use a known-good public endpoint.
	data, err := resolveHTTPSource("GET:https://httpbin.org/get", nil)
	if err != nil {
		t.Skipf("skipping external HTTP test: %v", err)
	}
	if data == nil {
		t.Error("expected non-nil data")
	}
}
