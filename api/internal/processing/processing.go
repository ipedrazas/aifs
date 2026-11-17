// Package processing makes processed versions of product photographs:
// background removed, trimmed to the garment and resized. Only deterministic
// operations touch the pixels. Background removal asks a provider for a
// mask and applies it to the original pixels, so no garment pixel is ever
// redrawn. The original photograph is never changed.
package processing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"time"

	_ "image/png" // mask decoder

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/imaging"
	"github.com/ipedrazas/aifs/api/internal/provider"
)

// Limits on the resize step.
const (
	MinEdge = 256
	MaxEdge = 8192
)

// DefaultTrimMarginPct is the margin kept around the garment when trimming.
const DefaultTrimMarginPct = 3.0

// maskInputEdge is the long edge of the copy sent for background removal.
// The mask is scaled back up to the original's size; a smaller upload keeps
// the request fast and well within the provider's data-URI limits.
const maskInputEdge = 2048

// Options choose the steps. At least one is required.
type Options struct {
	RemoveBackground bool `json:"removeBackground"`
	Trim             bool `json:"trim"`
	// MaxEdge resizes so the long edge is at most this many pixels (0 = no
	// resize).
	MaxEdge int `json:"maxEdge"`
	// Provider runs background removal (default: the first configured
	// provider that can).
	Provider string `json:"provider"`
}

// ProviderError is a failure reported by the background-removal provider.
// Its message is meant for the user.
type ProviderError struct{ Msg string }

func (e *ProviderError) Error() string { return e.Msg }

// Config tunes the service.
type Config struct {
	// PollInterval between status checks (default 1 s).
	PollInterval time.Duration
	// Timeout bounds one background removal, retries included (default 90 s).
	Timeout time.Duration
	// Attempts is how many times a transient failure is tried (default 3).
	Attempts int
}

// Service processes product photographs.
type Service struct {
	cat       *catalog.Service
	providers *provider.Registry
	log       *slog.Logger
	cfg       Config
	now       func() time.Time
}

// New returns a processing service.
func New(cat *catalog.Service, providers *provider.Registry, log *slog.Logger, cfg Config) *Service {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.Attempts <= 0 {
		cfg.Attempts = 3
	}
	return &Service{cat: cat, providers: providers, log: log, cfg: cfg, now: time.Now}
}

func invalid(field, reason string) error {
	return &catalog.ValidationError{Fields: map[string]string{field: reason}}
}

// BackgroundRemover returns the provider used for background removal by
// default, or nil when none is configured. A real service is preferred over
// the offline fake.
func (s *Service) BackgroundRemover() provider.Provider {
	var fallback provider.Provider
	for _, p := range s.providers.All() {
		if !provider.Supports(p, provider.RemoveBackground) {
			continue
		}
		if p.Name() != "fake" {
			return p
		}
		fallback = p
	}
	return fallback
}

// Process creates the processed version of an original product photograph,
// replacing any previous one.
func (s *Service) Process(ctx context.Context, productID, imageID int64, opt Options) (catalog.ProductImage, error) {
	if !opt.RemoveBackground && !opt.Trim && opt.MaxEdge == 0 {
		return catalog.ProductImage{}, invalid("steps", "choose at least one step")
	}
	if opt.MaxEdge != 0 && (opt.MaxEdge < MinEdge || opt.MaxEdge > MaxEdge) {
		return catalog.ProductImage{}, invalid("maxEdge", fmt.Sprintf("must be between %d and %d pixels", MinEdge, MaxEdge))
	}
	var remover provider.Provider
	if opt.RemoveBackground {
		var err error
		if remover, err = s.remover(opt.Provider); err != nil {
			return catalog.ProductImage{}, err
		}
	}

	src, data, err := s.cat.ReadProductImage(ctx, productID, imageID)
	if err != nil {
		return catalog.ProductImage{}, err
	}
	if !src.Original() {
		return catalog.ProductImage{}, invalid("imageId", "only an original photograph can be processed")
	}
	img, err := imaging.Decode(data)
	if err != nil {
		return catalog.ProductImage{}, err
	}

	start := s.now()
	proc := catalog.Processing{}
	if opt.RemoveBackground {
		mask, jobID, cost, err := s.mask(ctx, remover, img)
		if err != nil {
			return catalog.ProductImage{}, err
		}
		img = imaging.ApplyMask(img, mask)
		proc.Steps = append(proc.Steps, catalog.StepRemoveBackground)
		proc.Provider, proc.ProviderModel, proc.ProviderJobID = remover.Name(), remover.Model(provider.RemoveBackground), jobID
		proc.CostMicros = cost
	}
	if opt.Trim {
		trimmed, ok := imaging.Trim(img, DefaultTrimMarginPct)
		if !ok {
			return catalog.ProductImage{}, invalid("trim", "trimming needs a transparent background: remove the background as well")
		}
		img = trimmed
		proc.Steps = append(proc.Steps, catalog.StepTrim)
		proc.TrimMarginPct = DefaultTrimMarginPct
	}
	if b := img.Bounds(); opt.MaxEdge != 0 {
		// A photograph already within the limit is left as it is, and the
		// step is not recorded.
		switch {
		case max(b.Dx(), b.Dy()) > opt.MaxEdge:
			img = imaging.Fit(img, opt.MaxEdge)
			proc.Steps = append(proc.Steps, catalog.StepResize)
			proc.MaxEdge = opt.MaxEdge
		case len(proc.Steps) == 0:
			return catalog.ProductImage{}, invalid("maxEdge", fmt.Sprintf("the photograph is already %d×%d pixels", b.Dx(), b.Dy()))
		}
	}

	// A cut-out stays lossless even when trimming left no transparent pixel.
	out, _, err := imaging.Encode(img, opt.RemoveBackground)
	if err != nil {
		return catalog.ProductImage{}, fmt.Errorf("encode the processed photograph: %w", err)
	}
	proc.DurationMs = s.now().Sub(start).Milliseconds()
	// The provider work is done and paid for: always record the result.
	im, err := s.cat.AddProcessedImage(context.WithoutCancel(ctx), productID, imageID, out, proc)
	if err != nil {
		return catalog.ProductImage{}, err
	}
	s.log.InfoContext(ctx, "photograph processed", "product", productID, "image", imageID, "processed", im.ID,
		"steps", proc.Steps, "provider", proc.Provider, "cost_micros", proc.CostMicros, "duration_ms", proc.DurationMs)
	return im, nil
}

