// Package pdfgen converts markdown content to PDF using goldmark and goldmark-pdf.
// It uses only built-in PDF fonts (Helvetica, Courier) so no external font files
// or network downloads are required.
package pdfgen

import (
	"bytes"
	"fmt"
	"image/color"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	pdf "github.com/stephenafamo/goldmark-pdf"
)

// ConvertMarkdownToPDF converts markdown source bytes to PDF bytes.
// The resulting PDF uses built-in fonts (Helvetica for body, Courier for code)
// and requires no external dependencies or network access.
func ConvertMarkdownToPDF(source []byte) ([]byte, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM), // tables, strikethrough, autolinks
		goldmark.WithRenderer(
			pdf.New(
				// Use built-in PDF fonts — no network downloads needed.
				pdf.WithHeadingFont(pdf.FontHelvetica),
				pdf.WithBodyFont(pdf.FontHelvetica),
				pdf.WithCodeFont(pdf.FontCourier),
				pdf.WithLinkColor(color.RGBA{R: 0, G: 102, B: 204, A: 255}),
			),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert(source, &buf); err != nil {
		return nil, fmt.Errorf("markdown to PDF conversion failed: %w", err)
	}

	return buf.Bytes(), nil
}
