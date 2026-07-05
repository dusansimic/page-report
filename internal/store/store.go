// Package store persists report pages in SQLite.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a page id does not exist.
var ErrNotFound = errors.New("page not found")

type Page struct {
	ID          string
	Title       string
	Content     []byte
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
	CreatedBy   string
}

// Store is the persistence interface used by the server.
type Store interface {
	// CreatePage inserts a page. The caller supplies the id; a duplicate id
	// returns an error satisfying IsDuplicateID.
	CreatePage(ctx context.Context, p Page) error
	// GetPage returns the full page including content.
	GetPage(ctx context.Context, id string) (Page, error)
	// ListPages returns metadata for all pages, newest first. Content is nil.
	ListPages(ctx context.Context) ([]Page, error)
	DeletePage(ctx context.Context, id string) error
	// PrunePages deletes pages created before the cutoff and reports how many.
	PrunePages(ctx context.Context, cutoff time.Time) (int64, error)
	// Ping verifies database connectivity (used by health checks).
	Ping(ctx context.Context) error
	Close() error
}
