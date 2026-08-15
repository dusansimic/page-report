package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var alice = Owner{Subject: "sub-alice", Email: "alice@example.org", Login: "alice"}
var bob = Owner{Subject: "sub-bob", Email: "bob@example.org", Login: "bob"}

func TestSQLiteStoreLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := Page{
		ID: "abc123", Title: "t", Content: []byte("<html></html>"),
		ContentType: "text/html; charset=utf-8", SizeBytes: 13,
		CreatedAt: time.Now().UTC().Truncate(time.Second), CreatedBy: alice.Email,
		OwnerSubject: alice.Subject,
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
	if string(got.Content) != "<html></html>" || got.CreatedBy != p.CreatedBy ||
		!got.CreatedAt.Equal(p.CreatedAt) || got.OwnerSubject != alice.Subject {
		t.Fatalf("get mismatch: %+v", got)
	}
	if _, err := s.GetPage(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing page: got %v", err)
	}

	list, err := s.ListPagesByOwner(ctx, alice)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, %d entries", err, len(list))
	}
	if list[0].Content != nil {
		t.Fatal("list must not include content")
	}

	n, err := s.PrunePagesForOwner(ctx, time.Now().Add(time.Hour), alice)
	if err != nil || n != 1 {
		t.Fatalf("prune: %v, deleted %d", err, n)
	}
	if err := s.DeletePageForOwner(ctx, "abc123", alice); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete after prune: got %v, want ErrNotFound", err)
	}
}

// Ownership is the whole point of the dashboard: one user must never see or
// mutate another's pages through any owner-scoped path.
func TestPageOwnerIsolation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mk := func(id string, o Owner) {
		if err := s.CreatePage(ctx, Page{
			ID: id, Content: []byte("x"), ContentType: "text/html; charset=utf-8", SizeBytes: 1,
			CreatedAt: now, CreatedBy: o.Email, OwnerSubject: o.Subject,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("alice-1", alice)
	mk("bob-1", bob)

	list, err := s.ListPagesByOwner(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "alice-1" {
		t.Fatalf("alice sees %+v, want only alice-1", list)
	}

	if _, err := s.GetPageForOwner(ctx, "bob-1", alice); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner get: got %v, want ErrNotFound", err)
	}
	if err := s.DeletePageForOwner(ctx, "bob-1", alice); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner delete: got %v, want ErrNotFound", err)
	}
	n, err := s.PrunePagesForOwner(ctx, now.Add(time.Hour), alice)
	if err != nil || n != 1 {
		t.Fatalf("prune scoped to alice: %v, deleted %d, want 1", err, n)
	}
	if _, err := s.GetPage(ctx, "bob-1"); err != nil {
		t.Errorf("bob's page must survive alice's prune: %v", err)
	}
}

// Pages written before migration 000003 have no owner_subject, only a
// created_by label. They must remain reachable by the user that label names.
func TestLegacyPagesMatchOnCreatedBy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	legacy := Page{
		ID: "legacy-1", Content: []byte("x"), ContentType: "text/html; charset=utf-8", SizeBytes: 1,
		CreatedAt: now, CreatedBy: alice.Email, OwnerSubject: "",
	}
	if err := s.CreatePage(ctx, legacy); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListPagesByOwner(ctx, alice)
	if err != nil || len(list) != 1 {
		t.Fatalf("legacy page not visible to its creator: %v, %+v", err, list)
	}
	if _, err := s.GetPageForOwner(ctx, "legacy-1", alice); err != nil {
		t.Errorf("legacy get: %v", err)
	}
	if _, err := s.GetPageForOwner(ctx, "legacy-1", bob); !errors.Is(err, ErrNotFound) {
		t.Errorf("legacy page leaked to another user: %v", err)
	}
}

func TestTokenLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tok := Token{
		ID: "tok1", Name: "laptop", Hash: "hash-1", OwnerSubject: alice.Subject,
		OwnerLogin: alice.Login, OwnerEmail: alice.Email, CreatedAt: now,
	}
	if err := s.CreateToken(ctx, tok); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetToken(ctx, "tok1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash != "hash-1" || got.Name != "laptop" || got.OwnerEmail != alice.Email {
		t.Fatalf("token mismatch: %+v", got)
	}
	if got.LastUsedAt != nil || got.ExpiresAt != nil || got.RevokedAt != nil {
		t.Fatalf("unset timestamps must be nil: %+v", got)
	}
	if got.Revoked(now) {
		t.Error("fresh token must not read as revoked")
	}

	if _, err := s.GetToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing token: got %v", err)
	}

	used := now.Add(time.Minute)
	if err := s.TouchToken(ctx, "tok1", used); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetToken(ctx, "tok1")
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Fatalf("last_used_at = %v, want %v", got.LastUsedAt, used)
	}

	// Rotation swaps the secret and clears the old usage history.
	if err := s.UpdateTokenHash(ctx, "tok1", alice.Subject, "hash-2"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetToken(ctx, "tok1")
	if got.Hash != "hash-2" || got.LastUsedAt != nil {
		t.Fatalf("after rotate: %+v", got)
	}

	if err := s.RevokeToken(ctx, "tok1", alice.Subject); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetToken(ctx, "tok1")
	if got.RevokedAt == nil || !got.Revoked(time.Now()) {
		t.Fatalf("revoke did not stick: %+v", got)
	}
	// Revoking twice is not an error the caller can act on, but it must not
	// silently claim success on a row it did not change.
	if err := s.RevokeToken(ctx, "tok1", alice.Subject); !errors.Is(err, ErrNotFound) {
		t.Errorf("double revoke: got %v", err)
	}

	if err := s.DeleteToken(ctx, "tok1", alice.Subject); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetToken(ctx, "tok1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token survived delete: %v", err)
	}
}

func TestTokenExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Hour)

	if err := s.CreateToken(ctx, Token{
		ID: "expired", Hash: "h", OwnerSubject: alice.Subject,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: &past,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetToken(ctx, "expired")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(past) {
		t.Fatalf("expires_at round trip: %+v", got.ExpiresAt)
	}
	if !got.Revoked(now) {
		t.Error("expired token must read as revoked")
	}
}

func TestTokenOwnerIsolation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.CreateToken(ctx, Token{
		ID: "bobs", Hash: "h", OwnerSubject: bob.Subject, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListTokensByOwner(ctx, alice.Subject)
	if err != nil || len(list) != 0 {
		t.Fatalf("alice sees bob's tokens: %v, %+v", err, list)
	}
	if err := s.UpdateTokenHash(ctx, "bobs", alice.Subject, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner rotate: got %v", err)
	}
	if err := s.RevokeToken(ctx, "bobs", alice.Subject); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner revoke: got %v", err)
	}
	if err := s.DeleteToken(ctx, "bobs", alice.Subject); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner delete: got %v", err)
	}
}

// Migrations must apply cleanly onto a database created at schema version 1,
// which is what every existing deployment is running.
func TestMigrateFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	seedV1(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrating a v1 database: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	p, err := s.GetPage(ctx, "old1")
	if err != nil {
		t.Fatal(err)
	}
	if p.OwnerSubject != "" {
		t.Errorf("migrated row must have empty owner_subject, got %q", p.OwnerSubject)
	}
	list, err := s.ListPagesByOwner(ctx, alice)
	if err != nil || len(list) != 1 {
		t.Fatalf("migrated row unreachable by its creator: %v, %+v", err, list)
	}
}
