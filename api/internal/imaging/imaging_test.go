package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidateAccepts(t *testing.T) {
	for _, c := range []struct {
		file, ct     string
		w, h, orient int
	}{
		{"plain.jpg", TypeJPEG, 40, 20, 1},
		{"rot6.jpg", TypeJPEG, 20, 40, 6},
		{"plain.png", TypePNG, 40, 20, 1},
		{"plain.webp", TypeWebP, 40, 20, 1},
		{"lossless.webp", TypeWebP, 40, 20, 1},
	} {
		t.Run(c.file, func(t *testing.T) {
			info, err := Validate(fixture(t, c.file))
			if err != nil {
				t.Fatal(err)
			}
			want := Info{ContentType: c.ct, Width: c.w, Height: c.h, Orientation: c.orient}
			if info != want {
				t.Fatalf("got %+v, want %+v", info, want)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		msg  string
	}{
		{"gif", fixture(t, "plain.gif"), "unsupported file type"},
		{"text named .jpg", fixture(t, "text.jpg"), "unsupported file type"},
		{"truncated jpeg", fixture(t, "truncated.jpg"), "damaged or incomplete"},
		{"empty", nil, "unsupported file type"},
		{"jpeg header only", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "not a readable image"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Validate(c.data)
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(ve.Reason, c.msg) {
				t.Fatalf("got %v, want a validation error containing %q", err, c.msg)
			}
		})
	}
}

func TestValidateRejectsHugeDimensions(t *testing.T) {
	// A tiny PNG header that claims 20000×20000 pixels: rejected before any
	// pixel memory is allocated.
	png := fixture(t, "plain.png")
	huge := bytes.Clone(png)
	// IHDR width/height live at byte offsets 16 and 20.
	copy(huge[16:20], []byte{0, 0, 0x4E, 0x20})
	copy(huge[20:24], []byte{0, 0, 0x4E, 0x20})
	// Recompute the IHDR CRC (over chunk type + data) so only the size is wrong.
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	_, err := Validate(huge)
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Reason, "too large") {
		t.Fatalf("got %v", err)
	}
}

func TestJPEGOrientationGarbage(t *testing.T) {
	for _, b := range [][]byte{
		nil,
		{0xFF, 0xD8},
		{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF, 'E', 'x'},
		append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10}, []byte("Exif\x00\x00II*\x00\xFF\xFF\xFF\xFF")...),
	} {
		if o := JPEGOrientation(b); o != 1 {
			t.Errorf("%x: got %d", b, o)
		}
	}
}

// The fixtures are 40×20 with the left half red and the right half blue.
func TestThumbnailAppliesOrientation(t *testing.T) {
	var out bytes.Buffer
	if err := Thumbnail(bytes.NewReader(fixture(t, "rot6.jpg")), 6, 1000, &out); err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 40 {
		t.Fatalf("bounds %v, want 20×40", b)
	}
	// Rotated 90° clockwise: red (originally left) ends up on top.
	if !reddish(img.At(10, 5)) || reddish(img.At(10, 35)) {
		t.Fatal("orientation not applied")
	}
}

func TestThumbnailScalesDownOnly(t *testing.T) {
	for _, c := range []struct{ maxW, w, h int }{{10, 10, 5}, {1000, 40, 20}} {
		var out bytes.Buffer
		if err := Thumbnail(bytes.NewReader(fixture(t, "plain.png")), 1, c.maxW, &out); err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(&out)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width != c.w || cfg.Height != c.h {
			t.Errorf("max %d: got %d×%d", c.maxW, cfg.Width, cfg.Height)
		}
	}
}

func TestOrientAll(t *testing.T) {
	// 2×1 image: (0,0) red, (1,0) blue.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	src.Set(1, 0, color.RGBA{0, 0, 255, 255})

	// Where the red pixel ends up for each orientation.
	want := map[int]image.Point{1: {0, 0}, 2: {1, 0}, 3: {1, 0}, 4: {0, 0}, 5: {0, 0}, 6: {0, 0}, 7: {0, 1}, 8: {0, 1}}
	for o, p := range want {
		got := Orient(src, o)
		if !reddish(got.At(p.X, p.Y)) {
			t.Errorf("orientation %d: red not at %v (bounds %v)", o, p, got.Bounds())
		}
	}
}

func reddish(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0xA000 && g < 0x6000 && b < 0x6000
}
