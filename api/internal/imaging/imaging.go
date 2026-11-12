// Package imaging validates uploaded photographs and renders derivatives
// (orientation-corrected, resized JPEG thumbnails). It never modifies the
// original bytes.
package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"

	_ "image/png" // register decoder

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register decoder
)

// Accepted content types.
const (
	TypeJPEG = "image/jpeg"
	TypePNG  = "image/png"
	TypeWebP = "image/webp"
)

// MaxPixels bounds decoded image size (and therefore memory: ~4 bytes per
// pixel) to guard against decompression bombs. 64 MP covers current phone and
// most camera sensors.
const MaxPixels = 64_000_000

// ValidationError is a user-facing reason an upload was rejected.
type ValidationError struct{ Reason string }

func (e *ValidationError) Error() string { return e.Reason }

func invalid(format string, args ...any) error {
	return &ValidationError{Reason: fmt.Sprintf(format, args...)}
}

// Info describes a validated image.
type Info struct {
	ContentType string
	// Width and Height are the displayed dimensions, after EXIF orientation.
	Width, Height int
	// Orientation is the EXIF orientation (1–8); 1 when absent.
	Orientation int
}

// Validate checks that data is a complete, decodable JPEG, PNG or WebP image
// within the size limits. The content type comes from the bytes, never from
// the client.
func Validate(data []byte) (Info, error) {
	ct := sniff(data)
	if ct == "" {
		return Info{}, invalid("unsupported file type: only JPEG, PNG and WebP photographs are accepted")
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Info{}, invalid("the file is not a readable image (%s)", trimErr(err))
	}
	if formatType(format) != ct {
		return Info{}, invalid("the file contents do not match its image type")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Info{}, invalid("the image has no pixels")
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Info{}, invalid("the image is too large (%d×%d); the limit is %d megapixels", cfg.Width, cfg.Height, MaxPixels/1_000_000)
	}

	// A full decode catches truncated or corrupt data that the header alone
	// does not reveal.
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return Info{}, invalid("the image is damaged or incomplete (%s)", trimErr(err))
	}

	o := 1
	if ct == TypeJPEG {
		o = JPEGOrientation(data)
	}
	w, h := cfg.Width, cfg.Height
	if o >= 5 { // 5–8 swap axes
		w, h = h, w
	}
	return Info{ContentType: ct, Width: w, Height: h, Orientation: o}, nil
}

func sniff(data []byte) string {
	switch ct := http.DetectContentType(data); ct {
	case TypeJPEG, TypePNG, TypeWebP:
		return ct
	}
	return ""
}

func formatType(format string) string {
	switch format {
	case "jpeg":
		return TypeJPEG
	case "png":
		return TypePNG
	case "webp":
		return TypeWebP
	}
	return ""
}

func trimErr(err error) string {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected end of file"
	}
	return err.Error()
}

// Thumbnail decodes an original, applies its EXIF orientation, scales it so
// the width is at most maxWidth (never upscaling), flattens transparency onto
// white and encodes a JPEG.
func Thumbnail(original io.Reader, orientation, maxWidth int, w io.Writer) error {
	src, _, err := image.Decode(original)
	if err != nil {
		return err
	}
	src = Orient(src, orientation)

	b := src.Bounds()
	tw, th := b.Dx(), b.Dy()
	if tw > maxWidth {
		th = max(1, th*maxWidth/tw)
		tw = maxWidth
	}

	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return jpeg.Encode(w, dst, &jpeg.Options{Quality: 85})
}
