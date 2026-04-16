package pdfgen

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:  "heading",
			input: "# Hello World",
			contains: []string{
				"<h1>Hello World</h1>",
			},
		},
		{
			name:  "bullet list",
			input: "- Item one\n- Item two\n- Item three",
			contains: []string{
				"<ul>",
				"<li>Item one</li>",
				"<li>Item two</li>",
				"</ul>",
			},
		},
		{
			name:  "GFM table",
			input: "| Name | Value |\n|------|-------|\n| A    | 1     |",
			contains: []string{
				"<table>",
				"<th>Name</th>",
				"<td>A</td>",
				"</table>",
			},
		},
		{
			name:  "bold and italic",
			input: "This is **bold** and *italic*.",
			contains: []string{
				"<strong>bold</strong>",
				"<em>italic</em>",
			},
		},
		{
			name:  "code block",
			input: "```go\nfunc main() {}\n```",
			contains: []string{
				"<pre><code",
				"func main()",
				"</code></pre>",
			},
		},
		{
			name:  "ampersand and quotes preserved",
			input: "Tom & Jerry said \"hello\"",
			contains: []string{
				"Tom &amp; Jerry", // goldmark HTML-encodes & for valid HTML
				"&quot;hello&quot;",
			},
		},
		{
			name:  "emoji preserved in HTML",
			input: "🚀 Launch day ⚠️ Warning",
			contains: []string{
				"🚀",
				"⚠️",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := markdownToHTML([]byte(tt.input))
			if err != nil {
				t.Fatalf("markdownToHTML failed: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(html, want) {
					t.Errorf("markdownToHTML output missing %q\ngot: %s", want, html)
				}
			}
		})
	}
}

func TestWrapInHTMLTemplate(t *testing.T) {
	body := "<h1>Test</h1><p>Hello world</p>"
	result := wrapInHTMLTemplate(body)

	checks := []string{
		"<!DOCTYPE html>",
		"<html lang=\"en\">",
		"<meta charset=\"UTF-8\">",
		"<style>",
		"font-family:",
		"@page",
		body,
		"</body>",
		"</html>",
	}

	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("wrapInHTMLTemplate missing %q", want)
		}
	}
}

func TestConvertMarkdownToPDFChrome_NilBrowser(t *testing.T) {
	_, err := ConvertMarkdownToPDFChrome([]byte("# Test"), nil)
	if err == nil {
		t.Fatal("expected error for nil browser, got nil")
	}
	if !strings.Contains(err.Error(), "no browser provider") {
		t.Errorf("unexpected error: %v", err)
	}
}
