-- Login sessions. The cookie carries a random token; only its SHA-256 is
-- stored, so a copy of the database cannot be used to hijack a session.
CREATE TABLE session (
    token_hash BLOB PRIMARY KEY,
    created_at INTEGER NOT NULL, -- unix seconds
    expires_at INTEGER NOT NULL  -- unix seconds
) STRICT, WITHOUT ROWID;

CREATE INDEX session_expires_at ON session (expires_at);
