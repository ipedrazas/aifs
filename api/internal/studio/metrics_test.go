package studio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/provider/fake"
)

func (e *env) review(im GeneratedImage, status, reason string) {
	e.t.Helper()
	if _, err := e.svc.ReviewImage(context.Background(), im.ID, ReviewInput{Status: status, Reason: reason}); err != nil {
		e.t.Fatal(err)
	}
}

func TestMetricsSummary(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	if _, err := e.cat.UpdateProduct(ctx, e.product.ID, catalog.ProductInput{TestSet: ptr(true), PrintComplexity: ptr("complex")}); err != nil {
		t.Fatal(err)
	}
	other, _ := e.cat.CreateProduct(ctx, catalog.ProductInput{Name: ptr("Plain jeans")})
	if _, err := e.cat.AddProductImage(ctx, other.ID, "front", "f.png", fixture(t, "plain.png")); err != nil {
		t.Fatal(err)
	}
	otherShoot, _ := e.svc.CreatePhotoshoot(ctx, PhotoshootInput{ProductID: &other.ID, ModelID: &e.model.ID})
	e.start()

	ref := e.waitStatus(e.generate("reference", 3).ID, StatusSucceeded)
	e.review(ref.Images[0], "approved", "")
	e.review(ref.Images[1], "rejected", "fidelity")
	e.review(ref.Images[2], "rejected", "photo_quality")
	tryon := e.waitStatus(e.generate("tryon", 2).ID, StatusSucceeded)
	e.review(tryon.Images[0], "approved", "")
	g, err := e.svc.StartGeneration(ctx, otherShoot.ID, GenerationRequest{Recipe: "reference", Provider: fake.Name, Variations: 1})
	if err != nil {
		t.Fatal(err)
	}
	plain := e.waitStatus(g.ID, StatusSucceeded)
	e.review(plain.Images[0], "approved", "")

	sum, err := e.svc.MetricsSummary(ctx, MetricsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	m := sum.Total
	if m.Generations != 3 || m.Variations != 6 || m.Images != 6 || m.Pending != 1 || m.Reviewed != 5 || m.Approved != 3 {
		t.Fatalf("counts: %+v", m)
	}
	if m.Rejected["fidelity"] != 1 || m.Rejected["photo_quality"] != 1 || m.NeedsRefinement != 0 {
		t.Fatalf("rejections: %+v", m.Rejected)
	}
	if *m.ApprovalRate != 0.6 || *m.FidelityPassRate != 0.8 || m.RejectionRates["fidelity"] != 0.2 || m.RejectionRates["photo_quality"] != 0.2 {
		t.Fatalf("rates: %v %v %v", *m.ApprovalRate, *m.FidelityPassRate, m.RejectionRates)
	}
	// 3 + 2×2 + 1 provider calls at $0.01, over 3 approved images.
	if m.CostMicros != 80_000 || *m.CostPerApprovedMicros != 26_666 {
		t.Fatalf("cost: %d %d", m.CostMicros, *m.CostPerApprovedMicros)
	}
	if m.Garments != 2 || *m.UsableImagesPerGarment != 1.5 {
		t.Fatalf("garments: %d %v", m.Garments, *m.UsableImagesPerGarment)
	}
	if m.AvgGenerationSeconds == nil || *m.AvgGenerationSeconds < 0 || m.AvgTimeToApprovalSeconds == nil || *m.AvgTimeToApprovalSeconds < *m.AvgGenerationSeconds {
		t.Fatalf("times: %v %v", m.AvgGenerationSeconds, m.AvgTimeToApprovalSeconds)
	}

	// Grouped by pipeline (recipe and step providers).
	if sum.GroupBy != "pipeline" || len(sum.Groups) != 2 {
		t.Fatalf("groups: %+v", sum.Groups)
	}
	if g := sum.Groups[0]; g.Label != "reference · fake" || g.Generations != 2 || g.Approved != 2 || g.Reviewed != 4 {
		t.Fatalf("reference group: %+v", g)
	}
	if g := sum.Groups[1]; g.Label != "tryon · fake" || g.Approved != 1 || g.Reviewed != 1 || g.Pending != 1 || *g.ApprovalRate != 1 {
		t.Fatalf("tryon group: %+v", g)
	}

	// The test set, the hardest prints, and the number of garment photographs.
	ts, _ := e.svc.MetricsSummary(ctx, MetricsFilter{TestSet: true, GroupBy: "printComplexity"})
	if ts.Total.Generations != 2 || ts.Total.Approved != 2 || len(ts.Groups) != 1 || ts.Groups[0].Label != "complex" {
		t.Fatalf("test set: %+v", ts)
	}
	refs, _ := e.svc.MetricsSummary(ctx, MetricsFilter{GroupBy: "garmentRefs", ProductID: e.product.ID})
	if len(refs.Groups) != 2 || refs.Groups[0].Label != "1 garment photo" || refs.Groups[1].Label != "2 garment photos" {
		t.Fatalf("garment refs: %+v", refs.Groups)
	}

	// Filters.
	for name, c := range map[string]struct {
		f    MetricsFilter
		gens int
	}{
		"recipe":         {MetricsFilter{Recipe: "tryon"}, 1},
		"provider":       {MetricsFilter{Provider: fake.Name}, 3},
		"other provider": {MetricsFilter{Provider: "fashn"}, 0},
		"product":        {MetricsFilter{ProductID: other.ID}, 1},
		"from today":     {MetricsFilter{From: time.Now().UTC().Truncate(24 * time.Hour)}, 3},
		"until":          {MetricsFilter{To: time.Now().UTC().AddDate(0, 0, -1)}, 0},
	} {
		got, err := e.svc.MetricsSummary(ctx, c.f)
		if err != nil || got.Total.Generations != c.gens {
			t.Errorf("%s: %d generations (want %d) %v", name, got.Total.Generations, c.gens, err)
		}
	}
	empty, _ := e.svc.MetricsSummary(ctx, MetricsFilter{Provider: "fashn"})
	if empty.Total.ApprovalRate != nil || empty.Total.CostPerApprovedMicros != nil || len(empty.Groups) != 0 {
		t.Fatalf("empty: %+v", empty)
	}

	var ve *ValidationError
	if _, err := e.svc.MetricsSummary(ctx, MetricsFilter{GroupBy: "colour"}); !errors.As(err, &ve) {
		t.Fatalf("bad grouping: %v", err)
	}
	if _, err := e.svc.MetricsSummary(ctx, MetricsFilter{From: time.Now(), To: time.Now().AddDate(0, 0, -2)}); !errors.As(err, &ve) {
		t.Fatalf("reversed range: %v", err)
	}
}

func TestProductEvaluationFields(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	p, err := e.cat.CreateProduct(ctx, catalog.ProductInput{Name: ptr("Paisley"), TestSet: ptr(true), PrintComplexity: ptr("complex")})
	if err != nil || !p.TestSet || p.PrintComplexity != "complex" {
		t.Fatalf("create: %+v %v", p, err)
	}
	p, err = e.cat.UpdateProduct(ctx, p.ID, catalog.ProductInput{TestSet: ptr(false)})
	if err != nil || p.TestSet || p.PrintComplexity != "complex" {
		t.Fatalf("update: %+v %v", p, err)
	}
	var ve *catalog.ValidationError
	if _, err := e.cat.UpdateProduct(ctx, p.ID, catalog.ProductInput{PrintComplexity: ptr("very")}); !errors.As(err, &ve) {
		t.Fatalf("bad complexity: %v", err)
	}
}
