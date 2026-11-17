// Package gemini is the Gemini image provider (approach B), reached
// through OpenRouter.
//
// API reference: https://openrouter.ai/docs/features/multimodal/image-generation.
// One synchronous call, POST /api/v1/images, takes the prompt and up to 14
// reference images (data URIs; the server is not publicly reachable) and
// returns the image as base64 together with the USD cost.
//
// The provider port is submit-then-poll, so Submit performs the call and
// keeps the result in memory under a fresh job id until Poll collects it.
// After a restart the id is unknown, Poll returns provider.ErrUnknownJob and
// the runner submits again.
package gemini

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

// Name is the provider identifier stored with generations.
const Name = "gemini"

// DefaultModel is Nano Banana Pro (Gemini 3 Pro Image).
const DefaultModel = "google/gemini-3-pro-image"

// DefaultBaseURL is OpenRouter's API.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// DefaultUnitPriceMicros is the fallback cost of one image (USD 0.14) when
// OpenRouter does not report it.
const DefaultUnitPriceMicros = 140_000

// maxInputBytes bounds one reference image sent as a data URI.
const maxInputBytes = 20 << 20

// maxResponseBytes bounds the JSON response (it carries a base64 image).
const maxResponseBytes = 96 << 20

// resultTTL is how long an uncollected result is kept.
const resultTTL = time.Hour

// AspectRatios the Gemini image models accept.
var AspectRatios = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}

// Config configures the adapter. Only APIKey is required.
type Config struct {
	// APIKey is the OpenRouter API key.
	APIKey string
	// Model is the OpenRouter model id (default DefaultModel).
	Model string
	// Resolution is 1K, 2K (default) or 4K. Models without a resolution
	// setting (gemini-2.5-flash-image) ignore it.
	Resolution string
	// MaxReferences is how many images one call may carry (default: 3 for
	// gemini-2.5-flash-image, 14 otherwise).
	MaxReferences int
	// UnitPriceMicros is the fallback cost per image in micro-USD.
	UnitPriceMicros int64
	// BaseURL defaults to DefaultBaseURL.
	BaseURL string
	// HTTPClient defaults to a client with a 5 minute timeout.
	HTTPClient *http.Client
	// Now is the clock (tests).
	Now func() time.Time
}

// Provider calls Gemini image models through OpenRouter.
type Provider struct {
	cfg Config

	mu      sync.Mutex
	results map[string]stored
}

type stored struct {
	res provider.Result
	at  time.Time
}

// New validates cfg and returns the provider.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("gemini: an OpenRouter API key is required")
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if !strings.Contains(cfg.Model, "/") {
		return nil, fmt.Errorf("gemini: model must be an OpenRouter id such as %s, got %q", DefaultModel, cfg.Model)
	}
	if cfg.Resolution == "" {
		cfg.Resolution = "2K"
	}
	cfg.Resolution = strings.ToUpper(cfg.Resolution)
	if cfg.Resolution != "1K" && cfg.Resolution != "2K" && cfg.Resolution != "4K" {
		return nil, fmt.Errorf("gemini: resolution must be 1K, 2K or 4K, got %q", cfg.Resolution)
	}
	if cfg.MaxReferences <= 0 {
		cfg.MaxReferences = 14
		if !supportsResolution(cfg.Model) {
			cfg.MaxReferences = 3
		}
	}
	if cfg.UnitPriceMicros <= 0 {
		cfg.UnitPriceMicros = DefaultUnitPriceMicros
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Provider{cfg: cfg, results: map[string]stored{}}, nil
}

// supportsResolution reports whether the model takes a resolution setting
// (Gemini 2.5 Flash Image does not).
func supportsResolution(model string) bool { return !strings.Contains(model, "gemini-2.5-flash-image") }

// Name implements provider.Provider.
func (p *Provider) Name() string { return Name }

// Label implements provider.Provider.
func (p *Provider) Label() string { return "Gemini image (OpenRouter)" }

// Capabilities implements provider.Provider.
func (p *Provider) Capabilities() []provider.Capability {
	return []provider.Capability{provider.ReferenceGenerate, provider.Edit}
}

// Model implements provider.Provider: the model id plus the resolution.
func (p *Provider) Model(provider.Capability) string {
	if supportsResolution(p.cfg.Model) {
		return fmt.Sprintf("%s (%s)", p.cfg.Model, p.cfg.Resolution)
	}
	return p.cfg.Model
}

// UnitPriceMicros is the expected cost of one image, for estimates.
func (p *Provider) UnitPriceMicros() int64 { return p.cfg.UnitPriceMicros }

// --- request ---------------------------------------------------------------

type imageRef struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

