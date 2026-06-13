package images

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math/rand"
	"testing"
)

// noiseImage builds an image filled with deterministic per-pixel noise so
// the PNG encoding stays large (noise defeats compression).
func noiseImage(t *testing.T, width, height int, alpha uint8) *image.NRGBA {
	t.Helper()
	rnd := rand.New(rand.NewSource(42))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(rnd.Intn(256)),
				G: uint8(rnd.Intn(256)),
				B: uint8(rnd.Intn(256)),
				A: alpha,
			})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode png: %v", err)
	}
	return buf.Bytes()
}

func TestGenerateThumbnailDownscales(t *testing.T) {
	data := encodePNG(t, noiseImage(t, 1500, 1000, 255))
	if len(data) <= ThumbnailThreshold {
		t.Fatalf("test image too small: %d bytes", len(data))
	}

	thumb, mime, err := GenerateThumbnail(data, "image/png")
	if err != nil {
		t.Fatalf("GenerateThumbnail failed: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}
	if len(thumb) > targetThumbBytes {
		t.Errorf("thumbnail too big: %d bytes", len(thumb))
	}

	decoded, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != MaxThumbDimension {
		t.Errorf("expected width %d, got %d", MaxThumbDimension, bounds.Dx())
	}
	if want := 1000 * MaxThumbDimension / 1500; bounds.Dy() != want {
		t.Errorf("expected height %d, got %d", want, bounds.Dy())
	}
}

func TestGenerateThumbnailNoUpscale(t *testing.T) {
	data := encodePNG(t, noiseImage(t, 400, 400, 255))
	if len(data) <= ThumbnailThreshold {
		t.Fatalf("test image too small: %d bytes", len(data))
	}

	thumb, _, err := GenerateThumbnail(data, "image/png")
	if err != nil {
		t.Fatalf("GenerateThumbnail failed: %v", err)
	}
	if len(thumb) >= len(data) {
		t.Errorf("expected thumbnail smaller than original: %d >= %d", len(thumb), len(data))
	}

	decoded, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	if got := decoded.Bounds(); got.Dx() != 400 || got.Dy() != 400 {
		t.Errorf("expected 400x400, got %dx%d", got.Dx(), got.Dy())
	}
}

func TestGenerateThumbnailTransparency(t *testing.T) {
	data := encodePNG(t, noiseImage(t, 700, 700, 0))

	thumb, mime, err := GenerateThumbnail(data, "image/png")
	if err != nil {
		t.Fatalf("GenerateThumbnail failed: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}

	decoded, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	// Fully transparent source must composite onto white.
	r, g, b, a := decoded.At(10, 10).RGBA()
	if r != 0xffff || g != 0xffff || b != 0xffff || a != 0xffff {
		t.Errorf("expected white opaque pixel, got r=%d g=%d b=%d a=%d", r, g, b, a)
	}
}

func TestGenerateThumbnailGIF(t *testing.T) {
	var buf bytes.Buffer
	if err := gif.Encode(&buf, noiseImage(t, 800, 600, 255), nil); err != nil {
		t.Fatalf("failed to encode gif: %v", err)
	}

	thumb, mime, err := GenerateThumbnail(buf.Bytes(), "image/gif")
	if err != nil {
		t.Fatalf("GenerateThumbnail failed: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}
	decoded, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	if got := decoded.Bounds(); got.Dx() != MaxThumbDimension {
		t.Errorf("expected width %d, got %d", MaxThumbDimension, got.Dx())
	}
}

func TestGenerateThumbnailUnsupported(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
	}{
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "image/svg+xml"},
		{"garbage", []byte("definitely not an image"), "image/webp"},
		{"empty", nil, "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := GenerateThumbnail(tc.data, tc.mime); err != ErrUnsupported {
				t.Errorf("expected ErrUnsupported, got %v", err)
			}
		})
	}
}

func makeLargeWebP(t *testing.T) []byte {
	t.Helper()
	b64 := "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}
	return append(data, make([]byte, 105*1024)...)
}

func TestGenerateThumbnailWebP(t *testing.T) {
	data := makeLargeWebP(t)

	if len(data) <= ThumbnailThreshold {
		t.Fatalf("test image too small: %d bytes", len(data))
	}

	thumb, mime, err := GenerateThumbnail(data, "image/webp")
	if err != nil {
		t.Fatalf("GenerateThumbnail failed: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}
	decoded, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	if got := decoded.Bounds(); got.Dx() != 1 || got.Dy() != 1 {
		t.Errorf("expected 1x1 thumbnail from 1x1 dummy webp, got %dx%d", got.Dx(), got.Dy())
	}
}
