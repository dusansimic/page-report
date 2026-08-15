package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// seedV1 builds a database at schema version 1 — the shape every deployment
// predating the dashboard is running — so migrations can be exercised against
// real legacy data rather than only against an empty file. The schema and the
// golang-migrate bookkeeping table are written by hand on purpose: reusing the
// migration runner here would test nothing.
func seedV1(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE pages (
			id           TEXT PRIMARY KEY,
			title        TEXT NOT NULL DEFAULT '',
			content      BLOB NOT NULL,
			content_type TEXT NOT NULL DEFAULT 'text/html; charset=utf-8',
			size_bytes   INTEGER NOT NULL,
			created_at   INTEGER NOT NULL,
			created_by   TEXT NOT NULL
		)`,
		`CREATE INDEX idx_pages_created_at ON pages(created_at)`,
		`CREATE TABLE schema_migrations (version uint64, dirty bool)`,
		`INSERT INTO schema_migrations (version, dirty) VALUES (1, false)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed v1 (%s): %v", s, err)
		}
	}

	if _, err := db.Exec(
		`INSERT INTO pages (id, title, content, content_type, size_bytes, created_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"old1", "legacy report", []byte("<html></html>"), "text/html; charset=utf-8", 13,
		time.Now().UTC().Unix(), alice.Email); err != nil {
		t.Fatalf("seed v1 row: %v", err)
	}
}