type imagesRequest struct {
	Model           string     `json:"model"`
	Prompt          string     `json:"prompt"`
	N               int        `json:"n"`
	AspectRatio     string     `json:"aspect_ratio,omitempty"`
	Resolution      string     `json:"resolution,omitempty"`
	InputReferences []imageRef `json:"input_references,omitempty"`
}

type imagesResponse struct {
	Data []struct {
		B64JSON   string `json:"b64_json"`
		MediaType string `json:"media_type"`
	} `json:"data"`
	Usage *struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
	Error *apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code     json.RawMessage `json:"code"`
	Message  string          `json:"message"`
	Metadata struct {
		Reasons   []string `json:"reasons"`
		ErrorType string   `json:"error_type"`
		Raw       string   `json:"raw"`
	} `json:"metadata"`
}

// references orders the images sent: the image to change first, then the
// garment photographs, then the model references.
func references(req provider.Request) []provider.Image {
	var out []provider.Image
	if req.Source != nil {
		out = append(out, *req.Source)
	}
	out = append(out, req.Garments...)
	return append(out, req.ModelRefs...)
}

// Legend names each attached image so the prompt can refer to them. It is a
// deterministic function of the stored inputs, so the generation snapshot
// still determines exactly what was sent.
func Legend(req provider.Request) string {
	refs := references(req)
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Attached images, in order:\n")
	for i, im := range refs {
		var what string
		switch im.Role {
		case "source":
			what = "the photograph to work from"
		case "garment":
			what = "garment reference photograph"
		case "model":
			what = "model reference image"
		default:
			what = "reference image"
		}
		if im.Label != "" && im.Role != "source" {
			what += " (" + strings.ReplaceAll(im.Label, "_", " ") + ")"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, what)
	}
	return b.String()
}

// Submit implements provider.Provider. It makes the call synchronously.
func (p *Provider) Submit(ctx context.Context, req provider.Request) (string, error) {
	if req.Capability != provider.ReferenceGenerate && req.Capability != provider.Edit {
		return "", fmt.Errorf("gemini: unsupported capability %q", req.Capability)
	}
	if req.Capability == provider.Edit && req.Source == nil {
		return "", errors.New("gemini: an edit needs the image to change")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return "", errors.New("gemini: the prompt is empty")
	}
	refs := references(req)
	if len(refs) > p.cfg.MaxReferences {
		return "", fmt.Errorf("gemini: %s accepts at most %d reference images; this request has %d", p.cfg.Model, p.cfg.MaxReferences, len(refs))
	}
	body := imagesRequest{Model: p.cfg.Model, N: 1}
	if legend := Legend(req); legend != "" {
		prompt = legend + "\n" + prompt
	}
	body.Prompt = prompt
	if contains(AspectRatios, req.AspectRatio) {
		body.AspectRatio = req.AspectRatio
	}
	if supportsResolution(p.cfg.Model) {
		body.Resolution = p.cfg.Resolution
	}
	for i, im := range refs {
		uri, err := dataURI(im)
		if err != nil {
			return "", fmt.Errorf("gemini: reference image %d: %w", i+1, err)
		}
		r := imageRef{Type: "image_url"}
		r.ImageURL.URL = uri
		body.InputReferences = append(body.InputReferences, r)
	}

	res, err := p.call(ctx, body)
	if err != nil {
		return "", err
	}
	id, err := newJobID()
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.cfg.Now()
	for k, v := range p.results {
		if now.Sub(v.at) > resultTTL {
			delete(p.results, k)
		}
	}
	p.results[id] = stored{res: res, at: now}
	return id, nil
}

// Poll implements provider.Provider. A result can be collected once.
func (p *Provider) Poll(_ context.Context, jobID string) (provider.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.results[jobID]
	if !ok {
		return provider.Result{}, provider.ErrUnknownJob
	}
	delete(p.results, jobID)
	return s.res, nil
}

