-- Photoshoot presets: the scene (environment, pose, framing, lighting,
-- style) a generation is asked for.
--
-- Presets never describe the garment: the reference photographs define it.
-- Built-in presets ship with the application and are read-only; custom
-- presets arrive in a later phase.

CREATE TABLE preset (
    id              INTEGER PRIMARY KEY,
    -- Stable identifier of a built-in preset; NULL for custom presets.
    slug            TEXT UNIQUE,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description     TEXT NOT NULL DEFAULT '',
    environment     TEXT NOT NULL DEFAULT '',
    pose            TEXT NOT NULL DEFAULT '',
    framing         TEXT NOT NULL DEFAULT '',
    lighting        TEXT NOT NULL DEFAULT '',
    style           TEXT NOT NULL DEFAULT '',
    -- Free text appended to the prompt.
    prompt_fragment TEXT NOT NULL DEFAULT '',
    built_in        INTEGER NOT NULL DEFAULT 0 CHECK (built_in IN (0, 1)),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    CHECK ((built_in = 1) = (slug IS NOT NULL))
) STRICT;

CREATE TRIGGER preset_built_in_no_update BEFORE UPDATE ON preset WHEN OLD.built_in = 1
BEGIN
    SELECT RAISE(ABORT, 'built-in presets are read-only');
END;

CREATE TRIGGER preset_built_in_no_delete BEFORE DELETE ON preset WHEN OLD.built_in = 1
BEGIN
    SELECT RAISE(ABORT, 'built-in presets are read-only');
END;

INSERT INTO preset (slug, name, description, environment, pose, framing, lighting, style, built_in, created_at, updated_at) VALUES
(
    'studio', 'Clean studio catalogue',
    'Plain seamless backdrop for product listings.',
    'Plain light-grey seamless studio backdrop with no props.',
    'Natural standing pose facing the camera, weight on one leg, arms relaxed at the sides.',
    'Full body from head to toe, centred, with a little space above the head and below the feet.',
    'Soft, even, high-key studio lighting with gentle shadows.',
    'Clean e-commerce catalogue photography, true-to-life colour, sharp focus throughout.',
    1, '2026-11-17T00:00:00Z', '2026-11-17T00:00:00Z'
),
(
    'urban', 'Urban lifestyle',
    'City street, editorial street-style look.',
    'City street with modern architecture, concrete and glass, softly out of focus behind the model.',
    'Confident mid-stride walking pose, or leaning casually against a wall.',
    'Full body, camera slightly below eye level, the whole outfit visible.',
    'Natural late-afternoon daylight with soft directional sun.',
    'Editorial street-style photography, shallow depth of field, natural colour.',
    1, '2026-11-17T00:00:00Z', '2026-11-17T00:00:00Z'
),
(
    'coffee-shop', 'Coffee shop',
    'Warm, cosy interior lifestyle shot.',
    'Warm, modern coffee shop interior with wooden furniture and green plants, softly out of focus.',
    'Relaxed standing pose beside a table or the counter, candid natural expression.',
    'Full body, the whole outfit visible, model in sharp focus.',
    'Warm ambient interior light mixed with soft daylight from a large window.',
    'Lifestyle photography, cosy and natural, gentle warm tones.',
    1, '2026-11-17T00:00:00Z', '2026-11-17T00:00:00Z'
),
(
    'outdoor', 'Outdoor',
    'Natural landscape in golden-hour light.',
    'Open green park or meadow with trees in the distance, softly out of focus.',
    'Relaxed natural standing or slow walking pose.',
    'Full body from head to toe, the whole outfit visible.',
    'Soft natural golden-hour sunlight with a gentle rim light.',
    'Outdoor lifestyle photography, fresh and airy, natural colour.',
    1, '2026-11-17T00:00:00Z', '2026-11-17T00:00:00Z'
);

-- The preset a photoshoot uses by default; a generation may choose another.
ALTER TABLE photoshoot ADD COLUMN preset_id INTEGER REFERENCES preset (id) ON DELETE RESTRICT;

UPDATE photoshoot SET preset_id = (SELECT id FROM preset WHERE slug = 'studio');
