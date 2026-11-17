// Package replicate runs background removal on Replicate.
//
// API reference: https://replicate.com/docs/reference/http. A prediction is
// started with POST /v1/predictions and polled with
// GET /v1/predictions/{id}. The input image is sent as a data URI (the
// server is not publicly reachable); the output is a file URL on
// replicate.delivery that expires after an hour and is downloaded as soon
// as the prediction succeeds.
//
// The model is 851-labs/background-remover with background_type "map", so
// the result is a mask (white = garment, black = background) at the input's
// size. The caller applies it to the original pixels.
package replicate

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

// Name is the provider identifier.
const Name = "replicate"

// DefaultModel is the background remover, pinned to a version so results
// can be reproduced.
const DefaultModel = "851-labs/background-remover:a029dff38972b5fda4ec5d75d7d1cd25aeff621d2cf4946a41055d7db66b80bc"

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://api.replicate.com"

// maxInputBytes keeps the JSON request reasonable. Callers scale images
// down before sending them, so this is only a safety net.
const maxInputBytes = 16 << 20

// maxOutputBytes bounds a downloaded mask.
const maxOutputBytes = 64 << 20

// Config configures the adapter. Only APIToken is required.
type Config struct {
	APIToken string
	// Model is "owner/name:version" (default DefaultModel).
	Model string
	// BaseURL defaults to DefaultBaseURL.
	BaseURL string
	// OutputHosts are the hosts results may be downloaded from, in addition
	// to replicate.delivery and its subdomains.
	OutputHosts []string
	// HTTPClient defaults to a client with a 60 s timeout.
	HTTPClient *http.Client
}

// Provider calls the Replicate API.
type Provider struct {
	cfg     Config
	version string
}

// New validates cfg and returns the provider.
func New(cfg Config) (*Provider, error) {
	if cfg.APIToken == "" {
		return nil, errors.New("replicate: an API token is required")
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	_, version, ok := strings.Cut(cfg.Model, ":")
	if !ok || version == "" || !strings.Contains(cfg.Model[:len(cfg.Model)-len(version)], "/") {
		return nil, fmt.Errorf("replicate: model must be owner/name:version, got %q", cfg.Model)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Provider{cfg: cfg, version: version}, nil
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return Name }

// Label implements provider.Provider.
func (p *Provider) Label() string { return "Replicate background removal" }

// Capabilities implements provider.Provider.
func (p *Provider) Capabilities() []provider.Capability {
	return []provider.Capability{provider.RemoveBackground}
}

// Model implements provider.Provider.
func (p *Provider) Model(provider.Capability) string { return p.cfg.Model }

type predictionRequest struct {
	Version string         `json:"version"`
	Input   map[string]any `json:"input"`
}

type prediction struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	Output      json.RawMessage `json:"output"`
	Error       json.RawMessage `json:"error"`
	DataRemoved bool            `json:"data_removed"`
}

// Submit implements provider.Provider.
func (p *Provider) Submit(ctx context.Context, req provider.Request) (string, error) {
	if req.Capability != provider.RemoveBackground {
		return "", fmt.Errorf("replicate: unsupported capability %q", req.Capability)
	}
	if req.Source == nil || len(req.Source.Data) == 0 {
		return "", errors.New("replicate: background removal needs an image")
	}
	if len(req.Source.Data) > maxInputBytes {
		return "", fmt.Errorf("replicate: the image is %d MB; scale it down first", len(req.Source.Data)>>20)
	}
	ct := req.Source.ContentType
	if ct == "" {
		ct = http.DetectContentType(req.Source.Data)
	}
	body, _ := json.Marshal(predictionRequest{
		Version: p.version,
		Input: map[string]any{
			"image":           "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(req.Source.Data),
			"background_type": "map",
		},
	})
	resp, err := p.do(ctx, http.MethodPost, "/v1/predictions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", httpError(resp, "start background removal")
	}
	var pr prediction
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pr); err != nil || pr.ID == "" {
		return "", provider.Transient(errors.New("replicate: unreadable response when starting the prediction"), 0)
	}
	return pr.ID, nil
}

