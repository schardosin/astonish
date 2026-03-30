package drill

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// CompareImages compares two PNG images pixel by pixel and returns the
// fraction of pixels that differ beyond the anti-aliasing tolerance.
// Returns (diffFraction, diffImage, error).
// diffImage is a PNG-encoded image highlighting the differences in red.
func CompareImages(baseline, actual []byte, threshold float64) (float64, []byte, error) {
	baseImg, err := png.Decode(bytes.NewReader(baseline))
	if err != nil {
		return 0, nil, fmt.Errorf("decode baseline: %w", err)
	}

	actualImg, err := png.Decode(bytes.NewReader(actual))
	if err != nil {
		return 0, nil, fmt.Errorf("decode actual: %w", err)
	}

	baseBounds := baseImg.Bounds()
	actualBounds := actualImg.Bounds()

	// Use the larger dimensions for comparison
	width := baseBounds.Dx()
	height := baseBounds.Dy()
	if actualBounds.Dx() > width {
		width = actualBounds.Dx()
	}
	if actualBounds.Dy() > height {
		height = actualBounds.Dy()
	}

	if width == 0 || height == 0 {
		return 0, nil, fmt.Errorf("images have zero dimensions")
	}

	totalPixels := width * height
	diffCount := 0

	// Create diff image
	diffImg := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Get pixel from each image (out-of-bounds = transparent)
			var basePixel, actualPixel color.Color
			if x < baseBounds.Dx() && y < baseBounds.Dy() {
				basePixel = baseImg.At(baseBounds.Min.X+x, baseBounds.Min.Y+y)
			} else {
				basePixel = color.RGBA{0, 0, 0, 0}
			}
			if x < actualBounds.Dx() && y < actualBounds.Dy() {
				actualPixel = actualImg.At(actualBounds.Min.X+x, actualBounds.Min.Y+y)
			} else {
				actualPixel = color.RGBA{0, 0, 0, 0}
			}

			if pixelsDiffer(basePixel, actualPixel) {
				diffCount++
				// Mark diff pixels in red
				diffImg.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 200})
			} else {
				// Dim matching pixels
				r, g, b, a := actualPixel.RGBA()
				diffImg.Set(x, y, color.RGBA{
					R: uint8(r >> 9), // half brightness
					G: uint8(g >> 9),
					B: uint8(b >> 9),
					A: uint8(a >> 8),
				})
			}
		}
	}

	diffPct := float64(diffCount) / float64(totalPixels)

	// Encode diff image only if there are differences
	var diffPNG []byte
	if diffCount > 0 {
		var buf bytes.Buffer
		if err := png.Encode(&buf, diffImg); err != nil {
			return diffPct, nil, fmt.Errorf("encode diff image: %w", err)
		}
		diffPNG = buf.Bytes()
	}

	return diffPct, diffPNG, nil
}

// pixelsDiffer returns true if two pixels differ by more than the
// anti-aliasing tolerance (10 per channel out of 255).
func pixelsDiffer(a, b color.Color) bool {
	const tolerance = 10 * 257 // tolerance per channel in 16-bit color space

	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()

	return absDiff(r1, r2) > tolerance ||
		absDiff(g1, g2) > tolerance ||
		absDiff(b1, b2) > tolerance ||
		absDiff(a1, a2) > tolerance
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// BaselineDir returns the directory where visual baselines are stored.
// If an artifact manager is available, baselines go alongside artifacts.
// Otherwise, uses ~/.config/astonish/drill-baselines/.
func BaselineDir(am *ArtifactManager) string {
	if am != nil {
		return filepath.Join(filepath.Dir(am.baseDir), "baselines")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "astonish-drill-baselines")
	}
	return filepath.Join(home, ".config", "astonish", "drill-baselines")
}

// SaveBaseline saves PNG image data as a baseline for visual regression tests.
func SaveBaseline(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create baseline dir: %w", err)
	}
	path := filepath.Join(dir, name+".png")
	return os.WriteFile(path, data, 0o644)
}

// LoadBaseline loads a previously saved baseline image.
// Returns os.ErrNotExist if the baseline doesn't exist yet.
func LoadBaseline(dir, name string) ([]byte, error) {
	path := filepath.Join(dir, name+".png")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}
