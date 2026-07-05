CREATE TABLE pages (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL DEFAULT '',
    content      BLOB NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text/html; charset=utf-8',
    size_bytes   INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    created_by   TEXT NOT NULL
);

CREATE INDEX idx_pages_created_at ON pages(created_at);