func (s *Service) remover(name string) (provider.Provider, error) {
	if name == "" {
		if p := s.BackgroundRemover(); p != nil {
			return p, nil
		}
		return nil, invalid("removeBackground", "no background-removal service is configured (set AIFS_REPLICATE_API_TOKEN)")
	}
	p, err := s.providers.Get(name)
	if err != nil {
		return nil, invalid("provider", err.Error())
	}
	if !provider.Supports(p, provider.RemoveBackground) {
		return nil, invalid("provider", fmt.Sprintf("%s cannot remove backgrounds", p.Label()))
	}
	return p, nil
}

// mask sends a scaled-down, opaque copy of img to the provider and returns
// the mask it produces, the job id and the cost.
func (s *Service) mask(ctx context.Context, p provider.Provider, img *image.NRGBA) (image.Image, string, int64, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, imaging.Flatten(imaging.Fit(img, maskInputEdge), color.White), &jpeg.Options{Quality: 92}); err != nil {
		return nil, "", 0, err
	}
	req := provider.Request{
		Capability: provider.RemoveBackground,
		Source:     &provider.Image{Role: "source", ContentType: imaging.TypeJPEG, Data: buf.Bytes()},
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= s.cfg.Attempts; attempt++ {
		res, jobID, err := s.run(ctx, p, req)
		if err == nil {
			if res.Status == provider.Failed {
				return nil, jobID, 0, &ProviderError{Msg: nonEmpty(res.Error, p.Label()+" could not remove the background")}
			}
			if len(res.Images) == 0 {
				return nil, jobID, 0, &ProviderError{Msg: p.Label() + " returned no mask"}
			}
			m, _, derr := image.Decode(bytes.NewReader(res.Images[0].Data))
			if derr != nil {
				return nil, jobID, 0, &ProviderError{Msg: p.Label() + " returned an unreadable mask"}
			}
			return m, jobID, s.providers.Cost(p.Name(), res), nil
		}
		lastErr = err
		transient, wait := provider.IsTransient(err)
		if !transient && !errors.Is(err, provider.ErrUnknownJob) {
			break
		}
		if attempt == s.cfg.Attempts {
			break
		}
		s.log.WarnContext(ctx, "background removal: retrying", "provider", p.Name(), "attempt", attempt, "err", err)
		if wait <= 0 {
			wait = time.Duration(attempt) * 2 * time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, "", 0, &ProviderError{Msg: "background removal timed out"}
		}
	}
	if ctx.Err() != nil {
		return nil, "", 0, &ProviderError{Msg: "background removal timed out"}
	}
	return nil, "", 0, &ProviderError{Msg: lastErr.Error()}
}

// run submits one job and polls it to completion. Transient poll errors
// are retried until the timeout.
func (s *Service) run(ctx context.Context, p provider.Provider, req provider.Request) (provider.Result, string, error) {
	jobID, err := p.Submit(ctx, req)
	if err != nil {
		return provider.Result{}, "", err
	}
	for {
		res, err := p.Poll(ctx, jobID)
		if err == nil && res.Status != provider.Pending {
			return res, jobID, nil
		}
		if err != nil {
			if transient, _ := provider.IsTransient(err); !transient {
				return provider.Result{}, jobID, err
			}
		}
		select {
		case <-time.After(s.cfg.PollInterval):
		case <-ctx.Done():
			return provider.Result{}, jobID, ctx.Err()
		}
	}
}

func nonEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
