package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// jpegWithOrientation encodes img as JPEG and injects an EXIF APP1 segment
// carrying the given orientation tag, mirroring what a phone camera writes.
func jpegWithOrientation(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("failed to encode jpeg: %v", err)
	}
	raw := buf.Bytes()

	// Little-endian TIFF with a single Orientation (0x0112) SHORT entry.
	exif := []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // header, IFD0 at offset 8
		0x01, 0x00, // 1 entry
		0x12, 0x01, // tag 0x0112 (Orientation)
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		byte(orientation), 0x00, 0x00, 0x00, // value (inline)
		0x00, 0x00, 0x00, 0x00, // next IFD offset
	}
	segLen := len(exif) + 2
	app1 := append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}, exif...)

	out := make([]byte, 0, len(raw)+len(app1))
	out = append(out, raw[:2]...) // SOI
	out = append(out, app1...)
	out = append(out, raw[2:]...)
	return out
}

func TestReadOrientation(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))

	for orientation := 1; orientation <= 8; orientation++ {
		data := jpegWithOrientation(t, img, orientation)
		if got := readOrientation(data); got != orientation {
			t.Errorf("orientation %d: got %d", orientation, got)
		}
	}

	// A plain JPEG without EXIF, and a non-JPEG, default to normal.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("failed to encode jpeg: %v", err)
	}
	if got := readOrientation(buf.Bytes()); got != orientationNormal {
		t.Errorf("plain jpeg: expected %d, got %d", orientationNormal, got)
	}
	if got := readOrientation([]byte("not an image")); got != orientationNormal {
		t.Errorf("non-jpeg: expected %d, got %d", orientationNormal, got)
	}
}

// TestParseEXIFOrientationRealistic covers EXIF layouts a synthetic
// single-entry IFD misses: big-endian byte order and an Orientation tag that
// is not the first IFD entry.
func TestParseEXIFOrientationRealistic(t *testing.T) {
	// Big-endian ("MM") TIFF, IFD0 at offset 8, three entries with Orientation
	// (0x0112 = 6) sandwiched between ImageWidth (0x0100) and ImageLength (0x0101).
	seg := []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08, // header, IFD0 at offset 8
		0x00, 0x03, // 3 entries
		0x01, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, 0x10, 0x00, 0x00, // ImageWidth
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, 0x06, 0x00, 0x00, // Orientation = 6
		0x01, 0x01, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, 0x08, 0x00, 0x00, // ImageLength
		0x00, 0x00, 0x00, 0x00, // next IFD offset
	}
	got, ok := parseEXIFOrientation(seg)
	if !ok || got != 6 {
		t.Errorf("expected orientation 6, got %d (ok=%v)", got, ok)
	}
}

func TestApplyOrientation(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}

	// A 2x1 image: red on the left, blue on the right.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, red)
	src.Set(1, 0, blue)

	rgba := func(c color.Color) color.RGBA {
		r, g, b, a := c.RGBA()
		return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	}

	t.Run("normal is unchanged", func(t *testing.T) {
		if got := applyOrientation(src, 1); got != image.Image(src) {
			t.Errorf("expected the same image instance for orientation 1")
		}
	})

	t.Run("mirror horizontal", func(t *testing.T) {
		got := applyOrientation(src, 2)
		if b := got.Bounds(); b.Dx() != 2 || b.Dy() != 1 {
			t.Fatalf("expected 2x1, got %dx%d", b.Dx(), b.Dy())
		}
		if rgba(got.At(0, 0)) != blue || rgba(got.At(1, 0)) != red {
			t.Errorf("mirror horizontal did not swap pixels")
		}
	})

	t.Run("rotate 90 CW swaps dimensions", func(t *testing.T) {
		got := applyOrientation(src, 6)
		if b := got.Bounds(); b.Dx() != 1 || b.Dy() != 2 {
			t.Fatalf("expected 1x2, got %dx%d", b.Dx(), b.Dy())
		}
		// Left pixel (red) rotates to the top, right pixel (blue) to the bottom.
		if rgba(got.At(0, 0)) != red || rgba(got.At(0, 1)) != blue {
			t.Errorf("rotate 90 CW placed pixels incorrectly")
		}
	})

	t.Run("rotate 90 CCW swaps dimensions", func(t *testing.T) {
		got := applyOrientation(src, 8)
		if b := got.Bounds(); b.Dx() != 1 || b.Dy() != 2 {
			t.Fatalf("expected 1x2, got %dx%d", b.Dx(), b.Dy())
		}
		// Left pixel (red) rotates to the bottom, right pixel (blue) to the top.
		if rgba(got.At(0, 0)) != blue || rgba(got.At(0, 1)) != red {
			t.Errorf("rotate 90 CCW placed pixels incorrectly")
		}
	})
}

func TestGenerateThumbnailHonorsOrientation(t *testing.T) {
	// Landscape source (wider than tall) that a portrait photo would store
	// with orientation 6 (rotate 90 CW for display).
	landscape := noiseImage(t, 800, 400, 255)

	upright := jpegWithOrientation(t, landscape, 1)
	rotated := jpegWithOrientation(t, landscape, 6)

	uprightThumb, _, err := GenerateThumbnail(upright, "image/jpeg")
	if err != nil {
		t.Fatalf("GenerateThumbnail (upright) failed: %v", err)
	}
	rotatedThumb, _, err := GenerateThumbnail(rotated, "image/jpeg")
	if err != nil {
		t.Fatalf("GenerateThumbnail (rotated) failed: %v", err)
	}

	uprightDecoded, _, err := image.Decode(bytes.NewReader(uprightThumb))
	if err != nil {
		t.Fatalf("failed to decode upright thumbnail: %v", err)
	}
	rotatedDecoded, _, err := image.Decode(bytes.NewReader(rotatedThumb))
	if err != nil {
		t.Fatalf("failed to decode rotated thumbnail: %v", err)
	}

	// The upright thumbnail stays landscape; the oriented one becomes portrait.
	if ub := uprightDecoded.Bounds(); ub.Dx() <= ub.Dy() {
		t.Errorf("expected landscape upright thumbnail, got %dx%d", ub.Dx(), ub.Dy())
	}
	if rb := rotatedDecoded.Bounds(); rb.Dx() >= rb.Dy() {
		t.Errorf("expected portrait rotated thumbnail, got %dx%d", rb.Dx(), rb.Dy())
	}
}
