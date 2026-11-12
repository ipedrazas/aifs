// Package provider defines the port to AI image services.
//
// Every capability is submit-then-poll: Submit starts a job and returns its
// id, which the caller persists, and Poll reports progress. A job can be
// resumed after a restart by polling the stored id.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Capability is one kind of operation a provider can perform.
type Capability string

// Capabilities.
const (
	// TryOn dresses the person in Source with the first garment image.
	TryOn Capability = "try_on"
	// ReferenceGenerate creates an image from a prompt plus reference images.
	ReferenceGenerate Capability = "reference_generate"
	// Edit changes Source according to the prompt.
	Edit Capability = "edit"
	// GenerateModel creates a new person from a description.
	GenerateModel Capability = "generate_model"
)

// Image is an input image.
type Image struct {
	// Role is "garment", "model" or "source".
	Role string
	// Label is the product view or model image role, e.g. "front", "identity".
	Label       string
	Hash        string
	ContentType string
	Data        []byte
}

// Request is everything a provider needs for one call.
type Request struct {
	Capability  Capability
	Prompt      string
	Garments    []Image
	ModelRefs   []Image
	Source      *Image // the image to dress (try_on) or change (edit)
	AspectRatio string // e.g. "3:4"
	Seed        int64
}

// Status of a provider job.
type Status string

// Job statuses.
const (
	Pending   Status = "pending"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
)

// Output is a produced image.
type Output struct {
	ContentType string
	Data        []byte
}

// Result is the state of a job.
type Result struct {
	Status Status
	Images []Output
	// CostMicros is the cost in millionths of a US dollar, when the provider
	// reports it (CostReported). Otherwise the configured unit price applies.
	CostMicros   int64
	CostReported bool
	// Error explains a failed job in words a user can act on.
	Error string
}

// Provider is an AI image service.
type Provider interface {
	// Name is the stable identifier stored with every generation.
	Name() string
	// Label is a human-readable name.
	Label() string
	Capabilities() []Capability
	// Model identifies the provider model/version used for a capability.
	Model(Capability) string
	Submit(ctx context.Context, req Request) (jobID string, err error)
	Poll(ctx context.Context, jobID string) (Result, error)
}

// ErrUnknownJob means the provider has no record of a job id (for example
// it expired, or the provider restarted). The caller may resubmit.
var ErrUnknownJob = errors.New("the provider has no record of this job")

// TransientError marks a failure worth retrying: network trouble, rate
// limiting, a temporary outage.
type TransientError struct {
	Err error
	// RetryAfter is the provider's requested delay, if any.
	RetryAfter time.Duration
}

func (e *TransientError) Error() string { return e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

// Transient wraps err as retryable.
func Transient(err error, retryAfter time.Duration) error {
	return &TransientError{Err: err, RetryAfter: retryAfter}
}

// IsTransient reports whether err is worth retrying, and the requested delay.
func IsTransient(err error) (bool, time.Duration) {
	var te *TransientError
	if errors.As(err, &te) {
		return true, te.RetryAfter
	}
	return false, 0
}

// Supports reports whether p offers capability c.
func Supports(p Provider, c Capability) bool {
	for _, x := range p.Capabilities() {
		if x == c {
			return true
		}
	}
	return false
}

// Registry holds the configured providers and their unit prices.
type Registry struct {
	providers map[string]Provider
	prices    map[string]int64 // micro-USD per call, by provider name
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}, prices: map[string]int64{}}
}

// Add registers a provider with its fallback unit price per call.
func (r *Registry) Add(p Provider, unitPriceMicros int64) {
	r.providers[p.Name()] = p
	r.prices[p.Name()] = unitPriceMicros
}

// Get returns a provider by name.
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", name)
	}
	return p, nil
}

// UnitPrice returns the configured price per call in micro-USD.
func (r *Registry) UnitPrice(name string) int64 { return r.prices[name] }

// All returns the providers sorted by name.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Cost returns the cost of a finished job: the reported cost, else the unit
// price.
func (r *Registry) Cost(name string, res Result) int64 {
	if res.CostReported {
		return res.CostMicros
	}
	return r.UnitPrice(name)
}
