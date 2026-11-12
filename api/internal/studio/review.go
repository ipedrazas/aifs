package studio

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// Review decisions and rejection reasons.
var (
	ReviewDecisions  = []string{"approved", "rejected", "needs_refinement"}
	RejectionReasons = []string{"fidelity", "photo_quality", "other"}
)

// ReviewInput is a human review decision.
type ReviewInput struct {
	Status string `json:"status"`
	// Reason is required when rejecting, and must be empty otherwise.
	Reason string `json:"reason"`
	Notes  string `json:"notes"`
}

// ReviewEvent is one recorded decision.
type ReviewEvent struct {
	ID              int64     `json:"id"`
	Status          string    `json:"status"`
	RejectionReason string    `json:"rejectionReason"`
	Notes           string    `json:"notes"`
	CreatedAt       time.Time `json:"createdAt"`
}

const maxNotes = 2000

// ReviewImage records a human decision on a generated image. This is the
// only way an image's review status changes: the database refuses a status
// change that is not backed by a review event. A decision can be changed by
// reviewing again; every decision is kept.
func (s *Service) ReviewImage(ctx context.Context, id int64, in ReviewInput) (ImageDetail, error) {
	in.Status = strings.TrimSpace(in.Status)
	in.Reason = strings.TrimSpace(in.Reason)
	in.Notes = strings.TrimSpace(in.Notes)
	errs := map[string]string{}
	if !contains(ReviewDecisions, in.Status) {
		errs["status"] = "must be one of " + strings.Join(ReviewDecisions, ", ")
	}
	switch {
	case in.Status == "rejected" && !contains(RejectionReasons, in.Reason):
		errs["reason"] = "a rejection needs a reason: " + strings.Join(RejectionReasons, ", ")
	case in.Status != "rejected" && in.Reason != "":
		errs["reason"] = "only a rejection has a reason"
	}
	if utf8.RuneCountInString(in.Notes) > maxNotes {
		errs["notes"] = "must be at most 2000 characters"
	}
	if len(errs) > 0 {
		return ImageDetail{}, &ValidationError{Fields: errs}
	}

	var reason any
	if in.Reason != "" {
		reason = in.Reason
	}
	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return ImageDetail{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM generated_image WHERE id = ?`, id).Scan(&exists); err != nil {
		return ImageDetail{}, err
	}
	if exists == 0 {
		return ImageDetail{}, ErrNotFound
	}
	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO review_event (generated_image_id, status, rejection_reason, notes, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, in.Status, reason, in.Notes, now); err != nil {
		return ImageDetail{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE generated_image SET review_status = ?, rejection_reason = ?, reviewer_notes = ?, reviewed_at = ? WHERE id = ?`,
		in.Status, reason, in.Notes, now, id); err != nil {
		return ImageDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return ImageDetail{}, err
	}
	s.log.InfoContext(ctx, "image reviewed", "image", id, "status", in.Status, "reason", in.Reason)
	return s.GetImage(ctx, id)
}

func (s *Service) reviewHistory(ctx context.Context, imageID int64) ([]ReviewEvent, error) {
	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT id, status, COALESCE(rejection_reason, ''), notes, created_at
		FROM review_event WHERE generated_image_id = ? ORDER BY id`, imageID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ReviewEvent{}
	for rows.Next() {
		var e ReviewEvent
		var created string
		if err := rows.Scan(&e.ID, &e.Status, &e.RejectionReason, &e.Notes, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}
