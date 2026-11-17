-- The fidelity gate (PRD §9): products can be marked as part of the
-- evaluation test set and rated by how hard their print is to reproduce,
-- so metrics can be read for the test set and for the hardest prints.

ALTER TABLE product ADD COLUMN test_set INTEGER NOT NULL DEFAULT 0 CHECK (test_set IN (0, 1));

ALTER TABLE product ADD COLUMN print_complexity TEXT NOT NULL DEFAULT ''
    CHECK (print_complexity IN ('', 'none', 'simple', 'complex'));

-- Metrics filter generations by date and group images by review status.
CREATE INDEX generation_created ON generation (created_at);
