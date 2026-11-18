DROP INDEX idx_pages_owner_session;
ALTER TABLE pages DROP COLUMN session_title;
ALTER TABLE pages DROP COLUMN session_id;
ALTER TABLE pages DROP COLUMN session_source;
