package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, applies
// pending migrations, and returns a Store.
func Open(path string) (Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// A single connection sidesteps SQLITE_BUSY between concurrent writers.
	db.SetMaxOpenConns(1)

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

// IsDuplicateID reports whether err came from inserting an already-used id.
func IsDuplicateID(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: pages.id")
}

func (s *sqliteStore) CreatePage(ctx context.Context, p Page) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pages (id, title, content, content_type, size_bytes, created_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Title, p.Content, p.ContentType, p.SizeBytes, p.CreatedAt.Unix(), p.CreatedBy)
	if err != nil {
		return fmt.Errorf("create page %s: %w", p.ID, err)
	}
	return nil
}

func (s *sqliteStore) GetPage(ctx context.Context, id string) (Page, error) {
	var p Page
	var createdAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, content_type, size_bytes, created_at, created_by
		 FROM pages WHERE id = ?`, id).
		Scan(&p.ID, &p.Title, &p.Content, &p.ContentType, &p.SizeBytes, &createdAt, &p.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("get page %s: %w", id, err)
	}
	p.CreatedAt = time.Unix(createdAt, 0).UTC()
	return p, nil
}

func (s *sqliteStore) ListPages(ctx context.Context) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, content_type, size_bytes, created_at, created_by
		 FROM pages ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		var createdAt int64
		if err := rows.Scan(&p.ID, &p.Title, &p.ContentType, &p.SizeBytes, &createdAt, &p.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		p.CreatedAt = time.Unix(createdAt, 0).UTC()
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

func (s *sqliteStore) DeletePage(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete page %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) PrunePages(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pages WHERE created_at < ?`, cutoff.Unix())
	if err != nil {
		return 0, fmt.Errorf("prune pages: %w", err)
	}
	return res.RowsAffected()
}

func (s *sqliteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
