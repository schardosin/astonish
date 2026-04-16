package pdfgen

import (
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
