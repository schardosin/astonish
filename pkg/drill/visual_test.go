package drill

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func createTestPNG(t *testing.T, width, height int, fill color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func createTestPNGWithRegion(t *testing.T, width, height int, bg, region color.Color, rx, ry, rw, rh int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x >= rx && x < rx+rw && y >= ry && y < ry+rh {
				img.Set(x, y, region)
			} else {
				img.Set(x, y, bg)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestCompareImagesIdentical(t *testing.T) {
	img := createTestPNG(t, 100, 100, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	diffPct, diffImg, err := CompareImages(img, img, 0.01)
	if err != nil {
		t.Fatalf("CompareImages: %v", err)
	}
	if diffPct != 0 {
		t.Errorf("diffPct = %f, want 0", diffPct)
	}
	if diffImg != nil {
		t.Error("diffImg should be nil for identical images")
	}
}

func TestCompareImagesTotallyDifferent(t *testing.T) {
	baseline := createTestPNG(t, 100, 100, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	actual := createTestPNG(t, 100, 100, color.RGBA{R: 0, G: 0, B: 255, A: 255})

	diffPct, diffImg, err := CompareImages(baseline, actual, 0.01)
	if err != nil {
		t.Fatalf("CompareImages: %v", err)
	}
	if diffPct != 1.0 {
		t.Errorf("diffPct = %f, want 1.0 (all pixels differ)", diffPct)
	}
	if diffImg == nil {
		t.Error("diffImg should not be nil for different images")
	}
}

func TestCompareImagesPartialDiff(t *testing.T) {
	// 100x100 = 10000 pixels. 10x10 region = 100 pixels = 1%
	bg := color.RGBA{R: 128, G: 128, B: 128, A: 255}
	baseline := createTestPNG(t, 100, 100, bg)
	actual := createTestPNGWithRegion(t, 100, 100, bg,
		color.RGBA{R: 255, G: 0, B: 0, A: 255}, 0, 0, 10, 10)

	diffPct, _, err := CompareImages(baseline, actual, 0.02)
	if err != nil {
		t.Fatalf("CompareImages: %v", err)
	}
	// 100 / 10000 = 0.01 (1%)
	if diffPct < 0.009 || diffPct > 0.011 {
		t.Errorf("diffPct = %f, want ~0.01 (1%%)", diffPct)
	}
}

func TestCompareImagesDifferentSize(t *testing.T) {
	baseline := createTestPNG(t, 100, 100, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	actual := createTestPNG(t, 100, 50, color.RGBA{R: 128, G: 128, B: 128, A: 255})

	diffPct, _, err := CompareImages(baseline, actual, 0.01)
	if err != nil {
		t.Fatalf("CompareImages: %v", err)
	}
	// The bottom 50 rows of actual are transparent (out-of-bounds), while baseline has gray
	// 50*100 = 5000 pixels differ out of 10000 = 50%
	if diffPct < 0.45 || diffPct > 0.55 {
		t.Errorf("diffPct = %f, want ~0.50 (50%%)", diffPct)
	}
}

func TestCompareImagesAntiAliasTolerance(t *testing.T) {
	// Colors that differ by less than the tolerance (10 per channel)
	baseline := createTestPNG(t, 10, 10, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	actual := createTestPNG(t, 10, 10, color.RGBA{R: 133, G: 128, B: 128, A: 255})

	diffPct, _, err := CompareImages(baseline, actual, 0.01)
	if err != nil {
		t.Fatalf("CompareImages: %v", err)
	}
	if diffPct != 0 {
		t.Errorf("diffPct = %f, want 0 (within anti-alias tolerance)", diffPct)
	}
}

func TestCompareImagesInvalidPNG(t *testing.T) {
	valid := createTestPNG(t, 10, 10, color.White)

	_, _, err := CompareImages([]byte("not a png"), valid, 0.01)
	if err == nil {
		t.Error("expected error for invalid baseline")
	}

	_, _, err = CompareImages(valid, []byte("not a png"), 0.01)
	if err == nil {
		t.Error("expected error for invalid actual")
	}
}

func TestSaveLoadBaseline(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "visual-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imgData := createTestPNG(t, 50, 50, color.RGBA{R: 0, G: 255, B: 0, A: 255})

	if err := SaveBaseline(tmpDir, "test-baseline", imgData); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	loaded, err := LoadBaseline(tmpDir, "test-baseline")
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	if !bytes.Equal(imgData, loaded) {
		t.Error("loaded baseline does not match saved data")
	}
}

func TestLoadBaselineNotExist(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "visual-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = LoadBaseline(tmpDir, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent baseline")
	}
}

func TestBaselineDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "visual-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, _ := NewArtifactManager(tmpDir, "test")
	dir := BaselineDir(am)
	if dir == "" {
		t.Error("BaselineDir should return non-empty path")
	}
	expected := filepath.Join(filepath.Dir(am.baseDir), "baselines")
	if dir != expected {
		t.Errorf("BaselineDir = %q, want %q", dir, expected)
	}

	// Without artifact manager, uses home dir
	dir2 := BaselineDir(nil)
	if dir2 == "" {
		t.Error("BaselineDir(nil) should return non-empty path")
	}
}

func TestPixelsDiffer(t *testing.T) {
	tests := []struct {
		name string
		a, b color.Color
		want bool
	}{
		{
			name: "identical",
			a:    color.RGBA{R: 128, G: 128, B: 128, A: 255},
			b:    color.RGBA{R: 128, G: 128, B: 128, A: 255},
			want: false,
		},
		{
			name: "within tolerance",
			a:    color.RGBA{R: 128, G: 128, B: 128, A: 255},
			b:    color.RGBA{R: 135, G: 128, B: 128, A: 255},
			want: false,
		},
		{
			name: "beyond tolerance",
			a:    color.RGBA{R: 128, G: 128, B: 128, A: 255},
			b:    color.RGBA{R: 200, G: 128, B: 128, A: 255},
			want: true,
		},
		{
			name: "different alpha",
			a:    color.RGBA{R: 128, G: 128, B: 128, A: 255},
			b:    color.RGBA{R: 128, G: 128, B: 128, A: 0},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pixelsDiffer(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("pixelsDiffer = %v, want %v", got, tt.want)
			}
		})
	}
}
