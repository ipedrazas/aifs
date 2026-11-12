package studio

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func generatedImages(t *testing.T, n int) (*env, []GeneratedImage) {
	t.Helper()
	e := newEnv(t, 0, "")
	e.start()
	g := e.waitStatus(e.generate("reference", n).ID, StatusSucceeded)
	return e, g.Images
}

func TestReviewDecisionsAndHistory(t *testing.T) {
	ctx := context.Background()
	e, images := generatedImages(t, 1)
	im := images[0]
	if im.ReviewStatus != "pending" || im.ReviewedAt != nil {
		t.Fatalf("new image: %+v", im)
	}

	d, err := e.svc.ReviewImage(ctx, im.ID, ReviewInput{Status: "rejected", Reason: "fidelity", Notes: " pocket stitching is wrong "})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReviewStatus != "rejected" || d.RejectionReason != "fidelity" || d.ReviewerNotes != "pocket stitching is wrong" || d.ReviewedAt == nil {
		t.Fatalf("rejected: %+v", d.GeneratedImage)
	}

	// Decisions can change; every one is kept with its time.
	if _, err := e.svc.ReviewImage(ctx, im.ID, ReviewInput{Status: "needs_refinement"}); err != nil {
		t.Fatal(err)
	}
	d, err = e.svc.ReviewImage(ctx, im.ID, ReviewInput{Status: "approved", Notes: "fixed after a closer look"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReviewStatus != "approved" || d.RejectionReason != "" {
		t.Fatalf("approved: %+v", d.GeneratedImage)
	}
	if len(d.ReviewHistory) != 3 {
		t.Fatalf("history: %+v", d.ReviewHistory)
	}
	want := []string{"rejected", "needs_refinement", "approved"}
	for i, ev := range d.ReviewHistory {
		if ev.Status != want[i] || ev.CreatedAt.IsZero() {
			t.Fatalf("event %d: %+v", i, ev)
		}
	}
	if d.ReviewHistory[0].RejectionReason != "fidelity" || d.ReviewHistory[2].Notes != "fixed after a closer look" {
		t.Fatalf("history detail: %+v", d.ReviewHistory)
	}

	// The gallery filter follows the current decision.
	approved, _ := e.svc.ListImages(ctx, ImageFilter{ReviewStatus: "approved"})
	pending, _ := e.svc.ListImages(ctx, ImageFilter{ReviewStatus: "pending"})
	if len(approved) != 1 || len(pending) != 0 {
		t.Fatalf("filters: approved=%d pending=%d", len(approved), len(pending))
	}
}

func TestReviewValidation(t *testing.T) {
	ctx := context.Background()
	e, images := generatedImages(t, 1)
	id := images[0].ID
	var ve *ValidationError
	for name, in := range map[string]ReviewInput{
		"no reason":         {Status: "rejected"},
		"bad reason":        {Status: "rejected", Reason: "ugly"},
		"reason on approve": {Status: "approved", Reason: "fidelity"},
		"back to pending":   {Status: "pending"},
		"unknown status":    {Status: "maybe"},
		"long notes":        {Status: "approved", Notes: strings.Repeat("x", 2001)},
	} {
		if _, err := e.svc.ReviewImage(ctx, id, in); !errors.As(err, &ve) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.svc.ReviewImage(ctx, 999, ReviewInput{Status: "approved"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing image: %v", err)
	}
	d, _ := e.svc.GetImage(ctx, id)
	if d.ReviewStatus != "pending" || len(d.ReviewHistory) != 0 {
		t.Fatalf("invalid reviews changed the image: %+v", d)
	}
}

// The database itself guarantees that only a recorded human decision can
// change the review status.
func TestNoUnrecordedApproval(t *testing.T) {
	ctx := context.Background()
	e, images := generatedImages(t, 1)
	id := images[0].ID
	mustFail := func(what, q string, args ...any) {
		t.Helper()
		if _, err := e.db.Write.ExecContext(ctx, q, args...); err == nil {
			t.Errorf("%s: allowed", what)
		}
	}

	mustFail("approve without an event", `UPDATE generated_image SET review_status = 'approved' WHERE id = ?`, id)

	var gen int64
	var hash string
	_ = e.db.Read.QueryRow(`SELECT generation_id, blob_hash FROM generated_image WHERE id = ?`, id).Scan(&gen, &hash)
	mustFail("insert pre-approved",
		`INSERT INTO generated_image (generation_id, variation, blob_hash, review_status, created_at) VALUES (?, 99, ?, 'approved', 'now')`, gen, hash)

	if _, err := e.svc.ReviewImage(ctx, id, ReviewInput{Status: "rejected", Reason: "other"}); err != nil {
		t.Fatal(err)
	}
	// An event for a different decision does not authorise this one.
	mustFail("approve against a rejection event", `UPDATE generated_image SET review_status = 'approved', rejection_reason = NULL WHERE id = ?`, id)
	mustFail("rewrite history", `UPDATE review_event SET status = 'approved' WHERE generated_image_id = ?`, id)
	mustFail("delete history", `DELETE FROM review_event WHERE generated_image_id = ?`, id)
	mustFail("rejection without reason",
		`INSERT INTO review_event (generated_image_id, status, created_at) VALUES (?, 'rejected', 'now')`, id)

	d, _ := e.svc.GetImage(ctx, id)
	if d.ReviewStatus != "rejected" || len(d.ReviewHistory) != 1 {
		t.Fatalf("state changed: %+v", d)
	}
}
