CREATE TABLE api_tokens (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    token_hash    TEXT NOT NULL,
    owner_subject TEXT NOT NULL,
    owner_login   TEXT NOT NULL DEFAULT '',
    owner_email   TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER,
    expires_at    INTEGER,
    revoked_at    INTEGER
);

CREATE INDEX idx_api_tokens_owner ON api_tokens(owner_subject, created_at DESC);
