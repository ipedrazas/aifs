-- Photoshoots, generations and generated images.
--
-- Everything that produced an image is kept: generations, their steps and
-- inputs are never deleted, and they reference products, models and blobs
-- with ON DELETE RESTRICT. A used model is therefore archived instead of
-- deleted, and input photographs outlive the records they came from.

-- A reusable combination of product and model (presets and the full set of
-- photoshoot controls arrive in later phases).
CREATE TABLE photoshoot (
    id                  INTEGER PRIMARY KEY,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    product_id          INTEGER NOT NULL REFERENCES product (id) ON DELETE RESTRICT,
    model_id            INTEGER NOT NULL REFERENCES model (id) ON DELETE RESTRICT,
    aspect_ratio        TEXT NOT NULL DEFAULT '3:4'
                        CHECK (aspect_ratio IN ('1:1', '2:3', '3:4', '4:5', '9:16', '4:3', '16:9')),
    custom_instructions TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
) STRICT;

CREATE INDEX photoshoot_product ON photoshoot (product_id);
CREATE INDEX photoshoot_model ON photoshoot (model_id);

-- One request to produce N variations through a recipe. The job queue is
-- this table: queued and running rows are (re)started by the worker.
CREATE TABLE generation (
    id               INTEGER PRIMARY KEY,
    photoshoot_id    INTEGER NOT NULL REFERENCES photoshoot (id) ON DELETE RESTRICT,
    product_id       INTEGER NOT NULL REFERENCES product (id) ON DELETE RESTRICT,
    model_id         INTEGER NOT NULL REFERENCES model (id) ON DELETE RESTRICT,
    recipe           TEXT NOT NULL,
    provider         TEXT NOT NULL,
    variations       INTEGER NOT NULL CHECK (variations BETWEEN 1 AND 100),
    -- Complete JSON snapshot of what produced the images.
    settings         TEXT NOT NULL,
    prompt           TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'queued'
                     CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    error            TEXT NOT NULL DEFAULT '',
    cost_micros      INTEGER NOT NULL DEFAULT 0,
    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
    created_at       TEXT NOT NULL,
    started_at       TEXT,
    finished_at      TEXT
) STRICT;

CREATE INDEX generation_status ON generation (status);
CREATE INDEX generation_photoshoot ON generation (photoshoot_id);
CREATE INDEX generation_product ON generation (product_id);

-- The snapshot is the record of what happened; it never changes.
CREATE TRIGGER generation_snapshot_immutable
BEFORE UPDATE OF photoshoot_id, product_id, model_id, recipe, provider, variations, settings, prompt, created_at
ON generation
BEGIN
    SELECT RAISE(ABORT, 'a generation snapshot is immutable');
END;

-- Input photographs, by content hash. Keeps the bytes alive for traceability.
CREATE TABLE generation_input (
    generation_id INTEGER NOT NULL REFERENCES generation (id) ON DELETE RESTRICT,
    position      INTEGER NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('garment', 'model')),
    label         TEXT NOT NULL, -- product view or model image role
    blob_hash     TEXT NOT NULL REFERENCES blob (hash) ON DELETE RESTRICT,
    PRIMARY KEY (generation_id, position)
) STRICT, WITHOUT ROWID;

CREATE INDEX generation_input_blob ON generation_input (blob_hash);

-- One row per provider call: steps of the recipe for each variation. All
-- steps are planned when the generation is created.
CREATE TABLE generation_step (
    id               INTEGER PRIMARY KEY,
    generation_id    INTEGER NOT NULL REFERENCES generation (id) ON DELETE RESTRICT,
    variation        INTEGER NOT NULL CHECK (variation >= 1),
    position         INTEGER NOT NULL CHECK (position >= 1),
    capability       TEXT NOT NULL,
    provider         TEXT NOT NULL,
    provider_model   TEXT NOT NULL DEFAULT '',
    request          TEXT NOT NULL DEFAULT '{}', -- JSON: prompt, input hashes, seed
    provider_job_id  TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'submitted', 'succeeded', 'failed', 'cancelled')),
    attempts         INTEGER NOT NULL DEFAULT 0,
    error            TEXT NOT NULL DEFAULT '',
    cost_micros      INTEGER NOT NULL DEFAULT 0,
    output_blob_hash TEXT REFERENCES blob (hash) ON DELETE RESTRICT,
    started_at       TEXT,
    finished_at      TEXT,
    UNIQUE (generation_id, variation, position)
) STRICT;

-- Final images. At most one per variation, so a resumed generation can never
-- produce duplicates.
CREATE TABLE generated_image (
    id               INTEGER PRIMARY KEY,
    generation_id    INTEGER NOT NULL REFERENCES generation (id) ON DELETE RESTRICT,
    variation        INTEGER NOT NULL,
    blob_hash        TEXT NOT NULL REFERENCES blob (hash) ON DELETE RESTRICT,
    review_status    TEXT NOT NULL DEFAULT 'pending'
                     CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_refinement')),
    rejection_reason TEXT CHECK (rejection_reason IS NULL OR rejection_reason IN ('fidelity', 'photo_quality', 'other')),
    reviewer_notes   TEXT NOT NULL DEFAULT '',
    parent_image_id  INTEGER REFERENCES generated_image (id) ON DELETE RESTRICT,
    created_at       TEXT NOT NULL,
    UNIQUE (generation_id, variation)
) STRICT;

CREATE INDEX generated_image_status ON generated_image (review_status);

CREATE TRIGGER generated_image_bytes_immutable
BEFORE UPDATE OF generation_id, variation, blob_hash, parent_image_id, created_at ON generated_image
BEGIN
    SELECT RAISE(ABORT, 'a generated image is immutable');
END;
