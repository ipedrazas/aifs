package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// Decode decodes validated image bytes and turns them upright.
func Decode(data []byte) (*image.NRGBA, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalid("the image cannot be read (%s)", trimErr(err))
	}
	o := 1
	if sniff(data) == TypeJPEG {
		o = JPEGOrientation(data)
	}
	return toNRGBA(Orient(src, o)), nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// HasTransparency reports whether any pixel is not fully opaque.
func HasTransparency(img *image.NRGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return true
		}
	}
	return false
}

// ApplyMask returns a copy of img whose alpha is the mask's brightness
// (white keeps a pixel, black removes it), multiplied with any alpha img
// already has. The mask is scaled to img's size. Colour values are copied
// unchanged, so every kept pixel is the original pixel.
func ApplyMask(img *image.NRGBA, mask image.Image) *image.NRGBA {
	b := img.Bounds()
	m := image.NewGray(b)
	mb := mask.Bounds()
	if mb.Dx() == b.Dx() && mb.Dy() == b.Dy() {
		draw.Draw(m, b, mask, mb.Min, draw.Src)
	} else {
		xdraw.BiLinear.Scale(m, b, mask, mb, draw.Src, nil)
	}
	out := image.NewNRGBA(b)
	copy(out.Pix, img.Pix)
	for y := range b.Dy() {
		for x := range b.Dx() {
			i := out.PixOffset(x, y)
			a := uint32(out.Pix[i+3]) * uint32(m.Pix[m.PixOffset(x, y)]) / 0xff
			out.Pix[i+3] = uint8(a) //nolint:gosec // a ≤ 255
		}
	}
	return out
}

// trimAlpha is the alpha at or below which a pixel counts as background
// when trimming, so faint mask noise does not stop the crop.
const trimAlpha = 16

// Trim crops img to the bounding box of its visible pixels, plus a margin
// of marginPct per cent of the box's longer side on every edge (clamped to
// the image). It reports false when the image has no transparency to trim
// by, or nothing visible.
func Trim(img *image.NRGBA, marginPct float64) (*image.NRGBA, bool) {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Dx(), b.Dy(), -1, -1
	for y := range b.Dy() {
		for x := range b.Dx() {
			if img.Pix[img.PixOffset(x, y)+3] > trimAlpha {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < 0 || !HasTransparency(img) {
		return img, false
	}
	m := int(float64(max(maxX-minX+1, maxY-minY+1)) * marginPct / 100)
	r := image.Rect(minX-m, minY-m, maxX+1+m, maxY+1+m).Intersect(b)
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out, true
}

// Fit scales img down so its long edge is at most maxEdge. It never
// upscales; maxEdge ≤ 0 leaves the image as it is.
func Fit(img *image.NRGBA, maxEdge int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxEdge <= 0 || max(w, h) <= maxEdge {
		return img
	}
	if w >= h {
		h, w = max(1, h*maxEdge/w), maxEdge
	} else {
		w, h = max(1, w*maxEdge/h), maxEdge
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

// Flatten draws img onto an opaque background colour.
func Flatten(img *image.NRGBA, bg color.Color) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

// Encode writes img as PNG when it has transparency or lossless is set
// (keeping every pixel exactly), and as a high-quality JPEG otherwise.
func Encode(img *image.NRGBA, lossless bool) ([]byte, string, error) {
	var buf bytes.Buffer
	if lossless || HasTransparency(img) {
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), TypePNG, nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), TypeJPEG, nil
}

// ProviderLimits bound what is sent to an AI provider.
type ProviderLimits struct {
	// MaxEdge is the longest side in pixels.
	MaxEdge int
	// MaxBytes is the largest file sent as it is.
	MaxBytes int
}

// ForProvider returns the bytes to send to a provider for a stored
// photograph. A JPEG or PNG that is upright, opaque and within the limits
// is sent unchanged. Anything else is turned upright, flattened onto white,
// scaled to fit and encoded as a JPEG at quality 92. It is deterministic,
// so the stored hash still identifies what was sent.
func ForProvider(data []byte, l ProviderLimits) ([]byte, string, error) {
	ct := sniff(data)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", invalid("the image cannot be read (%s)", trimErr(err))
	}
	upright := ct != TypeJPEG || JPEGOrientation(data) <= 1
	small := max(cfg.Width, cfg.Height) <= l.MaxEdge && len(data) <= l.MaxBytes
	if ct == TypeJPEG && upright && small {
		return data, ct, nil
	}
	img, err := Decode(data)
	if err != nil {
		return nil, "", err
	}
	if ct == TypePNG && small && !HasTransparency(img) {
		return data, ct, nil
	}
	var buf bytes.Buffer
	flat := Flatten(Fit(img, l.MaxEdge), color.White)
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 92}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), TypeJPEG, nil
}
