package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"golang.org/x/image/draw"
)

const (
	// maxScreenshotDimension is the maximum pixels on the longest side.
	maxScreenshotDimension = 2000
	// maxScreenshotBytes is the target maximum file size (5MB).
	maxScreenshotBytes = 5 * 1024 * 1024
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
// The result is automatically compressed: resized if exceeding 2000px on the
// longest side, and for JPEG progressively quality-reduced to stay under 5MB.
func TakeScreenshot(pg *rod.Page, refs *RefMap, opts ScreenshotOptions) ([]byte, string, error) {
	format := proto.PageCaptureScreenshotFormatPng
	if opts.Format == "jpeg" || opts.Format == "jpg" {
		format = proto.PageCaptureScreenshotFormatJpeg
	}

	quality := opts.Quality
	if quality == 0 {
		quality = 80
	}

	var data []byte
	var err error

	// Element screenshot by ref
	if opts.Ref != "" {
		el, err := refs.ResolveElement(pg, opts.Ref)
		if err != nil {
			return nil, "", err
		}
		data, err = el.Screenshot(format, quality)
		if err != nil {
			return nil, "", err
		}
	} else if opts.Selector != "" {
		// Element screenshot by CSS selector
		el, err := pg.Element(opts.Selector)
		if err != nil {
			return nil, "", err
		}
		data, err = el.Screenshot(format, quality)
		if err != nil {
			return nil, "", err
		}
	} else {
		// Full page or viewport screenshot
		req := &proto.PageCaptureScreenshot{
			Format:  format,
			Quality: &quality,
		}
		data, err = pg.Screenshot(opts.FullPage, req)
		if err != nil {
			return nil, "", err
		}
	}

	// Apply compression: resize large images and reduce JPEG quality if needed.
	data, err = compressScreenshot(data, string(format), quality)
	if err != nil {
		// Compression failed — return the original uncompressed data.
		return data, string(format), nil
	}

	return data, string(format), nil
}

// compressScreenshot resizes images exceeding maxScreenshotDimension and
// progressively reduces JPEG quality to stay under maxScreenshotBytes.
func compressScreenshot(data []byte, format string, quality int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Resize if either dimension exceeds the limit.
	needsResize := w > maxScreenshotDimension || h > maxScreenshotDimension
	if needsResize {
		img = resizeImage(img, w, h)
	}

	// For PNG: just re-encode if resized, no quality knob.
	if format == "png" {
		if !needsResize {
			return data, nil
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return data, err
		}
		return buf.Bytes(), nil
	}

	// For JPEG: re-encode if resized or if over size limit.
	if !needsResize && len(data) <= maxScreenshotBytes {
		return data, nil
	}

	// Progressive quality reduction to fit under the size limit.
	q := quality
	for q >= 20 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return data, err
		}
		if buf.Len() <= maxScreenshotBytes {
			return buf.Bytes(), nil
		}
		q -= 10
	}

	// Last resort: encode at quality 20.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 20}); err != nil {
		return data, err
	}
	return buf.Bytes(), nil
}

// resizeImage scales an image so its longest side is maxScreenshotDimension,
// preserving aspect ratio.
func resizeImage(img image.Image, w, h int) image.Image {
	var newW, newH int
	if w >= h {
		newW = maxScreenshotDimension
		newH = h * maxScreenshotDimension / w
	} else {
		newH = maxScreenshotDimension
		newW = w * maxScreenshotDimension / h
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}
