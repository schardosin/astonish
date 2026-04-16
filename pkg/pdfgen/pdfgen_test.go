package pdfgen

import (
	"bytes"
	"compress/flate"
	"io"
	"regexp"
	"strings"
	"testing"
)

func TestConvertMarkdownToPDF_Basic(t *testing.T) {
	source := []byte(`# Hello World

This is a **bold** and *italic* test.

## Features

- Item one
- Item two
- Item three

### Code Example

` + "```go" + `
func main() {
    fmt.Println("Hello, World!")
}
` + "```" + `

> This is a blockquote with some wisdom.

Here is a [link](https://example.com) to test.

---

| Name  | Value |
|-------|-------|
| Alpha | 1     |
| Beta  | 2     |
`)

	pdf, err := ConvertMarkdownToPDF(source)
	if err != nil {
		t.Fatalf("ConvertMarkdownToPDF failed: %v", err)
	}

	if len(pdf) == 0 {
		t.Fatal("ConvertMarkdownToPDF returned empty output")
	}

	// PDF files start with "%PDF-"
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		t.Fatalf("output does not look like a PDF; first bytes: %q", pdf[:min(20, len(pdf))])
	}

	t.Logf("Generated PDF: %d bytes", len(pdf))
}

func TestConvertMarkdownToPDF_Empty(t *testing.T) {
	pdf, err := ConvertMarkdownToPDF([]byte(""))
	if err != nil {
		t.Fatalf("ConvertMarkdownToPDF failed on empty input: %v", err)
	}

	if len(pdf) == 0 {
		t.Fatal("ConvertMarkdownToPDF returned empty output for empty markdown")
	}

	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		t.Fatalf("output does not look like a PDF; first bytes: %q", pdf[:min(20, len(pdf))])
	}
}

func TestConvertMarkdownToPDF_LargeContent(t *testing.T) {
	// Generate content that spans multiple pages
	source := []byte("# Long Document\n\n")
	for i := 0; i < 100; i++ {
		source = append(source, []byte("This is paragraph number. It contains enough text to test that multi-page documents render correctly without errors.\n\n")...)
	}

	pdf, err := ConvertMarkdownToPDF(source)
	if err != nil {
		t.Fatalf("ConvertMarkdownToPDF failed on large input: %v", err)
	}

	if len(pdf) < 1000 {
		t.Fatalf("large document PDF seems too small: %d bytes", len(pdf))
	}

	t.Logf("Generated large PDF: %d bytes", len(pdf))
}

func TestConvertMarkdownToPDF_NoHTMLEntitiesInOutput(t *testing.T) {
	// Verify that the PDF output does not contain HTML-encoded entities.
	// This catches a goldmark-pdf bug where EscapeHTML defaults to true,
	// causing & to become &amp;, " to become &quot;, etc. in PDF text streams.
	source := []byte(`# Report

Tom & Jerry said "hello" world.

GTC 2025 & 2026 -- Blackwell Ultra.

Key Facts & Timeline: "important" <tag> values.

LLM entities: &amp; and &quot;quoted&quot; and &lt;angle&gt;.
`)

	pdfData, err := ConvertMarkdownToPDF(source)
	if err != nil {
		t.Fatalf("ConvertMarkdownToPDF failed: %v", err)
	}

	// Decompress PDF content streams and check for HTML entities
	re := regexp.MustCompile(`(?s)stream\r?\n(.+?)\r?\nendstream`)
	matches := re.FindAllSubmatch(pdfData, -1)
	if len(matches) == 0 {
		t.Fatal("no streams found in PDF output")
	}

	badEntities := []string{"&amp;", "&quot;", "&lt;", "&gt;"}

	for i, m := range matches {
		reader := flate.NewReader(bytes.NewReader(m[1]))
		decompressed, err := io.ReadAll(reader)
		if err != nil {
			continue // skip non-flate streams
		}
		text := string(decompressed)
		for _, ent := range badEntities {
			if strings.Contains(text, ent) {
				t.Errorf("PDF stream %d contains HTML entity %q; goldmark-pdf EscapeHTML may be enabled", i, ent)
			}
		}
	}
}
