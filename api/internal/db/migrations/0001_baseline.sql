-- Baseline schema. Domain tables arrive with the phases that need them.

-- Installation-level key/value settings (e.g. installation id).
CREATE TABLE app_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

INSERT INTO app_meta (key, value) VALUES ('installation_id', lower(hex(randomblob(16))));
