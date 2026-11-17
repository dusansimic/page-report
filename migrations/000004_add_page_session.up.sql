ALTER TABLE pages ADD COLUMN session_source TEXT NOT NULL DEFAULT '';
ALTER TABLE pages ADD COLUMN session_id     TEXT NOT NULL DEFAULT '';
ALTER TABLE pages ADD COLUMN session_title  TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_pages_owner_session
  ON pages(owner_subject, session_source, session_id, created_at DESC);
