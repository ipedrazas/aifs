-- Processed versions of reference photographs (background removed, trimmed,
-- resized). A processed version is a new, write-once product_image linked to
-- its original, which is kept. Generation uses the processed version in place
-- of its original. An original has at most one processed version; deleting
-- the original deletes its processed version too.
ALTER TABLE product_image ADD COLUMN source_image_id INTEGER REFERENCES product_image (id) ON DELETE CASCADE;

-- JSON record of the steps, provider, model and cost ('' for originals).
ALTER TABLE product_image ADD COLUMN processing TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX product_image_source ON product_image (source_image_id) WHERE source_image_id IS NOT NULL;
