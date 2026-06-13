package images

import (
	"encoding/binary"
	"image"
)

// orientationNormal is EXIF orientation 1: no rotation or flip needed.
const orientationNormal = 1

// readOrientation returns the EXIF orientation (1-8) embedded in a JPEG's
// APP1 segment, or orientationNormal when the data is not a JPEG, carries no
// EXIF, or the tag is absent or malformed. Go's image/jpeg decoder ignores
// this tag, so thumbnails must apply the rotation explicitly or portrait
// photos come out sideways.
func readOrientation(data []byte) int {
	// Only JPEG carries the EXIF APP1 segment parsed here. WebP can also embed
	// EXIF, but in a different container we do not handle.
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return orientationNormal
	}

	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return orientationNormal
		}
		marker := data[i+1]

		// Standalone markers carry no length payload.
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		// Start of scan: compressed image data follows, no more metadata.
		if marker == 0xDA {
			return orientationNormal
		}

		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 || i+2+segLen > len(data) {
			return orientationNormal
		}

		if marker == 0xE1 { // APP1 may hold EXIF
			if o, ok := parseEXIFOrientation(data[i+4 : i+2+segLen]); ok {
				return o
			}
		}

		i += 2 + segLen
	}
	return orientationNormal
}

// parseEXIFOrientation extracts the Orientation tag (0x0112) from an APP1
// segment payload (the bytes following the marker and length field). The
// orientation value is a SHORT stored inline, so no offset following is needed.
func parseEXIFOrientation(seg []byte) (int, bool) {
	const exifHeaderLen = 6 // "Exif\0\0"
	if len(seg) < exifHeaderLen+8 || string(seg[:4]) != "Exif" {
		return 0, false
	}
	tiff := seg[exifHeaderLen:]

	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, false
	}
	if bo.Uint16(tiff[2:4]) != 0x002A {
		return 0, false
	}

	ifdOffset := int(bo.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return 0, false
	}

	count := int(bo.Uint16(tiff[ifdOffset : ifdOffset+2]))
	const entrySize = 12
	for entry := ifdOffset + 2; count > 0; count, entry = count-1, entry+entrySize {
		if entry+entrySize > len(tiff) {
			break
		}
		if bo.Uint16(tiff[entry:entry+2]) != 0x0112 { // Orientation tag
			continue
		}
		value := int(bo.Uint16(tiff[entry+8 : entry+10]))
		if value >= 1 && value <= 8 {
			return value, true
		}
		return 0, false
	}
	return 0, false
}

// applyOrientation returns img transformed for display per the given EXIF
// orientation (1-8). Orientation 1, or any out-of-range value, returns img
// unchanged. Orientations 5-8 transpose the axes, swapping width and height.
func applyOrientation(img image.Image, orientation int) image.Image {
	if orientation <= orientationNormal || orientation > 8 {
		return img
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	dstW, dstH := w, h
	if orientation >= 5 {
		dstW, dstH = h, w
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2: // mirror horizontal
				dx, dy = w-1-x, y
			case 3: // rotate 180
				dx, dy = w-1-x, h-1-y
			case 4: // mirror vertical
				dx, dy = x, h-1-y
			case 5: // transpose (mirror over main diagonal)
				dx, dy = y, x
			case 6: // rotate 90 clockwise
				dx, dy = h-1-y, x
			case 7: // transverse (mirror over anti-diagonal)
				dx, dy = h-1-y, w-1-x
			case 8: // rotate 90 counter-clockwise
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