// Poll implements provider.Provider.
func (p *Provider) Poll(ctx context.Context, jobID string) (provider.Result, error) {
	resp, err := p.do(ctx, http.MethodGet, "/v1/predictions/"+url.PathEscape(jobID), nil)
	if err != nil {
		return provider.Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return provider.Result{}, provider.ErrUnknownJob
	}
	if resp.StatusCode != http.StatusOK {
		return provider.Result{}, httpError(resp, "check background removal")
	}
	var pr prediction
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&pr); err != nil {
		return provider.Result{}, provider.Transient(fmt.Errorf("replicate: unreadable status: %w", err), 0)
	}

	switch pr.Status {
	case "starting", "processing":
		return provider.Result{Status: provider.Pending}, nil
	case "failed":
		msg := "Replicate could not remove the background"
		if e := rawString(pr.Error); e != "" {
			msg += " (" + e + ")"
		}
		return provider.Result{Status: provider.Failed, Error: msg}, nil
	case "canceled":
		return provider.Result{Status: provider.Failed, Error: "the Replicate prediction was cancelled"}, nil
	case "succeeded":
	default:
		return provider.Result{}, fmt.Errorf("replicate: unknown prediction status %q", pr.Status)
	}

	out := outputURL(pr.Output)
	if out == "" {
		if pr.DataRemoved {
			return provider.Result{}, provider.ErrUnknownJob // expired; resubmit
		}
		return provider.Result{Status: provider.Failed, Error: "Replicate reported success but returned no image"}, nil
	}
	data, err := p.download(ctx, out)
	if err != nil {
		return provider.Result{}, err
	}
	return provider.Result{Status: provider.Succeeded, Images: []provider.Output{{ContentType: http.DetectContentType(data), Data: data}}}, nil
}

// rawString renders a JSON error value (a string, or anything else) as text.
func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	if t := strings.TrimSpace(string(raw)); t != "null" {
		return t
	}
	return ""
}

// outputURL accepts a single URL or a list of URLs.
func outputURL(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

// download fetches a result. Only Replicate's delivery hosts (and any
// configured extra hosts) are allowed, because the URL comes from a remote
// response. The API token is not sent: delivery URLs are signed.
func (p *Provider) download(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !p.allowedHost(u.Hostname()) {
		return nil, fmt.Errorf("replicate: refusing to download a result from %q", raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, provider.Transient(fmt.Errorf("replicate: download result: %w", unwrapURLError(err)), 0)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("replicate: download result: HTTP %d", resp.StatusCode)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return nil, provider.Transient(err, 0)
		}
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxOutputBytes+1))
	if err != nil {
		return nil, provider.Transient(fmt.Errorf("replicate: download result: %w", err), 0)
	}
	if len(data) > maxOutputBytes {
		return nil, errors.New("replicate: the result is larger than 64 MB")
	}
	return data, nil
}

func (p *Provider) allowedHost(h string) bool {
	h = strings.ToLower(h)
	if h == "replicate.delivery" || strings.HasSuffix(h, ".replicate.delivery") {
		return true
	}
	for _, a := range p.cfg.OutputHosts {
		if strings.EqualFold(h, a) {
			return true
		}
	}
	return false
}

func (p *Provider) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.cfg.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		// Never leave a forgotten prediction running.
		req.Header.Set("Cancel-After", "5m")
	}
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Never include the request (it carries the token) in the error.
		return nil, provider.Transient(fmt.Errorf("replicate: cannot reach the API: %w", unwrapURLError(err)), 0)
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

type apiError struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// httpError classifies a non-2xx response. Rate limits and server errors
// are transient; a bad token, missing credit and invalid requests are not.
func httpError(resp *http.Response, action string) error {
	var ae apiError
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&ae)
	detail := strings.TrimSpace(ae.Detail)
	if detail == "" {
		detail = strings.TrimSpace(ae.Title)
	}
	retry := retryAfter(resp.Header.Get("Retry-After"))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.New("the Replicate API token was rejected (check AIFS_REPLICATE_API_TOKEN)")
	case resp.StatusCode == http.StatusPaymentRequired:
		return errors.New("the Replicate account has no credit left")
	case resp.StatusCode == http.StatusTooManyRequests:
		if retry == 0 {
			retry = 10 * time.Second
		}
		return provider.Transient(errors.New("rate limit reached on Replicate"), retry)
	case resp.StatusCode >= 500:
		return provider.Transient(fmt.Errorf("server error on Replicate (HTTP %d) while trying to %s", resp.StatusCode, action), retry)
	default:
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("could not %s on Replicate: %s", action, detail)
	}
}

func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}
