//go:build live

// Live check against the real Replicate API. It costs money (about $0.0005),
// so it only runs with the "live" build tag and a token:
//
//	AIFS_REPLICATE_API_TOKEN=... go test -tags live -run TestLive -v ./internal/provider/replicate/
//
// Optional: REPLICATE_PHOTO points at your own photograph (JPEG or PNG; it
// is sent as a data URI, so keep it under a few MB), and the mask is written
// to REPLICATE_OUT (default: a temp file).
package replicate

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

// garment draws a striped block on a light background.
func garment() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 600, 800))
	for y := range 800 {
		for x := range 600 {
			c := color.RGBA{225, 220, 210, 255}
			if x > 170 && x < 430 && y > 120 && y < 720 {
				c = color.RGBA{40, 70, 140, 255}
				if (x/20+y/20)%2 == 0 {
					c = color.RGBA{200, 60, 50, 255}
				}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func TestLiveRemoveBackground(t *testing.T) {
	token := os.Getenv("AIFS_REPLICATE_API_TOKEN")
	if token == "" {
		token = os.Getenv("REPLICATE_API_TOKEN")
	}
	if token == "" {
		t.Skip("AIFS_REPLICATE_API_TOKEN not set")
	}
	data := garment()
	if path := os.Getenv("REPLICATE_PHOTO"); path != "" {
		var err error
		if data, err = os.ReadFile(path); err != nil { //nolint:gosec // test input
			t.Fatal(err)
		}
	}
	p, err := New(Config{APIToken: token})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	id, err := p.Submit(ctx, provider.Request{
		Capability: provider.RemoveBackground,
		Source:     &provider.Image{ContentType: http.DetectContentType(data), Data: data},
	})
	if err != nil {
		t.Fatal(err)
	}
	var res provider.Result
	for {
		res, err = p.Poll(ctx, id)
		if err != nil {
			if ok, _ := provider.IsTransient(err); !ok {
				t.Fatal(err)
			}
		} else if res.Status != provider.Pending {
			break
		}
		time.Sleep(time.Second)
	}
	if res.Status != provider.Succeeded {
		t.Fatalf("result: %s", res.Error)
	}
	mask, _, err := image.Decode(bytes.NewReader(res.Images[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	src, _, _ := image.DecodeConfig(bytes.NewReader(data))
	if mask.Bounds().Dx() != src.Width || mask.Bounds().Dy() != src.Height {
		t.Fatalf("mask %v, photo %dx%d", mask.Bounds(), src.Width, src.Height)
	}
	out := os.Getenv("REPLICATE_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "mask.png")
	}
	if err := os.WriteFile(out, res.Images[0].Data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("job %s: mask %s (%v)", id, out, mask.Bounds())
}
