// Package images provides server-side thumbnail generation for uploaded images.
package images

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"

	// Register stdlib decoders for image.Decode.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

const (
	// ThumbnailThreshold is the original file size above which a thumbnail is generated.
	ThumbnailThreshold = 100 * 1024
	// MaxThumbDimension is the maximum thumbnail dimension in pixels:
	// 2x the 300px .message-attachment box for retina displays.
	MaxThumbDimension = 600
	// targetThumbBytes is the upper bound of the 50-100KB thumbnail size target.
	targetThumbBytes = 100 * 1024
)

// ErrUnsupported is returned for images that cannot or should not be
// thumbnailed (SVG, formats the stdlib cannot decode, corrupt data).
var ErrUnsupported = errors.New("image format not supported for thumbnailing")

// jpegQualities are tried in order until the encoded thumbnail fits targetThumbBytes.
var jpegQualities = []int{85, 75, 65, 55, 50}

// GenerateThumbnail decodes data, downscales the longest side to at most
// MaxThumbDimension (never upscales) and re-encodes as JPEG, stepping quality
// down until the result fits targetThumbBytes. Transparency is composited
// onto white. Returns ErrUnsupported when no thumbnail can be produced.
func GenerateThumbnail(data []byte, mimeType string) (thumb []byte, thumbMime string, err error) {
	if mimeType == "image/svg+xml" {
		return nil, "", ErrUnsupported
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrUnsupported
	}

	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, "", ErrUnsupported
	}

	thumbWidth, thumbHeight := width, height
	if width > MaxThumbDimension || height > MaxThumbDimension {
		if width >= height {
			thumbWidth = MaxThumbDimension
			thumbHeight = max(height*MaxThumbDimension/width, 1)
		} else {
			thumbHeight = MaxThumbDimension
			thumbWidth = max(width*MaxThumbDimension/height, 1)
		}
	}

	// Composite onto white so transparent areas do not turn black in JPEG.
	canvas := image.NewRGBA(image.Rect(0, 0, thumbWidth, thumbHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), src, bounds, draw.Over, nil)

	var buf bytes.Buffer
	for _, quality := range jpegQualities {
		buf.Reset()
		if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		if buf.Len() <= targetThumbBytes {
			break
		}
	}
	// If even the lowest quality exceeds the target, keep it anyway:
	// a slightly oversized thumbnail beats no thumbnail.

	return buf.Bytes(), "image/jpeg", nil
}
