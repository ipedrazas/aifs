package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MetricsFilter selects the generations measured. Zero values match all.
type MetricsFilter struct {
	// Provider matches generations where any step used this provider.
	Provider  string
	Recipe    string
	ProductID int64
	// From and To bound the generation's creation day (UTC), inclusive.
	From, To time.Time
	// TestSet limits the metrics to products in the evaluation test set.
	TestSet bool
	// GroupBy is one of MetricsGroupings (default "pipeline").
	GroupBy string
}

// MetricsGroupings are the dimensions metrics can be broken down by.
var MetricsGroupings = []string{"pipeline", "recipe", "provider", "product", "printComplexity", "garmentRefs", "preset"}

// Metrics are the PRD §10 success metrics for a set of generations. Rates
// are over reviewed images (approved, rejected or needing refinement) and
// are nil when nothing has been reviewed.
type Metrics struct {
	Generations int `json:"generations"`
	// Variations is how many images were requested; Images how many were
	// produced (the rest failed or were cancelled).
	Variations      int `json:"variations"`
	Images          int `json:"images"`
	Pending         int `json:"pending"`
	Reviewed        int `json:"reviewed"`
	Approved        int `json:"approved"`
	NeedsRefinement int `json:"needsRefinement"`
	// Rejected counts rejections by reason: fidelity, photo_quality, other.
	Rejected map[string]int `json:"rejected"`

	ApprovalRate *float64 `json:"approvalRate"`
	// RejectionRates are rejections by reason over reviewed images.
	RejectionRates map[string]float64 `json:"rejectionRates"`
	// FidelityPassRate is the share of reviewed images not rejected for
	// fidelity: the most important metric (PRD §10).
	FidelityPassRate *float64 `json:"fidelityPassRate"`

	// CostMicros is everything spent, including failed and rejected work.
	CostMicros            int64  `json:"costMicros"`
	CostPerApprovedMicros *int64 `json:"costPerApprovedMicros"`
	// AvgGenerationSeconds is the mean time from request to image for
	// approved images; AvgTimeToApprovalSeconds runs from request to the
	// approval decision, so it includes waiting for review.
	AvgGenerationSeconds     *float64 `json:"avgGenerationSeconds"`
	AvgTimeToApprovalSeconds *float64 `json:"avgTimeToApprovalSeconds"`

	// Garments is the number of distinct products generated for.
	Garments               int      `json:"garments"`
	UsableImagesPerGarment *float64 `json:"usableImagesPerGarment"`
}

// MetricsGroup is the metrics of one value of the grouping dimension.
type MetricsGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Metrics
}

// MetricsSummary is the body of GET /api/v1/metrics/summary.
type MetricsSummary struct {
	GroupBy string         `json:"groupBy"`
	Total   Metrics        `json:"total"`
	Groups  []MetricsGroup `json:"groups"`
}

// metricsGeneration is one measured generation and its images.
type metricsGeneration struct {
	id         int64
	created    time.Time
	variations int
	cost       int64
	productID  int64
	keys       map[string][2]string // grouping -> key, label
	providers  []string
	images     []metricsImage
}

type metricsImage struct {
	status   string
	reason   string
	produced time.Time // the last step of its variation finished
	reviewed *time.Time
}

// ParseMetricsDay parses a YYYY-MM-DD filter bound.
func ParseMetricsDay(field, v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return time.Time{}, invalid(field, "must be a date (YYYY-MM-DD)")
	}
	return t, nil
}

