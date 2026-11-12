-- Virtual models: reusable people a garment is photographed on.
--
-- A model that something refers to (a generation, a photoshoot) cannot be
-- deleted: those tables reference it with ON DELETE RESTRICT, and the
-- application archives it instead, so past results stay traceable.
CREATE TABLE model (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description     TEXT NOT NULL DEFAULT '', -- descriptive profile
    appearance      TEXT NOT NULL DEFAULT '', -- appearance attributes
    preferred_poses TEXT NOT NULL DEFAULT '',
    styling         TEXT NOT NULL DEFAULT '',
    origin          TEXT NOT NULL DEFAULT 'uploaded' CHECK (origin IN ('uploaded', 'generated')),
    archived_at     TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
) STRICT;

-- Reference images of a model. Write-once, like product images.
CREATE TABLE model_image (
    id         INTEGER PRIMARY KEY,
    model_id   INTEGER NOT NULL REFERENCES model (id) ON DELETE CASCADE,
    blob_hash  TEXT NOT NULL REFERENCES blob (hash) ON DELETE RESTRICT,
    role       TEXT NOT NULL CHECK (role IN ('identity', 'full_body')),
    filename   TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (model_id, blob_hash)
) STRICT;

CREATE INDEX model_image_blob ON model_image (blob_hash);

CREATE TRIGGER model_image_immutable BEFORE UPDATE ON model_image
BEGIN
    SELECT RAISE(ABORT, 'model images are immutable');
END;
