-- Human review of generated images.
--
-- Every decision is an append-only review_event. generated_image keeps the
-- current decision for filtering, and the database refuses to change it
-- unless the change was first recorded as an event: no code path can approve
-- an image without a recorded human decision.

ALTER TABLE generated_image ADD COLUMN reviewed_at TEXT;

CREATE TABLE review_event (
    id                 INTEGER PRIMARY KEY,
    generated_image_id INTEGER NOT NULL REFERENCES generated_image (id) ON DELETE RESTRICT,
    status             TEXT NOT NULL CHECK (status IN ('approved', 'rejected', 'needs_refinement')),
    rejection_reason   TEXT CHECK (rejection_reason IS NULL OR rejection_reason IN ('fidelity', 'photo_quality', 'other')),
    notes              TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    -- A rejection needs a reason; other decisions have none.
    CHECK ((status = 'rejected') = (rejection_reason IS NOT NULL))
) STRICT;

CREATE INDEX review_event_image ON review_event (generated_image_id, id);

CREATE TRIGGER review_event_no_update BEFORE UPDATE ON review_event
BEGIN
    SELECT RAISE(ABORT, 'review history is append-only');
END;

CREATE TRIGGER review_event_no_delete BEFORE DELETE ON review_event
BEGIN
    SELECT RAISE(ABORT, 'review history is append-only');
END;

-- A new image always starts unreviewed.
CREATE TRIGGER generated_image_starts_pending BEFORE INSERT ON generated_image
WHEN NEW.review_status <> 'pending' OR NEW.rejection_reason IS NOT NULL OR NEW.reviewed_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'a generated image must start as pending');
END;

-- The current decision must match the latest recorded review event.
CREATE TRIGGER generated_image_review_needs_event
BEFORE UPDATE OF review_status, rejection_reason ON generated_image
WHEN NOT EXISTS (
    SELECT 1 FROM review_event e
    WHERE e.id = (SELECT max(id) FROM review_event WHERE generated_image_id = NEW.id)
      AND e.status = NEW.review_status
      AND e.rejection_reason IS NEW.rejection_reason
)
BEGIN
    SELECT RAISE(ABORT, 'a review decision must be recorded as a review event');
END;
