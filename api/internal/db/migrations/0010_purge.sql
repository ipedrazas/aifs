-- Deliberate deletion of a photoshoot and everything it produced.
--
-- Review history stays append-only for every other code path: deleting a
-- review event is refused unless the deleting transaction first inserted the
-- single row of purge_guard (and it removes it again before committing).
-- Other tables are protected by ON DELETE RESTRICT, which the purge satisfies
-- by deleting children before parents.
CREATE TABLE purge_guard (
    id INTEGER PRIMARY KEY CHECK (id = 1)
) STRICT;

DROP TRIGGER review_event_no_delete;

CREATE TRIGGER review_event_no_delete BEFORE DELETE ON review_event
WHEN NOT EXISTS (SELECT 1 FROM purge_guard)
BEGIN
    SELECT RAISE(ABORT, 'review history is append-only');
END;
