package pdfgen

import (
	"testing"
)

func TestPreprocessMarkdown_HTMLEntities(t *testing.T) {
	input := `Dan Muse &amp; John &mdash; co-authors. &quot;Hello&quot; they said.`
	got := preprocessMarkdown(input)

	// After HTML unescape, the em-dash (&mdash; → —) should then be replaced by --
	want := `Dan Muse & John -- co-authors. "Hello" they said.`
	if got != want {
		t.Errorf("preprocessMarkdown() = %q, want %q", got, want)
	}
}

func TestPreprocessMarkdown_SmartQuotes(t *testing.T) {
	input := "\u201CHello\u201D she said, \u2018it\u2019s fine\u2019"
	got := preprocessMarkdown(input)
	want := `"Hello" she said, 'it's fine'`
	if got != want {
		t.Errorf("preprocessMarkdown() = %q, want %q", got, want)
	}
}

func TestPreprocessMarkdown_EmDash(t *testing.T) {
	input := "first \u2014 second \u2013 third"
	got := preprocessMarkdown(input)
	want := "first -- second - third"
	if got != want {
		t.Errorf("preprocessMarkdown() = %q, want %q", got, want)
	}
}

func TestPreprocessMarkdown_Ellipsis(t *testing.T) {
	input := "wait for it\u2026 done"
	got := preprocessMarkdown(input)
	want := "wait for it... done"
	if got != want {
		t.Errorf("preprocessMarkdown() = %q, want %q", got, want)
	}
}

func TestPreprocessMarkdown_DoubleEncodedEntities(t *testing.T) {
	// LLMs sometimes produce double-encoded HTML entities
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "double-encoded ampersand",
			input: "Tom &amp;amp; Jerry",
			want:  "Tom & Jerry",
		},
		{
			name:  "double-encoded quotes",
			input: "said &amp;quot;hello&amp;quot;",
			want:  `said "hello"`,
		},
		{
			name:  "triple-encoded ampersand",
			input: "&amp;amp;amp; start",
			want:  "& start",
		},
		{
			name:  "mixed single and double encoded",
			input: "A &amp; B &amp;amp; C &amp;quot;D&amp;quot;",
			want:  `A & B & C "D"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preprocessMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("preprocessMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPreprocessMarkdown_Combined(t *testing.T) {
	// Simulates typical LLM-generated content with mixed encoding issues
	input := `&quot;Apple&quot; released iOS 19 &amp; macOS 16 ` +
		"\u2014 Dan Muse &amp; contributors \u2022 " +
		"\u201CRevolutionary\u201D features \u2026"
	got := preprocessMarkdown(input)
	want := `"Apple" released iOS 19 & macOS 16 -- Dan Muse & contributors * "Revolutionary" features ...`
	if got != want {
		t.Errorf("preprocessMarkdown():\n  got:  %q\n  want: %q", got, want)
	}
}
