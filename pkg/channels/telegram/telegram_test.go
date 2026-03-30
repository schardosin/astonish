package telegram

import (
	"strings"
	"testing"
)

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		maxLen     int
		wantChunks int
		check      func(t *testing.T, chunks []string)
	}{
		{
			name:       "text shorter than maxLen",
			text:       "Hello world",
			maxLen:     100,
			wantChunks: 1,
			check: func(t *testing.T, chunks []string) {
				if chunks[0] != "Hello world" {
					t.Errorf("expected unchanged text, got %q", chunks[0])
				}
			},
		},
		{
			name:       "split at paragraph boundary",
			text:       "First paragraph.\n\nSecond paragraph.",
			maxLen:     25,
			wantChunks: 2,
			check: func(t *testing.T, chunks []string) {
				if !strings.HasPrefix(chunks[0], "First paragraph.") {
					t.Errorf("first chunk should start with first paragraph, got %q", chunks[0])
				}
				if !strings.Contains(chunks[1], "Second paragraph.") {
					t.Errorf("second chunk should contain second paragraph, got %q", chunks[1])
				}
			},
		},
		{
			name:       "split at line boundary",
			text:       "Line one.\nLine two is here.\nLine three.",
			maxLen:     30,
			wantChunks: 2,
			check: func(t *testing.T, chunks []string) {
				// Should split at a newline boundary
				for _, chunk := range chunks {
					if len(chunk) > 30 {
						t.Errorf("chunk exceeds maxLen: %d > 30", len(chunk))
					}
				}
			},
		},
		{
			name:       "split at word boundary",
			text:       "one two three four five six seven eight nine ten",
			maxLen:     20,
			wantChunks: 3,
			check: func(t *testing.T, chunks []string) {
				// Each chunk should be <= maxLen
				for i, chunk := range chunks {
					if len(chunk) > 20 {
						t.Errorf("chunk[%d] exceeds maxLen: len=%d, content=%q", i, len(chunk), chunk)
					}
				}
			},
		},
		{
			name:       "hard split when no boundary exists",
			text:       strings.Repeat("x", 50),
			maxLen:     20,
			wantChunks: 3, // 20 + 20 + 10
			check: func(t *testing.T, chunks []string) {
				total := 0
				for _, chunk := range chunks {
					total += len(chunk)
				}
				if total != 50 {
					t.Errorf("total chars = %d, want 50", total)
				}
			},
		},
		{
			name:       "multiple chunks for very long text",
			text:       strings.Repeat("word ", 100),
			maxLen:     50,
			wantChunks: -1, // don't check exact count, just > 1
			check: func(t *testing.T, chunks []string) {
				if len(chunks) <= 1 {
					t.Error("expected multiple chunks for long text")
				}
				// Rejoin should produce the original
				rejoined := strings.Join(chunks, "")
				if rejoined != strings.Repeat("word ", 100) {
					t.Error("rejoined chunks do not equal original")
				}
			},
		},
		{
			name:       "empty string returns single chunk",
			text:       "",
			maxLen:     100,
			wantChunks: 1,
			check: func(t *testing.T, chunks []string) {
				if chunks[0] != "" {
					t.Errorf("expected empty string chunk, got %q", chunks[0])
				}
			},
		},
		{
			name:       "exact maxLen",
			text:       "12345",
			maxLen:     5,
			wantChunks: 1,
			check: func(t *testing.T, chunks []string) {
				if chunks[0] != "12345" {
					t.Errorf("expected %q, got %q", "12345", chunks[0])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := splitMessage(tt.text, tt.maxLen)
			if tt.wantChunks > 0 && len(chunks) != tt.wantChunks {
				t.Errorf("splitMessage() returned %d chunks, want %d", len(chunks), tt.wantChunks)
			}
			if tt.check != nil {
				tt.check(t, chunks)
			}
		})
	}
}

func TestMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, output string)
	}{
		{
			name:  "fenced code block with language",
			input: "```go\nfmt.Println(\"hello\")\n```",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, `<pre><code class="language-go">`) {
					t.Errorf("expected language-go code block, got %q", output)
				}
				if !strings.Contains(output, `</code></pre>`) {
					t.Errorf("expected closing code/pre tags, got %q", output)
				}
			},
		},
		{
			name:  "fenced code block without language",
			input: "```\nsome code\n```",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<pre>") {
					t.Errorf("expected <pre> tag, got %q", output)
				}
				if strings.Contains(output, `class="language-"`) {
					t.Errorf("should not have empty language class, got %q", output)
				}
			},
		},
		{
			name:  "inline code",
			input: "use `fmt.Println` here",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<code>fmt.Println</code>") {
					t.Errorf("expected inline code tags, got %q", output)
				}
			},
		},
		{
			name:  "bold",
			input: "this is **bold** text",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<b>bold</b>") {
					t.Errorf("expected bold tags, got %q", output)
				}
			},
		},
		{
			name:  "italic with asterisk",
			input: "this is *italic* text",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<i>italic</i>") {
					t.Errorf("expected italic tags, got %q", output)
				}
			},
		},
		{
			name:  "underscore italic",
			input: "this is _italic_ text",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<i>italic</i>") {
					t.Errorf("expected italic tags for underscore, got %q", output)
				}
			},
		},
		{
			name:  "headings to bold",
			input: "## My Heading",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<b>My Heading</b>") {
					t.Errorf("expected heading converted to bold, got %q", output)
				}
			},
		},
		{
			name:  "HTML escaping",
			input: "use <div> & \"quotes\"",
			check: func(t *testing.T, output string) {
				if strings.Contains(output, "<div>") {
					t.Errorf("HTML should be escaped, got %q", output)
				}
				if !strings.Contains(output, "&lt;div&gt;") {
					t.Errorf("expected escaped HTML entities, got %q", output)
				}
				if !strings.Contains(output, "&amp;") {
					t.Errorf("expected escaped ampersand, got %q", output)
				}
			},
		},
		{
			name:  "list items",
			input: "- item one\n- item two",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "• item one") {
					t.Errorf("expected bullet point, got %q", output)
				}
				if !strings.Contains(output, "• item two") {
					t.Errorf("expected second bullet point, got %q", output)
				}
			},
		},
		{
			name:  "excess blank lines collapsed",
			input: "line1\n\n\n\n\nline2",
			check: func(t *testing.T, output string) {
				if strings.Contains(output, "\n\n\n") {
					t.Errorf("excess blank lines should be collapsed, got %q", output)
				}
			},
		},
		{
			name:  "code block content is HTML-escaped",
			input: "```\n<script>alert('xss')</script>\n```",
			check: func(t *testing.T, output string) {
				if strings.Contains(output, "<script>") {
					t.Errorf("script tag should be escaped inside code block, got %q", output)
				}
				if !strings.Contains(output, "&lt;script&gt;") {
					t.Errorf("expected escaped script tag, got %q", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := markdownToTelegramHTML(tt.input)
			tt.check(t, output)
		})
	}
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple tags removed", "<b>bold</b> text", "bold text"},
		{"nested tags removed", "<div><p>hello</p></div>", "hello"},
		{"entities unescaped", "&lt;hello&gt; &amp; world", "<hello> & world"},
		{"no tags unchanged", "plain text", "plain text"},
		{"empty string", "", ""},
		{"self-closing tag", "before<br/>after", "beforeafter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTMLTags(tt.input)
			if got != tt.want {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