// MetricsSummary computes the success metrics for the filtered generations,
// in total and broken down by f.GroupBy.
func (s *Service) MetricsSummary(ctx context.Context, f MetricsFilter) (MetricsSummary, error) {
	if f.GroupBy == "" {
		f.GroupBy = "pipeline"
	}
	if !slices.Contains(MetricsGroupings, f.GroupBy) {
		return MetricsSummary{}, invalid("groupBy", "must be one of "+strings.Join(MetricsGroupings, ", "))
	}
	if !f.From.IsZero() && !f.To.IsZero() && day(f.To).Before(day(f.From)) {
		return MetricsSummary{}, invalid("to", "must not be before from")
	}
	gens, err := s.metricsGenerations(ctx, f)
	if err != nil {
		return MetricsSummary{}, err
	}

	out := MetricsSummary{GroupBy: f.GroupBy, Total: aggregate(gens), Groups: []MetricsGroup{}}
	byKey := map[string][]*metricsGeneration{}
	labels := map[string]string{}
	for _, g := range gens {
		k := g.keys[f.GroupBy]
		byKey[k[0]] = append(byKey[k[0]], g)
		labels[k[0]] = k[1]
	}
	for k, gs := range byKey {
		out.Groups = append(out.Groups, MetricsGroup{Key: k, Label: labels[k], Metrics: aggregate(gs)})
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Label < out.Groups[j].Label })
	return out, nil
}

const metricsGenerationsSQL = `SELECT g.id, g.created_at, g.variations, g.cost_micros, g.recipe, g.settings, p.id, p.name, p.print_complexity
	FROM generation g JOIN product p ON p.id = g.product_id WHERE `

const metricsImagesSQL = `SELECT gi.generation_id, gi.review_status, COALESCE(gi.rejection_reason, ''), gi.reviewed_at,
		COALESCE((SELECT max(st.finished_at) FROM generation_step st
			WHERE st.generation_id = gi.generation_id AND st.variation = gi.variation), gi.created_at)
	FROM generated_image gi JOIN generation g ON g.id = gi.generation_id JOIN product p ON p.id = g.product_id WHERE `

