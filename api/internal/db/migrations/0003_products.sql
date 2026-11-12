-- Content-addressed blobs (image bytes live in the blob store; this is the
-- registry and metadata). Rows are immutable. Tables that reference a blob
-- use ON DELETE RESTRICT, so a blob row can only be removed once nothing
-- refers to it.
CREATE TABLE blob (
    hash         TEXT PRIMARY KEY CHECK (length(hash) = 64),
    namespace    TEXT NOT NULL CHECK (namespace IN ('original', 'generated')),
    content_type TEXT NOT NULL,
    size         INTEGER NOT NULL CHECK (size > 0),
    width        INTEGER NOT NULL CHECK (width > 0),  -- displayed, after EXIF orientation
    height       INTEGER NOT NULL CHECK (height > 0),
    orientation  INTEGER NOT NULL DEFAULT 1 CHECK (orientation BETWEEN 1 AND 8),
    created_at   TEXT NOT NULL
) STRICT, WITHOUT ROWID;

CREATE TRIGGER blob_immutable BEFORE UPDATE ON blob
BEGIN
    SELECT RAISE(ABORT, 'blob rows are immutable');
END;

CREATE TABLE product (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    sku           TEXT COLLATE NOCASE UNIQUE CHECK (sku IS NULL OR length(sku) BETWEEN 1 AND 64),
    category      TEXT NOT NULL DEFAULT 'jeans',
    description   TEXT NOT NULL DEFAULT '',
    colour        TEXT NOT NULL DEFAULT '',
    print_details TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
) STRICT;

-- Reference photographs. Write-once: rows can be inserted and deleted, never
-- updated, and the bytes they point to are immutable.
CREATE TABLE product_image (
    id         INTEGER PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES product (id) ON DELETE CASCADE,
    blob_hash  TEXT NOT NULL REFERENCES blob (hash) ON DELETE RESTRICT,
    view       TEXT NOT NULL CHECK (view IN ('front', 'back', 'side', 'detail', 'print')),
    filename   TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (product_id, blob_hash)
) STRICT;

CREATE INDEX product_image_blob ON product_image (blob_hash);

CREATE TRIGGER product_image_immutable BEFORE UPDATE ON product_image
BEGIN
    SELECT RAISE(ABORT, 'product images are immutable');
END;
