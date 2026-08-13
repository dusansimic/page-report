ALTER TABLE pages ADD COLUMN owner_subject TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_pages_owner ON pages(owner_subject, created_at DESC);
