package tools

import (
	"testing"
)

func TestCheckSSRF(t *testing.T) {
	tests := []struct {
		name      string
		hostname  string
		wantError bool
	}{
		{
			name:      "PublicHost",
			hostname:  "example.com",
			wantError: false,
		},
		{
			name:      "Localhost",
			hostname:  "localhost",
			wantError: true,
		},
		{
			name:      "LoopbackIP",
			hostname:  "127.0.0.1",
			wantError: true,
		},
		{
			name:      "PrivateIP_10",
			hostname:  "10.0.0.1",
			wantError: true,
		},
		{
			name:      "PrivateIP_192",
			hostname:  "192.168.1.1",
			wantError: true,
		},
		{
			name:      "PrivateIP_172",
			hostname:  "172.16.0.1",
			wantError: true,
		},
		{
			name:      "UnresolvableHost",
			hostname:  "this-host-does-not-exist-astonish.invalid",
			wantError: false, // unresolvable hosts are allowed (HTTP client will handle)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSSRF(tt.hostname)
			if tt.wantError && err == nil {
				t.Errorf("expected error for hostname %q, got nil", tt.hostname)
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error for hostname %q: %v", tt.hostname, err)
			}
		})
	}
}

func TestExtractTitleFromHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "BasicTitle",
			html: `<html><head><title>Hello World</title></head><body></body></html>`,
			want: "Hello World",
		},
		{
			name: "NoTitle",
			html: `<html><head></head><body><p>Hello</p></body></html>`,
			want: "",
		},
		{
			name: "EmptyTitle",
			html: `<html><head><title></title></head><body></body></html>`,
			want: "",
		},
		{
			name: "TitleWithWhitespace",
			html: `<html><head><title>  Trimmed Title  </title></head><body></body></html>`,
			want: "Trimmed Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitleFromHTML(tt.html)
			if got != tt.want {
				t.Errorf("extractTitleFromHTML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "BasicParagraph",
			html: `<p>Hello world</p>`,
			want: "Hello world",
		},
		{
			name: "ScriptStripped",
			html: `<p>Before</p><script>alert('xss')</script><p>After</p>`,
			want: "Before\nAfter",
		},
		{
			name: "StyleStripped",
			html: `<style>body{color:red}</style><p>Content</p>`,
			want: "Content",
		},
		{
			name: "NestedTags",
			html: `<div><p>Nested <strong>bold</strong> text</p></div>`,
			want: "Nested\nbold\ntext",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.html)
			if got != tt.want {
				t.Errorf("stripHTML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrettyJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "SimpleObject",
			input: `{"key":"value"}`,
			want:  "{\n  \"key\": \"value\"\n}",
		},
		{
			name:    "InvalidJSON",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:  "Array",
			input: `[1,2,3]`,
			want:  "[\n  1,\n  2,\n  3\n]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prettyJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("prettyJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractHTML_Raw(t *testing.T) {
	html := `<html><head><title>Test Page</title></head><body><p>Hello</p></body></html>`
	content, title, err := extractHTML(html, "https://example.com", "raw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Test Page" {
		t.Errorf("title = %q, want %q", title, "Test Page")
	}
	if content != html {
		t.Errorf("content should be raw HTML")
	}
}

func TestExtractHTML_Markdown(t *testing.T) {
	htmlInput := `<html><head><title>Test</title></head><body><article><h1>Main Heading</h1><p>Some paragraph text here.</p></article></body></html>`
	content, _, err := extractHTML(htmlInput, "https://example.com", "markdown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content == "" {
		t.Error("expected non-empty content")
	}
	// The content should contain some text from the original
	if !containsSubstring(content, "paragraph text") && !containsSubstring(content, "Main Heading") {
		t.Errorf("content should contain article text, got: %q", content)
	}
}

func TestWebFetch_Validation(t *testing.T) {
	tests := []struct {
		name    string
		args    WebFetchArgs
		wantErr string
	}{
		{
			name:    "EmptyURL",
			args:    WebFetchArgs{URL: ""},
			wantErr: "url is required",
		},
		{
			name:    "InvalidScheme",
			args:    WebFetchArgs{URL: "ftp://example.com/file"},
			wantErr: "only http and https URLs are supported",
		},
		{
			name:    "InvalidMode",
			args:    WebFetchArgs{URL: "https://example.com", Mode: "xml"},
			wantErr: "invalid mode",
		},
		{
			name:    "InvalidURL",
			args:    WebFetchArgs{URL: "not a url"},
			wantErr: "invalid URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := WebFetch(nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
