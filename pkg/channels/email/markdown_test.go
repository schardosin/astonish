package email

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, output string)
	}{
		{
			name: "table becomes HTML table not bullets",
			input: strings.Join([]string{
				"| Service | Status |",
				"|---------|--------|",
				"| API | ✅ ACTIVE |",
				"| DB | 🟢 OK |",
			}, "\n"),
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<table") {
					t.Fatalf("expected <table>, got %q", output)
				}
				if !strings.Contains(output, "<th") || !strings.Contains(output, "Service") {
					t.Fatalf("expected header cells, got %q", output)
				}
				if !strings.Contains(output, "<td") || !strings.Contains(output, "ACTIVE") {
					t.Fatalf("expected body cells, got %q", output)
				}
				if strings.Contains(output, "• ") || strings.Contains(output, " — ") {
					t.Fatalf("tables must not flatten to bullets, got %q", output)
				}
			},
		},
		{
			name:  "headings",
			input: "## OpenStack Status\n\nAll good.",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<h2") || !strings.Contains(output, "OpenStack Status") {
					t.Fatalf("expected h2 heading, got %q", output)
				}
				if !strings.Contains(output, "<p") || !strings.Contains(output, "All good.") {
					t.Fatalf("expected paragraph, got %q", output)
				}
			},
		},
		{
			name:  "bold and italic",
			input: "This is **bold** and *italic* and _also_.",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<strong>bold</strong>") {
					t.Fatalf("expected strong, got %q", output)
				}
				if !strings.Contains(output, "<em>italic</em>") {
					t.Fatalf("expected em italic, got %q", output)
				}
				if !strings.Contains(output, "<em>also</em>") {
					t.Fatalf("expected em underscore, got %q", output)
				}
			},
		},
		{
			name:  "unordered list",
			input: "- first\n- second",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<ul") || !strings.Contains(output, "<li") {
					t.Fatalf("expected ul/li, got %q", output)
				}
				if !strings.Contains(output, "first") || !strings.Contains(output, "second") {
					t.Fatalf("expected list items, got %q", output)
				}
			},
		},
		{
			name:  "ordered list",
			input: "1. alpha\n2. beta",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<ol") {
					t.Fatalf("expected ol, got %q", output)
				}
				if !strings.Contains(output, "alpha") || !strings.Contains(output, "beta") {
					t.Fatalf("expected ordered items, got %q", output)
				}
			},
		},
		{
			name:  "HTML escaping in cells",
			input: "| Col |\n|-----|\n| <script>alert(1)</script> & x |",
			check: func(t *testing.T, output string) {
				if strings.Contains(output, "<script>") {
					t.Fatalf("script must be escaped, got %q", output)
				}
				if !strings.Contains(output, "&lt;script&gt;") {
					t.Fatalf("expected escaped script, got %q", output)
				}
				if !strings.Contains(output, "&amp;") {
					t.Fatalf("expected escaped ampersand, got %q", output)
				}
			},
		},
		{
			name:  "fenced code escaped",
			input: "```\n<script>alert('xss')</script>\n```",
			check: func(t *testing.T, output string) {
				if strings.Contains(output, "<script>") {
					t.Fatalf("script in code must be escaped, got %q", output)
				}
				if !strings.Contains(output, "<pre") || !strings.Contains(output, "&lt;script&gt;") {
					t.Fatalf("expected pre with escaped content, got %q", output)
				}
			},
		},
		{
			name:  "inline code",
			input: "Run `kubectl get pods` now.",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "<code") || !strings.Contains(output, "kubectl get pods") {
					t.Fatalf("expected inline code, got %q", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, MarkdownToHTML(tt.input))
		})
	}
}

func TestMarkdownToEmailHTML_WrapsDocument(t *testing.T) {
	t.Parallel()
	got := markdownToEmailHTML("**Scheduled Job: demo**\n\n| A | B |\n|---|---|\n| 1 | 2 |")
	if !strings.Contains(got, "<!DOCTYPE html>") {
		t.Fatalf("expected document wrapper, got %q", got)
	}
	if !strings.Contains(got, "<table") {
		t.Fatalf("expected table in email HTML, got %q", got)
	}
	if !strings.Contains(got, "<strong>Scheduled Job: demo</strong>") {
		t.Fatalf("expected bold job title, got %q", got)
	}
	if strings.Contains(got, "• ") {
		t.Fatalf("must not use telegram bullet tables, got %q", got)
	}
}
