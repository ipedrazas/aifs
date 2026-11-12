// Package fake is a deterministic, offline provider for tests, CI and local
// development. It renders a placeholder photograph from its inputs, so the
// whole pipeline can run without network access or cost.
//
// Behaviour can be steered from the prompt (for example via a photoshoot's
// custom instructions):
//
//	[fake:fail]   the job finishes as failed
//	[fake:reject] Submit returns a permanent error
//	[fake:flaky]  the first Submit for each seed returns a transient error
//	[fake:slow]   the job takes ten times longer
package fake

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "image/png" // register decoder

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register decoder

	"github.com/ipedrazas/aifs/api/internal/provider"
)

// Name is the provider identifier.
const Name = "fake"

// CostMicros is the cost the fake reports per job ($0.01).
const CostMicros = 10_000

// Provider is the fake provider. The zero value is not usable; use New.
type Provider struct {
	delay time.Duration
	now   func() time.Time

	mu    sync.Mutex
	jobs  map[string]job
	flaky map[int64]bool // seeds that already failed once
}

type job struct {
	req     provider.Request
	readyAt time.Time
}

// New returns a fake provider whose jobs take delay to complete.
func New(delay time.Duration) *Provider {
	return &Provider{delay: delay, now: time.Now, jobs: map[string]job{}, flaky: map[int64]bool{}}
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return Name }

// Label implements provider.Provider.
func (p *Provider) Label() string { return "Fake (offline test provider)" }

// Capabilities implements provider.Provider.
func (p *Provider) Capabilities() []provider.Capability {
	return []provider.Capability{provider.TryOn, provider.ReferenceGenerate, provider.Edit, provider.GenerateModel}
}

// Model implements provider.Provider.
func (p *Provider) Model(c provider.Capability) string { return "fake-" + string(c) + "-v1" }

// Submit implements provider.Provider.
func (p *Provider) Submit(_ context.Context, req provider.Request) (string, error) {
	if !provider.Supports(p, req.Capability) {
		return "", fmt.Errorf("fake: unsupported capability %q", req.Capability)
	}
	if strings.Contains(req.Prompt, "[fake:reject]") {
		return "", errors.New("fake: the request was rejected (simulated)")
	}
	if req.Capability == provider.TryOn && (req.Source == nil || len(req.Garments) == 0) {
		return "", errors.New("fake: try-on needs a person image and a garment image")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.Contains(req.Prompt, "[fake:flaky]") && !p.flaky[req.Seed] {
		p.flaky[req.Seed] = true
		return "", provider.Transient(errors.New("fake: temporary outage (simulated)"), 10*time.Millisecond)
	}

	delay := p.delay
	if strings.Contains(req.Prompt, "[fake:slow]") {
		delay *= 10
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	jobID := "fake-" + hex.EncodeToString(id)
	p.jobs[jobID] = job{req: req, readyAt: p.now().Add(delay)}
	return jobID, nil
}

// Poll implements provider.Provider.
func (p *Provider) Poll(_ context.Context, jobID string) (provider.Result, error) {
	p.mu.Lock()
	j, ok := p.jobs[jobID]
	p.mu.Unlock()
	if !ok {
		return provider.Result{}, provider.ErrUnknownJob
	}
	if p.now().Before(j.readyAt) {
		return provider.Result{Status: provider.Pending}, nil
	}

	p.mu.Lock()
	delete(p.jobs, jobID)
	p.mu.Unlock()

	if strings.Contains(j.req.Prompt, "[fake:fail]") {
		return provider.Result{Status: provider.Failed, Error: "fake: the provider could not produce an image (simulated)"}, nil
	}
	img, renderErr := Render(j.req)
	if renderErr != nil {
		// A bad input is a failed job, not a failure to poll.
		return provider.Result{Status: provider.Failed, Error: "fake: " + renderErr.Error()}, nil //nolint:nilerr // reported in the result
	}
	return provider.Result{
		Status:       provider.Succeeded,
		Images:       []provider.Output{{ContentType: "image/jpeg", Data: img}},
		CostMicros:   CostMicros,
		CostReported: true,
	}, nil
}

// Render draws a deterministic placeholder: a background tinted by the seed,
// the person (source or first model reference) filling the frame and the
// first garment inset in the lower right corner.
func Render(req provider.Request) ([]byte, error) {
	w, h := canvasSize(req.AspectRatio)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	sum := sha256.Sum256(binary.BigEndian.AppendUint64([]byte(req.Capability), uint64(req.Seed))) //nolint:gosec // bit pattern only
	bg := color.RGBA{R: 160 + sum[0]%80, G: 160 + sum[1]%80, B: 160 + sum[2]%80, A: 255}
	draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	person := req.Source
	if person == nil && len(req.ModelRefs) > 0 {
		person = &req.ModelRefs[0]
	}
	if person != nil {
		if err := paste(dst, person.Data, image.Rect(w/10, h/20, w*9/10, h*19/20)); err != nil {
			return nil, fmt.Errorf("person image: %w", err)
		}
	}
	if len(req.Garments) > 0 {
		if err := paste(dst, req.Garments[0].Data, image.Rect(w*3/5, h*3/5, w*19/20, h*19/20)); err != nil {
			return nil, fmt.Errorf("garment image: %w", err)
		}
	}
	// A band whose colour also depends on the seed, so variations differ.
	band := color.RGBA{R: sum[3], G: sum[4], B: sum[5], A: 255}
	draw.Draw(dst, image.Rect(0, 0, w, h/40), image.NewUniform(band), image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paste scales src to fit inside r, preserving aspect ratio.
func paste(dst draw.Image, data []byte, r image.Rectangle) error {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	sb := src.Bounds()
	scale := min(float64(r.Dx())/float64(sb.Dx()), float64(r.Dy())/float64(sb.Dy()))
	tw, th := max(1, int(float64(sb.Dx())*scale)), max(1, int(float64(sb.Dy())*scale))
	x := r.Min.X + (r.Dx()-tw)/2
	y := r.Min.Y + (r.Dy()-th)/2
	xdraw.ApproxBiLinear.Scale(dst, image.Rect(x, y, x+tw, y+th), src, sb, draw.Over, nil)
	return nil
}

// canvasSize returns dimensions with a 1024 px long edge for "W:H".
func canvasSize(ratio string) (int, int) {
	a, b, ok := strings.Cut(ratio, ":")
	rw, err1 := strconv.Atoi(a)
	rh, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil || rw <= 0 || rh <= 0 {
		rw, rh = 3, 4
	}
	if rw >= rh {
		return 1024, max(1, 1024*rh/rw)
	}
	return max(1, 1024*rw/rh), 1024
}
