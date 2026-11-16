// Package fashn is the FASHN virtual try-on provider (approach A).
//
// API reference: https://docs.fashn.ai. A job is started with
// POST /v1/run and polled with GET /v1/status/{id}. Input images are sent as
// data URIs (the server is not publicly reachable, so URLs are not an
// option); output images are CDN URLs that expire after three days and are
// downloaded as soon as the job completes.
package fashn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

// Name is the provider identifier stored with generations.
const Name = "fashn"

// Models supported by this adapter.
const (
	ModelTryOnMax = "tryon-max"  // recommended (Preview at the time of writing)
	ModelTryOnV16 = "tryon-v1.6" // stable, cheaper, faster
)

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://api.fashn.ai"

// maxInputBytes is FASHN's documented per-image limit (30 MiB).
const maxInputBytes = 30 << 20

// maxOutputBytes bounds a downloaded result.
const maxOutputBytes = 64 << 20

// Config configures the adapter. Only APIKey is required.
type Config struct {
	APIKey string
	// Model is tryon-max (default) or tryon-v1.6.
	Model string
	// Resolution for tryon-max: 1k, 2k (default) or 4k.
	Resolution string
	// Mode is fast, balanced (default) or quality. For tryon-v1.6, "fast"
	// maps to its "performance" mode.
	Mode string
	// CreditPriceMicros is the price of one FASHN credit in micro-USD
	// (default 75 000, i.e. $0.075).
	CreditPriceMicros int64
	// BaseURL defaults to DefaultBaseURL.
	BaseURL string
	// OutputHosts are the hosts results may be downloaded from (defaults to
	// FASHN's CDNs). Anything else is refused.
	OutputHosts []string
	// HTTPClient defaults to a client with a 60 s timeout.
	HTTPClient *http.Client
}

// Provider calls the FASHN API.
type Provider struct {
	cfg Config
}

