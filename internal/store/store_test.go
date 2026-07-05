package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreLifecycle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	p := Page{
		ID: "abc123", Title: "t", Content: []byte("<html></html>"),
		ContentType: "text/html; charset=utf-8", SizeBytes: 13,
		CreatedAt: time.Now().UTC().Truncate(time.Second), CreatedBy: "me@example.org",
	}
	if err := s.CreatePage(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePage(ctx, p); !IsDuplicateID(err) {
		t.Fatalf("duplicate insert: got %v, want duplicate-id error", err)
	}

	got, err := s.GetPage(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Content) != "<html></html>" || got.CreatedBy != p.CreatedBy || !got.CreatedAt.Equal(p.CreatedAt) {
		t.Fatalf("get mismatch: %+v", got)
	}
	if _, err := s.GetPage(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing page: got %v", err)
	}

	list, err := s.ListPages(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, %d entries", err, len(list))
	}
	if list[0].Content != nil {
		t.Fatal("list must not include content")
	}

	n, err := s.PrunePages(ctx, time.Now().Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("prune: %v, deleted %d", err, n)
	}
	if err := s.DeletePage(ctx, "abc123"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete after prune: got %v, want ErrNotFound", err)
	}
}