func (p *Provider) call(ctx context.Context, body imagesRequest) (provider.Result, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return provider.Result{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/images", bytes.NewReader(payload))
	if err != nil {
		return provider.Result{}, err
	}
	hreq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("X-Title", "AI Fashion Studio")

	resp, err := p.cfg.HTTPClient.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return provider.Result{}, ctx.Err()
		}
		// Never include the request (it carries the key) in the error.
		return provider.Result{}, provider.Transient(fmt.Errorf("gemini: cannot reach OpenRouter: %w", unwrapURLError(err)), 0)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return provider.Result{}, provider.Transient(fmt.Errorf("gemini: read the response: %w", err), 0)
	}
	if len(raw) > maxResponseBytes {
		return provider.Result{}, errors.New("gemini: the response is larger than 96 MB")
	}
	var ir imagesResponse
	jsonErr := json.Unmarshal(raw, &ir)
	if resp.StatusCode != http.StatusOK {
		return provider.Result{}, httpError(resp, ir.Error)
	}
	if jsonErr != nil {
		return provider.Result{}, provider.Transient(fmt.Errorf("gemini: unreadable response: %w", jsonErr), 0)
	}
	// OpenRouter may report a failure inside a 200 response.
	if ir.Error != nil {
		return provider.Result{}, bodyError(ir.Error)
	}
	if len(ir.Data) == 0 || ir.Data[0].B64JSON == "" {
		return provider.Result{Status: provider.Failed, Error: "Gemini returned no image (the request may have been declined)"}, nil
	}
	data, err := base64.StdEncoding.DecodeString(ir.Data[0].B64JSON)
	if err != nil {
		return provider.Result{}, fmt.Errorf("gemini: undecodable image: %w", err)
	}
	ct := ir.Data[0].MediaType
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	res := provider.Result{Status: provider.Succeeded, Images: []provider.Output{{ContentType: ct, Data: data}}}
	if ir.Usage != nil && ir.Usage.Cost != nil && *ir.Usage.Cost >= 0 {
		res.CostMicros = int64(math.Round(*ir.Usage.Cost * 1_000_000))
		res.CostReported = true
	}
	return res, nil
}

// --- errors ----------------------------------------------------------------

// httpError classifies a non-200 response. Timeouts, rate limits, upstream
// failures (not billed) and routing outages are transient; a bad key,
// missing credits, moderation blocks and invalid requests are not.
func httpError(resp *http.Response, body *apiErrorBody) error {
	detail := ""
	if body != nil {
		detail = body.Message
	}
	retry := retryAfter(resp.Header.Get("Retry-After"))
	switch code := resp.StatusCode; {
	case code == http.StatusUnauthorized:
		return errors.New("OpenRouter rejected the API key (check AIFS_OPENROUTER_API_KEY)")
	case code == http.StatusPaymentRequired && retry > 0:
		return provider.Transient(errors.New("the OpenRouter spending limit for in-flight requests was reached"), retry)
	case code == http.StatusPaymentRequired:
		return errors.New("the OpenRouter account has insufficient credits")
	case code == http.StatusForbidden:
		return moderationError(body)
	case code == http.StatusTooManyRequests:
		if retry == 0 {
			retry = 10 * time.Second
		}
		return provider.Transient(errors.New("OpenRouter rate limit reached"), retry)
	case code == http.StatusRequestTimeout || code >= 500:
		return provider.Transient(fmt.Errorf("Gemini failed upstream (HTTP %d): %s", code, nonEmpty(detail, "no details")), retry) //nolint:staticcheck // proper noun
	case code == http.StatusNotFound:
		return fmt.Errorf("OpenRouter does not offer this model: %s", nonEmpty(detail, "HTTP 404"))
	case code == http.StatusRequestEntityTooLarge:
		return errors.New("the reference images are too large for OpenRouter; use smaller photographs")
	default:
		return fmt.Errorf("Gemini could not generate the image: %s", nonEmpty(detail, fmt.Sprintf("HTTP %d", code))) //nolint:staticcheck // proper noun
	}
}

// bodyError classifies an error reported inside a 200 response by its code.
func bodyError(body *apiErrorBody) error {
	code, _ := strconv.Atoi(strings.Trim(string(body.Code), `"`))
	switch {
	case code == http.StatusForbidden || body.Metadata.ErrorType == "content_policy_violation":
		return moderationError(body)
	case code == http.StatusTooManyRequests || code == http.StatusRequestTimeout || code >= 500 || code == 0:
		return provider.Transient(fmt.Errorf("Gemini failed: %s", nonEmpty(body.Message, "no details")), 0) //nolint:staticcheck // proper noun
	default:
		return fmt.Errorf("Gemini could not generate the image: %s", nonEmpty(body.Message, "no details")) //nolint:staticcheck // proper noun
	}
}

func moderationError(body *apiErrorBody) error {
	msg := "the request was blocked by content moderation"
	if body != nil && len(body.Metadata.Reasons) > 0 {
		msg += " (" + strings.Join(body.Metadata.Reasons, ", ") + ")"
	} else if body != nil && body.Message != "" {
		msg += " (" + body.Message + ")"
	}
	return errors.New("Gemini: " + msg)
}

// --- helpers ---------------------------------------------------------------

func dataURI(im provider.Image) (string, error) {
	if len(im.Data) == 0 {
		return "", errors.New("empty image")
	}
	if len(im.Data) > maxInputBytes {
		return "", fmt.Errorf("the image is %d MB; at most %d MB can be sent", len(im.Data)>>20, maxInputBytes>>20)
	}
	ct := im.ContentType
	if ct == "" {
		ct = http.DetectContentType(im.Data)
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(im.Data), nil
}

func newJobID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "or-" + hex.EncodeToString(b[:]), nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

func nonEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
