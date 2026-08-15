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

// ownerClause builds the SQL predicate matching pages owned by o, together
// with its arguments. Rows carrying an owner_subject match on it directly;
// rows predating that column, whose owner_subject is the empty string, fall
// back to their created_by label — which is why a non-empty email and login
// are matched too.
func ownerClause(o Owner) (string, []any) {
	args := []any{o.Subject}
	var legacy []any
	for _, v := range []string{o.Email, o.Login, o.Subject} {
		if v != "" {
			legacy = append(legacy, v)
		}
	}
	if len(legacy) == 0 {
		return "owner_subject = ?", args
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(legacy)), ",")
	args = append(args, legacy...)
	return fmt.Sprintf("(owner_subject = ? OR (owner_subject = '' AND created_by IN (%s)))",
		placeholders), args
}

func (s *sqliteStore) CreatePage(ctx context.Context, p Page) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pages (id, title, content, content_type, size_bytes, created_at, created_by, owner_subject)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Title, p.Content, p.ContentType, p.SizeBytes, p.CreatedAt.Unix(),
		p.CreatedBy, p.OwnerSubject)
	if err != nil {
		return fmt.Errorf("create page %s: %w", p.ID, err)
	}
	return nil
}

func (s *sqliteStore) GetPage(ctx context.Context, id string) (Page, error) {
	return s.getPage(ctx, `SELECT id, title, content, content_type, size_bytes, created_at,
		created_by, owner_subject FROM pages WHERE id = ?`, id)
}

func (s *sqliteStore) GetPageForOwner(ctx context.Context, id string, o Owner) (Page, error) {
	clause, args := ownerClause(o)
	query := `SELECT id, title, content, content_type, size_bytes, created_at,
		created_by, owner_subject FROM pages WHERE id = ? AND ` + clause
	return s.getPage(ctx, query, append([]any{id}, args...)...)
}

func (s *sqliteStore) getPage(ctx context.Context, query string, args ...any) (Page, error) {
	var p Page
	var createdAt int64
	err := s.db.QueryRowContext(ctx, query, args...).
		Scan(&p.ID, &p.Title, &p.Content, &p.ContentType, &p.SizeBytes, &createdAt,
			&p.CreatedBy, &p.OwnerSubject)
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("get page: %w", err)
	}
	p.CreatedAt = time.Unix(createdAt, 0).UTC()
	return p, nil
}

func (s *sqliteStore) ListPagesByOwner(ctx context.Context, o Owner) ([]Page, error) {
	clause, args := ownerClause(o)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, content_type, size_bytes, created_at, created_by, owner_subject
		 FROM pages WHERE `+clause+` ORDER BY created_at DESC, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		var createdAt int64
		if err := rows.Scan(&p.ID, &p.Title, &p.ContentType, &p.SizeBytes, &createdAt,
			&p.CreatedBy, &p.OwnerSubject); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		p.CreatedAt = time.Unix(createdAt, 0).UTC()
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

func (s *sqliteStore) DeletePageForOwner(ctx context.Context, id string, o Owner) error {
	clause, args := ownerClause(o)
	res, err := s.db.ExecContext(ctx, `DELETE FROM pages WHERE id = ? AND `+clause,
		append([]any{id}, args...)...)
	if err != nil {
		return fmt.Errorf("delete page %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) PrunePagesForOwner(ctx context.Context, cutoff time.Time, o Owner) (int64, error) {
	clause, args := ownerClause(o)
	res, err := s.db.ExecContext(ctx, `DELETE FROM pages WHERE created_at < ? AND `+clause,
		append([]any{cutoff.Unix()}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("prune pages: %w", err)
	}
	return res.RowsAffected()
}

// --- tokens ---

func (s *sqliteStore) CreateToken(ctx context.Context, t Token) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, name, token_hash, owner_subject, owner_login, owner_email,
			created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Hash, t.OwnerSubject, t.OwnerLogin, t.OwnerEmail,
		t.CreatedAt.Unix(), unixPtr(t.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create token %s: %w", t.ID, err)
	}
	return nil
}

func (s *sqliteStore) GetToken(ctx context.Context, id string) (Token, error) {
	rows, err := s.db.QueryContext(ctx, tokenSelect+` WHERE id = ?`, id)
	if err != nil {
		return Token{}, fmt.Errorf("get token %s: %w", id, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Token{}, fmt.Errorf("get token %s: %w", id, err)
		}
		return Token{}, ErrNotFound
	}
	t, err := scanToken(rows)
	if err != nil {
		return Token{}, fmt.Errorf("get token %s: %w", id, err)
	}
	return t, nil
}

func (s *sqliteStore) ListTokensByOwner(ctx context.Context, subject string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx,
		tokenSelect+` WHERE owner_subject = ? ORDER BY created_at DESC, id`, subject)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *sqliteStore) UpdateTokenHash(ctx context.Context, id, subject, hash string) error {
	// Clearing revoked_at and last_used_at makes a rotated token read as the
	// fresh credential it is, rather than inheriting the old one's history.
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET token_hash = ?, revoked_at = NULL, last_used_at = NULL
		 WHERE id = ? AND owner_subject = ?`, hash, id, subject)
	if err != nil {
		return fmt.Errorf("rotate token %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) RevokeToken(ctx context.Context, id, subject string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND owner_subject = ? AND revoked_at IS NULL`,
		time.Now().UTC().Unix(), id, subject)
	if err != nil {
		return fmt.Errorf("revoke token %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteToken(ctx context.Context, id, subject string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM api_tokens WHERE id = ? AND owner_subject = ?`, id, subject)
	if err != nil {
		return fmt.Errorf("delete token %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) TouchToken(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, at.Unix(), id)
	if err != nil {
		return fmt.Errorf("touch token %s: %w", id, err)
	}
	return nil
}

const tokenSelect = `SELECT id, name, token_hash, owner_subject, owner_login, owner_email,
	created_at, last_used_at, expires_at, revoked_at FROM api_tokens`

func scanToken(rows *sql.Rows) (Token, error) {
	var t Token
	var createdAt int64
	var lastUsed, expires, revoked sql.NullInt64
	if err := rows.Scan(&t.ID, &t.Name, &t.Hash, &t.OwnerSubject, &t.OwnerLogin, &t.OwnerEmail,
		&createdAt, &lastUsed, &expires, &revoked); err != nil {
		return Token{}, err
	}
	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	t.LastUsedAt = timePtr(lastUsed)
	t.ExpiresAt = timePtr(expires)
	t.RevokedAt = timePtr(revoked)
	return t, nil
}

func timePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

func unixPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func (s *sqliteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