// New validates cfg and returns the provider.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("fashn: an API key is required")
	}
	if cfg.Model == "" {
		cfg.Model = ModelTryOnMax
	}
	if cfg.Model != ModelTryOnMax && cfg.Model != ModelTryOnV16 {
		return nil, fmt.Errorf("fashn: model must be %s or %s, got %q", ModelTryOnMax, ModelTryOnV16, cfg.Model)
	}
	if cfg.Resolution == "" {
		cfg.Resolution = "2k"
	}
	if cfg.Resolution != "1k" && cfg.Resolution != "2k" && cfg.Resolution != "4k" {
		return nil, fmt.Errorf("fashn: resolution must be 1k, 2k or 4k, got %q", cfg.Resolution)
	}
	if cfg.Mode == "" {
		cfg.Mode = "balanced"
	}
	if cfg.Mode != "fast" && cfg.Mode != "balanced" && cfg.Mode != "quality" {
		return nil, fmt.Errorf("fashn: mode must be fast, balanced or quality, got %q", cfg.Mode)
	}
	if cfg.CreditPriceMicros <= 0 {
		cfg.CreditPriceMicros = 75_000
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if len(cfg.OutputHosts) == 0 {
		cfg.OutputHosts = []string{"cdn.fashn.ai", "media.fashn.ai"}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Provider{cfg: cfg}, nil
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return Name }

// Label implements provider.Provider.
func (p *Provider) Label() string { return "FASHN virtual try-on" }

// Capabilities implements provider.Provider.
func (p *Provider) Capabilities() []provider.Capability {
	return []provider.Capability{provider.TryOn}
}

// Model implements provider.Provider: the model name plus the settings that
// change its output, so the snapshot records exactly what ran.
func (p *Provider) Model(provider.Capability) string {
	if p.cfg.Model == ModelTryOnMax {
		return fmt.Sprintf("%s (%s, %s)", p.cfg.Model, p.cfg.Resolution, p.cfg.Mode)
	}
	return fmt.Sprintf("%s (%s)", p.cfg.Model, p.v16Mode())
}

// CreditsPerCall is the documented credit cost of one image with the
// configured model, resolution and mode.
func (p *Provider) CreditsPerCall() int64 {
	if p.cfg.Model == ModelTryOnV16 {
		return 1
	}
	table := map[string][3]int64{"fast": {1, 2, 3}, "balanced": {2, 3, 4}, "quality": {3, 4, 5}}
	col := map[string]int{"1k": 0, "2k": 1, "4k": 2}[p.cfg.Resolution]
	return table[p.cfg.Mode][col]
}

// UnitPriceMicros is the expected cost of one call, for estimates and as the
// fallback when FASHN does not report credits.
func (p *Provider) UnitPriceMicros() int64 { return p.CreditsPerCall() * p.cfg.CreditPriceMicros }

func (p *Provider) v16Mode() string {
	if p.cfg.Mode == "fast" {
		return "performance"
	}
	return p.cfg.Mode
}

// --- run -----------------------------------------------------------------

type runRequest struct {
	ModelName string         `json:"model_name"`
	Inputs    map[string]any `json:"inputs"`
}

type runResponse struct {
	ID    string          `json:"id"`
	Error json.RawMessage `json:"error"`
}

// apiError is the body of a non-2xx response.
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Submit implements provider.Provider.
func (p *Provider) Submit(ctx context.Context, req provider.Request) (string, error) {
	if req.Capability != provider.TryOn {
		return "", fmt.Errorf("fashn: unsupported capability %q", req.Capability)
	}
	if req.Source == nil || len(req.Garments) == 0 {
		return "", errors.New("fashn: try-on needs a person image and a garment image")
	}
	person, err := dataURI(*req.Source)
	if err != nil {
		return "", fmt.Errorf("person image: %w", err)
	}
	garment, err := dataURI(req.Garments[0])
	if err != nil {
		return "", fmt.Errorf("garment image: %w", err)
	}

	seed := int64(uint64(req.Seed) % (1 << 32)) //nolint:gosec // FASHN seeds are 0..2^32-1
	inputs := map[string]any{
		"model_image":   person,
		"seed":          seed,
		"output_format": "png",
		"return_base64": false,
	}
	if p.cfg.Model == ModelTryOnMax {
		inputs["product_image"] = garment
		inputs["resolution"] = p.cfg.Resolution
		inputs["generation_mode"] = p.cfg.Mode
		inputs["num_images"] = 1
		if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
			inputs["prompt"] = prompt
		}
	} else {
		inputs["garment_image"] = garment
		inputs["category"] = category(req.GarmentCategory)
		inputs["mode"] = p.v16Mode()
		inputs["num_samples"] = 1
	}

	body, _ := json.Marshal(runRequest{ModelName: p.cfg.Model, Inputs: inputs})
	resp, err := p.do(ctx, http.MethodPost, "/v1/run", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", httpError(resp, "start the try-on")
	}
	var rr runResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rr); err != nil {
		return "", provider.Transient(fmt.Errorf("fashn: unreadable response: %w", err), 0)
	}
	if rr.ID == "" {
		msg := strings.TrimSpace(string(rr.Error))
		if msg == "" || msg == "null" {
			msg = "no job id in the response"
		}
		return "", fmt.Errorf("fashn: %s", msg)
	}
	return rr.ID, nil
}

// category maps the user's product category to FASHN v1.6 categories.
func category(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	switch c {
	case "jeans", "trousers", "pants", "shorts", "skirt", "skirts", "bottoms":
		return "bottoms"
	case "shirt", "t-shirt", "top", "tops", "jacket", "sweater", "hoodie":
		return "tops"
	case "dress", "dresses", "jumpsuit", "one-piece", "one-pieces":
		return "one-pieces"
	}
	return "auto"
}

func dataURI(im provider.Image) (string, error) {
	if len(im.Data) == 0 {
		return "", errors.New("empty image")
	}
	if len(im.Data) > maxInputBytes {
		return "", fmt.Errorf("the image is %d MB; FASHN accepts at most 30 MB", len(im.Data)>>20)
	}
	ct := im.ContentType
	if ct == "" {
		ct = http.DetectContentType(im.Data)
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(im.Data), nil
}

// --- status ----------------------------------------------------------------

type statusResponse struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Output []string `json:"output"`
	Error  *struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error"`
}

