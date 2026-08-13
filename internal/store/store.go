// Package store persists report pages and CLI API tokens in SQLite.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a page or token id does not exist. Owner-scoped
// lookups also return it for rows owned by somebody else: callers turn this
// into a 404 so the API never reveals that an id exists.
var ErrNotFound = errors.New("not found")

type Page struct {
	ID          string
	Title       string
	Content     []byte
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
	// CreatedBy is a human-readable label (email, else login, else subject).
	CreatedBy string
	// OwnerSubject is the stable IdP subject that owns the page. Rows written
	// before the owner column existed have it empty; see Owner.
	OwnerSubject string
}

// Token is a CLI API token. Only the hash of the secret half is ever stored,
// so a token's plaintext exists exactly once, in the response that minted it.
type Token struct {
	ID           string
	Name         string
	Hash         string
	OwnerSubject string
	OwnerLogin   string
	OwnerEmail   string
	CreatedAt    time.Time
	// LastUsedAt, ExpiresAt and RevokedAt are nil when unset: never used, no
	// expiry, and not revoked respectively.
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

// Revoked reports whether the token has been revoked or has expired as of now.
func (t Token) Revoked(now time.Time) bool {
	if t.RevokedAt != nil {
		return true
	}
	return t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)
}

// Owner identifies the principal a page belongs to. Subject is authoritative.
// Email and Login exist only to match legacy rows, which predate the
// owner_subject column and carry nothing but a CreatedBy label; there is no
// reliable way to backfill those, so they are matched on that label instead.
type Owner struct {
	Subject string
	Email   string
	Login   string
}

// Store is the persistence interface used by the server.
type Store interface {
	// CreatePage inserts a page. The caller supplies the id; a duplicate id
	// returns an error satisfying IsDuplicateID.
	CreatePage(ctx context.Context, p Page) error
	// GetPage returns the full page including content, ignoring ownership.
	// Only page serving may use it: every API path is owner-scoped.
	GetPage(ctx context.Context, id string) (Page, error)
	// ListPagesByOwner returns metadata for the owner's pages, newest first.
	// Content is nil.
	ListPagesByOwner(ctx context.Context, o Owner) ([]Page, error)
	// GetPageForOwner returns the full page if o owns it, else ErrNotFound.
	GetPageForOwner(ctx context.Context, id string, o Owner) (Page, error)
	DeletePageForOwner(ctx context.Context, id string, o Owner) error
	// PrunePagesForOwner deletes the owner's pages created before the cutoff
	// and reports how many.
	PrunePagesForOwner(ctx context.Context, cutoff time.Time, o Owner) (int64, error)

	CreateToken(ctx context.Context, t Token) error
	// GetToken looks a token up by its public id half. It does not check
	// revocation or expiry; the validator does.
	GetToken(ctx context.Context, id string) (Token, error)
	ListTokensByOwner(ctx context.Context, subject string) ([]Token, error)
	// UpdateTokenHash replaces the stored hash, keeping the id and name. This
	// is a rotation: the previous secret stops working immediately.
	UpdateTokenHash(ctx context.Context, id, subject, hash string) error
	RevokeToken(ctx context.Context, id, subject string) error
	DeleteToken(ctx context.Context, id, subject string) error
	// TouchToken records that the token was used at the given time.
	TouchToken(ctx context.Context, id string, at time.Time) error

	// Ping verifies database connectivity (used by health checks).
	Ping(ctx context.Context) error
	Close() error
}
