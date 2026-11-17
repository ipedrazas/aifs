package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// garment is a 100×80 opaque image: a red 40×30 block at (30,20) on grey.
func garment() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	for y := range 80 {
		for x := range 100 {
			c := color.NRGBA{200, 200, 200, 255}
			if x >= 30 && x < 70 && y >= 20 && y < 50 {
				c = color.NRGBA{uint8(150 + x), 20, uint8(y), 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// maskOf is white over the red block, black elsewhere, at scale s.
func maskOf(s int) *image.Gray {
	m := image.NewGray(image.Rect(0, 0, 100*s, 80*s))
	for y := 20 * s; y < 50*s; y++ {
		for x := 30 * s; x < 70*s; x++ {
			m.SetGray(x, y, color.Gray{255})
		}
	}
	return m
}

func TestApplyMaskKeepsOriginalPixels(t *testing.T) {
	src := garment()
	for _, s := range []int{1, 2} { // same size, and a mask that must be scaled
		out := ApplyMask(src, maskOf(s))
		if out.NRGBAAt(5, 5).A != 0 {
			t.Fatalf("scale %d: background kept: %v", s, out.NRGBAAt(5, 5))
		}
		for _, p := range []image.Point{{35, 25}, {50, 40}, {65, 45}} {
			got, want := out.NRGBAAt(p.X, p.Y), src.NRGBAAt(p.X, p.Y)
			if got != want {
				t.Fatalf("scale %d at %v: %v, want the original %v", s, p, got, want)
			}
		}
	}
	if src.NRGBAAt(5, 5).A != 255 {
		t.Fatal("the source was modified")
	}
}

func TestTrim(t *testing.T) {
	cut := ApplyMask(garment(), maskOf(1))
	out, ok := Trim(cut, 0)
	if !ok || out.Bounds().Dx() != 40 || out.Bounds().Dy() != 30 {
		t.Fatalf("trim: %v %v", out.Bounds(), ok)
	}
	if out.NRGBAAt(0, 0) != cut.NRGBAAt(30, 20) {
		t.Fatal("trim moved pixels")
	}
	// 10% of the longer side (40) = 4 px each side.
	if out, _ := Trim(cut, 10); out.Bounds().Dx() != 48 || out.Bounds().Dy() != 38 {
		t.Fatalf("margin: %v", out.Bounds())
	}
	// A huge margin is clamped to the image.
	if out, _ := Trim(cut, 500); out.Bounds() != cut.Bounds() {
		t.Fatalf("clamp: %v", out.Bounds())
	}
	if _, ok := Trim(garment(), 0); ok {
		t.Fatal("an opaque image cannot be trimmed")
	}
}

func TestFit(t *testing.T) {
	if out := Fit(garment(), 50); out.Bounds().Dx() != 50 || out.Bounds().Dy() != 40 {
		t.Fatalf("landscape: %v", out.Bounds())
	}
	tall := image.NewNRGBA(image.Rect(0, 0, 30, 90))
	if out := Fit(tall, 60); out.Bounds().Dx() != 20 || out.Bounds().Dy() != 60 {
		t.Fatalf("portrait: %v", out.Bounds())
	}
	g := garment()
	if Fit(g, 1000) != g || Fit(g, 0) != g {
		t.Fatal("never upscales")
	}
}

func TestEncodeChoosesFormat(t *testing.T) {
	if _, ct, _ := Encode(garment(), false); ct != TypeJPEG {
		t.Fatalf("opaque: %s", ct)
	}
	if _, ct, _ := Encode(garment(), true); ct != TypePNG {
		t.Fatalf("lossless: %s", ct)
	}
	data, ct, err := Encode(ApplyMask(garment(), maskOf(1)), false)
	if err != nil || ct != TypePNG {
		t.Fatalf("cut-out: %s %v", ct, err)
	}
	if _, err := Validate(data); err != nil {
		t.Fatal(err)
	}
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestForProvider(t *testing.T) {
	limits := ProviderLimits{MaxEdge: 64, MaxBytes: 1 << 20}

	small := fixture(t, "plain.jpg")
	if out, ct, err := ForProvider(small, limits); err != nil || ct != TypeJPEG || !bytes.Equal(out, small) {
		t.Fatalf("small JPEG must pass unchanged: %s %v", ct, err)
	}
	if out, ct, _ := ForProvider(fixture(t, "plain.png"), limits); ct != TypePNG || !bytes.Equal(out, fixture(t, "plain.png")) {
		t.Fatal("small opaque PNG must pass unchanged")
	}

	check := func(name string, data []byte, l ProviderLimits, w, h int) {
		t.Helper()
		out, ct, err := ForProvider(data, l)
		if err != nil || ct != TypeJPEG {
			t.Fatalf("%s: %s %v", name, ct, err)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
		if err != nil || cfg.Width != w || cfg.Height != h {
			t.Fatalf("%s: %dx%d %v", name, cfg.Width, cfg.Height, err)
		}
	}
	check("large", encodeJPEG(t, garment()), limits, 64, 51)
	check("rotated", fixture(t, "rot6.jpg"), limits, 20, 40)
	check("over the byte limit", fixture(t, "plain.png"), ProviderLimits{MaxEdge: 64, MaxBytes: 10}, 40, 20)

	var cut bytes.Buffer
	_ = png.Encode(&cut, ApplyMask(garment(), maskOf(1)))
	out, _, _ := ForProvider(cut.Bytes(), ProviderLimits{MaxEdge: 1000, MaxBytes: 1 << 20})
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := img.At(5, 5).RGBA(); r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Fatalf("transparency must become white: %v", img.At(5, 5))
	}
}
