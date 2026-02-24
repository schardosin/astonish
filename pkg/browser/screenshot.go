package browser

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// ScreenshotOptions controls screenshot capture.
type ScreenshotOptions struct {
	FullPage bool   // Capture the full scrollable page
	Format   string // "png" (default) or "jpeg"
	Quality  int    // JPEG quality 0-100 (default: 80)
	Ref      string // Element ref to screenshot (optional)
	Selector string // CSS selector to screenshot (optional)
}

// TakeScreenshot captures a screenshot of the page or a specific element.
func TakeScreenshot(pg *rod.Page, refs *RefMap, opts ScreenshotOptions) ([]byte, string, error) {
	format := proto.PageCaptureScreenshotFormatPng
	if opts.Format == "jpeg" || opts.Format == "jpg" {
		format = proto.PageCaptureScreenshotFormatJpeg
	}

	quality := opts.Quality
	if quality == 0 {
		quality = 80
	}

	// Element screenshot by ref
	if opts.Ref != "" {
		el, err := refs.ResolveElement(pg, opts.Ref)
		if err != nil {
			return nil, "", err
		}
		data, err := el.Screenshot(format, quality)
		if err != nil {
			return nil, "", err
		}
		return data, string(format), nil
	}

	// Element screenshot by CSS selector
	if opts.Selector != "" {
		el, err := pg.Element(opts.Selector)
		if err != nil {
			return nil, "", err
		}
		data, err := el.Screenshot(format, quality)
		if err != nil {
			return nil, "", err
		}
		return data, string(format), nil
	}

	// Full page or viewport screenshot
	req := &proto.PageCaptureScreenshot{
		Format:  format,
		Quality: &quality,
	}
	data, err := pg.Screenshot(opts.FullPage, req)
	if err != nil {
		return nil, "", err
	}
	return data, string(format), nil
}