// Poll implements provider.Provider.
func (p *Provider) Poll(ctx context.Context, jobID string) (provider.Result, error) {
	resp, err := p.do(ctx, http.MethodGet, "/v1/status/"+url.PathEscape(jobID), nil)
	if err != nil {
		return provider.Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return provider.Result{}, provider.ErrUnknownJob
	}
	if resp.StatusCode != http.StatusOK {
		return provider.Result{}, httpError(resp, "check the try-on")
	}
	var sr statusResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&sr); err != nil {
		return provider.Result{}, provider.Transient(fmt.Errorf("fashn: unreadable status: %w", err), 0)
	}

	switch sr.Status {
	case "starting", "in_queue", "processing":
		return provider.Result{Status: provider.Pending}, nil
	case "failed", "canceled", "cancelled":
		msg := "FASHN could not produce an image"
		if sr.Error != nil {
			msg = describeFailure(sr.Error.Name, sr.Error.Message)
		}
		return provider.Result{Status: provider.Failed, Error: msg}, nil
	case "completed":
	default:
		return provider.Result{}, fmt.Errorf("fashn: unknown job status %q", sr.Status)
	}

	if len(sr.Output) == 0 {
		return provider.Result{Status: provider.Failed, Error: "FASHN reported success but returned no image"}, nil
	}
	data, ct, err := p.download(ctx, sr.Output[0])
	if err != nil {
		return provider.Result{}, err
	}
	res := provider.Result{Status: provider.Succeeded, Images: []provider.Output{{ContentType: ct, Data: data}}}
	if credits, err := strconv.ParseFloat(resp.Header.Get("x-fashn-credits-used"), 64); err == nil && credits >= 0 {
		res.CostMicros = int64(math.Round(credits * float64(p.cfg.CreditPriceMicros)))
		res.CostReported = true
	}
	return res, nil
}

// describeFailure turns FASHN's error names into advice.
func describeFailure(name, message string) string {
	hint := map[string]string{
		"ImageLoadError":         "an input image could not be read",
		"ContentModerationError": "the request was blocked by content moderation",
		"InputValidationError":   "an input was rejected (size, dimensions or format)",
		"PoseError":              "the person's pose could not be detected; try a clearer full-body photograph",
		"ThirdPartyError":        "a service FASHN depends on failed; try again later",
		"UnavailableError":       "FASHN is temporarily unavailable; try again later",
		"PipelineError":          "FASHN's processing failed",
	}[name]
	parts := []string{"FASHN"}
	if hint != "" {
		parts = append(parts, hint)
	} else if name != "" {
		parts = append(parts, name)
	}
	out := strings.Join(parts, ": ")
	if message != "" {
		out += " (" + message + ")"
	}
	return out
}

// download fetches a result from FASHN's CDN. Only the configured hosts are
// allowed: the URL comes from a remote response.
func (p *Provider) download(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !p.allowedHost(u.Hostname()) {
		return nil, "", fmt.Errorf("fashn: refusing to download a result from %q", raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, "", provider.Transient(fmt.Errorf("fashn: download result: %w", err), 0)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("fashn: download result: HTTP %d", resp.StatusCode)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return nil, "", provider.Transient(err, 0)
		}
		return nil, "", err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxOutputBytes+1))
	if err != nil {
		return nil, "", provider.Transient(fmt.Errorf("fashn: download result: %w", err), 0)
	}
	if len(data) > maxOutputBytes {
		return nil, "", errors.New("fashn: the result is larger than 64 MB")
	}
	return data, http.DetectContentType(data), nil
}

func (p *Provider) allowedHost(h string) bool {
	for _, a := range p.cfg.OutputHosts {
		if strings.EqualFold(h, a) {
			return true
		}
	}
	return false
}

// --- HTTP ------------------------------------------------------------------

func (p *Provider) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.cfg.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Never include the request (it carries the key) in the error.
		return nil, provider.Transient(fmt.Errorf("fashn: cannot reach the API: %w", unwrapURLError(err)), 0)
	}
	return resp, nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// httpError classifies a non-2xx response. Rate and concurrency limits and
// server errors are transient; a bad key, missing credits and invalid
// requests are not.
func httpError(resp *http.Response, action string) error {
	var ae apiError
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&ae)
	detail := ae.Message
	if detail == "" {
		detail = ae.Error
	}
	retry := retryAfter(resp.Header.Get("Retry-After"))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("FASHN rejected the API key (check AIFS_FASHN_API_KEY)")
	case resp.StatusCode == http.StatusTooManyRequests && ae.Error == "OutOfCredits":
		return errors.New("the FASHN account has no credits left")
	case resp.StatusCode == http.StatusTooManyRequests:
		if retry == 0 {
			retry = 10 * time.Second
		}
		return provider.Transient(fmt.Errorf("FASHN rate limit reached (%s)", nonEmpty(ae.Error, "RateLimitExceeded")), retry)
	case resp.StatusCode >= 500:
		return provider.Transient(fmt.Errorf("FASHN server error (HTTP %d) while trying to %s", resp.StatusCode, action), retry)
	default:
		return fmt.Errorf("FASHN could not %s: %s", action, nonEmpty(detail, fmt.Sprintf("HTTP %d", resp.StatusCode)))
	}
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