func (s *Service) metricsGenerations(ctx context.Context, f MetricsFilter) ([]*metricsGeneration, error) {
	where := []string{"1 = 1"}
	var args []any
	if f.Recipe != "" {
		where, args = append(where, "g.recipe = ?"), append(args, f.Recipe)
	}
	if f.ProductID > 0 {
		where, args = append(where, "g.product_id = ?"), append(args, f.ProductID)
	}
	if !f.From.IsZero() {
		where, args = append(where, "g.created_at >= ?"), append(args, day(f.From).Format(time.RFC3339Nano))
	}
	if !f.To.IsZero() {
		where, args = append(where, "g.created_at < ?"), append(args, day(f.To).AddDate(0, 0, 1).Format(time.RFC3339Nano))
	}
	if f.TestSet {
		where = append(where, "p.test_set = 1")
	}
	cond := strings.Join(where, " AND ")

	rows, err := s.db.Read.QueryContext(ctx, metricsGenerationsSQL+cond+` ORDER BY g.id`, args...) //nolint:gosec // cond is built from constants
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var gens []*metricsGeneration
	byID := map[int64]*metricsGeneration{}
	for rows.Next() {
		g := &metricsGeneration{keys: map[string][2]string{}}
		var created, recipe, settings, productName, complexity string
		if err := rows.Scan(&g.id, &created, &g.variations, &g.cost, &recipe, &settings, &g.productID, &productName, &complexity); err != nil {
			return nil, err
		}
		g.created = parseTime(created)
		var snap Snapshot
		if err := json.Unmarshal([]byte(settings), &snap); err != nil {
			return nil, fmt.Errorf("generation %d settings: %w", g.id, err)
		}
		g.providers = stepProviderChain(snap)
		if f.Provider != "" && !slices.Contains(g.providers, f.Provider) {
			continue
		}
		chain := strings.Join(g.providers, " → ")
		g.keys["pipeline"] = [2]string{recipe + "|" + chain, recipe + " · " + chain}
		g.keys["recipe"] = [2]string{recipe, recipe}
		g.keys["provider"] = [2]string{chain, chain}
		g.keys["product"] = [2]string{strconv.FormatInt(g.productID, 10), productName}
		g.keys["printComplexity"] = [2]string{complexity, nonEmpty(complexity, "unrated")}
		refs := garmentRefs(snap)
		g.keys["garmentRefs"] = [2]string{strconv.Itoa(refs), fmt.Sprintf("%d garment photo%s", refs, plural(refs))}
		g.keys["preset"] = [2]string{snap.Preset.Slug + "|" + strconv.FormatInt(snap.Preset.ID, 10), nonEmpty(snap.Preset.Name, "no preset")}
		gens = append(gens, g)
		byID[g.id] = g
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(gens) == 0 {
		return gens, nil
	}

	irows, err := s.db.Read.QueryContext(ctx, metricsImagesSQL+cond, args...) //nolint:gosec // cond is built from constants
	if err != nil {
		return nil, err
	}
	defer func() { _ = irows.Close() }()
	for irows.Next() {
		var id int64
		var im metricsImage
		var reviewed *string
		var produced string
		if err := irows.Scan(&id, &im.status, &im.reason, &reviewed, &produced); err != nil {
			return nil, err
		}
		im.reviewed, im.produced = parseTimePtr(reviewed), parseTime(produced)
		if g, ok := byID[id]; ok {
			g.images = append(g.images, im)
		}
	}
	return gens, irows.Err()
}

// day is the start of t's day in UTC.
func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// stepProviderChain lists the providers of a generation's steps in order,
// without consecutive repeats ("gemini", "fashn").
func stepProviderChain(snap Snapshot) []string {
	var out []string
	for _, st := range snap.Steps {
		if len(out) == 0 || out[len(out)-1] != st.Provider {
			out = append(out, st.Provider)
		}
	}
	if len(out) == 0 && snap.Provider != "" {
		out = []string{snap.Provider}
	}
	return out
}

// garmentRefs counts the distinct garment photographs sent in any step.
func garmentRefs(snap Snapshot) int {
	sent := map[int]bool{}
	for _, st := range snap.Steps {
		for _, pos := range st.Inputs {
			if pos >= 1 && pos <= len(snap.Inputs) && snap.Inputs[pos-1].Role == "garment" {
				sent[pos] = true
			}
		}
	}
	return len(sent)
}

func aggregate(gens []*metricsGeneration) Metrics {
	m := Metrics{Rejected: map[string]int{}, RejectionRates: map[string]float64{}}
	garments := map[int64]bool{}
	var genSecs, approvalSecs float64
	var approvalTimed int
	for _, g := range gens {
		m.Generations++
		m.Variations += g.variations
		m.CostMicros += g.cost
		garments[g.productID] = true
		for _, im := range g.images {
			m.Images++
			switch im.status {
			case "pending":
				m.Pending++
				continue
			case "approved":
				m.Approved++
				genSecs += im.produced.Sub(g.created).Seconds()
				if im.reviewed != nil {
					approvalSecs += im.reviewed.Sub(g.created).Seconds()
					approvalTimed++
				}
			case "needs_refinement":
				m.NeedsRefinement++
			case "rejected":
				m.Rejected[im.reason]++
			}
			m.Reviewed++
		}
	}
	m.Garments = len(garments)
	if m.Reviewed > 0 {
		r := float64(m.Reviewed)
		m.ApprovalRate = ptrTo(float64(m.Approved) / r)
		m.FidelityPassRate = ptrTo(float64(m.Reviewed-m.Rejected["fidelity"]) / r)
		for reason, n := range m.Rejected {
			m.RejectionRates[reason] = float64(n) / r
		}
	}
	if m.Approved > 0 {
		m.CostPerApprovedMicros = ptrTo(m.CostMicros / int64(m.Approved))
		m.AvgGenerationSeconds = ptrTo(genSecs / float64(m.Approved))
	}
	if approvalTimed > 0 {
		m.AvgTimeToApprovalSeconds = ptrTo(approvalSecs / float64(approvalTimed))
	}
	if m.Garments > 0 {
		m.UsableImagesPerGarment = ptrTo(float64(m.Approved) / float64(m.Garments))
	}
	return m
}

func ptrTo[T any](v T) *T { return &v }

func nonEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
