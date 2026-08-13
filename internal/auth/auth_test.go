package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dusan/page-report/internal/config"
)

func TestAllowlistMatch(t *testing.T) {
	allow := NewAllowlist([]string{"Me@Example.org", "octocat", " spaced@example.org "})
	cases := []struct {
		name string
		id   Identity
		want bool
	}{
		{"email exact", Identity{Email: "me@example.org"}, true},
		{"email case-insensitive", Identity{Email: "ME@EXAMPLE.ORG"}, true},
		{"login match", Identity{Login: "OctoCat"}, true},
		{"trimmed entry", Identity{Email: "spaced@example.org"}, true},
		{"email miss, login hit", Identity{Email: "other@example.org", Login: "octocat"}, true},
		{"no match", Identity{Email: "nope@example.org", Login: "nobody"}, false},
		{"empty identity", Identity{}, false},
	}
	for _, c := range cases {
		if got := allow.Match(c.id); got != c.want {
			t.Errorf("%s: Match(%+v) = %v, want %v", c.name, c.id, got, c.want)
		}
	}
}

func testConfig() *config.Config {
	return &config.Config{
		SessionSecret: "0123456789abcdef0123456789abcdef",
		SessionTTL:    time.Hour,
		Dev:           true,
	}
}

func TestSessionRoundTrip(t *testing.T) {
	m := NewSessionManager(testConfig())
	id := Identity{Subject: "sub-1", Email: "me@example.org", Login: "me"}

	rec := httptest.NewRecorder()
	if err := m.Save(rec, httptest.NewRequest("GET", "/", nil), id); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie set")
	}
	if cookies[0].Domain != "" {
		t.Errorf("cookie must be host-only, got Domain=%q", cookies[0].Domain)
	}
	if !cookies[0].HttpOnly {
		t.Error("cookie must be HttpOnly")
	}

	r := httptest.NewRequest("GET", "/p/abc", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	got, ok := m.Identity(r)
	if !ok || got != id {
		t.Fatalf("Identity() = %+v, %v; want %+v, true", got, ok, id)
	}
}

func TestSessionExpired(t *testing.T) {
	cfg := testConfig()
	cfg.SessionTTL = -time.Minute // already expired at save time
	m := NewSessionManager(cfg)

	rec := httptest.NewRecorder()
	if err := m.Save(rec, httptest.NewRequest("GET", "/", nil), Identity{Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	if _, ok := m.Identity(r); ok {
		t.Fatal("expired session must not authenticate")
	}
}

func TestSessionMissing(t *testing.T) {
	m := NewSessionManager(testConfig())
	if _, ok := m.Identity(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("request without cookie must not authenticate")
	}
}

func TestSanitizeNext(t *testing.T) {
	cases := map[string]string{
		"/p/abc":             "/p/abc",
		"/":                  "/",
		"//evil.example.org": "",
		"https://evil.org":   "",
		"":                   "",
		"relative/path":      "",
		"/ok\\..\\backslash": "",
	}
	for in, want := range cases {
		if got := sanitizeNext(in); got != want {
			t.Errorf("sanitizeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogoutRedirect(t *testing.T) {
	h := Handlers(NewSessionManager(testConfig()), NewAllowlist([]string{"me@example.org"}))

	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/auth/logout", strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	// Browser form post: land back on the page it came from.
	rec := post("next=/")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("form logout: got %d %q, want 303 /", rec.Code, rec.Header().Get("Location"))
	}

	// Programmatic caller: unchanged 204, no redirect.
	if rec := post(""); rec.Code != http.StatusNoContent {
		t.Errorf("bare logout: got %d, want 204", rec.Code)
	}

	// Off-origin target is dropped, not followed.
	rec = post("next=" + url.QueryEscape("//evil.example.org"))
	if rec.Code != http.StatusNoContent || rec.Header().Get("Location") != "" {
		t.Errorf("off-origin next: got %d %q, want 204 and no Location",
			rec.Code, rec.Header().Get("Location"))
	}
}

// --- CLI tokens ---

// fakeTokenStore is the persistence slice StoreValidator needs.
type fakeTokenStore struct {
	rec     TokenRecord
	present bool
	touched []time.Time
}

func (f *fakeTokenStore) GetToken(_ context.Context, id string) (TokenRecord, error) {
	if !f.present || f.rec.ID != id {
		return TokenRecord{}, errors.New("not found")
	}
	return f.rec, nil
}

func (f *fakeTokenStore) TouchToken(_ context.Context, _ string, at time.Time) error {
	f.touched = append(f.touched, at)
	return nil
}

func mintInto(t *testing.T, f *fakeTokenStore) string {
	t.Helper()
	m, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	f.rec = TokenRecord{
		ID: m.ID, Name: "laptop", Hash: m.Hash,
		OwnerSubject: "sub-1", OwnerLogin: "alice", OwnerEmail: "alice@example.org",
	}
	f.present = true
	return m.Plaintext
}

func TestMintParseRoundTrip(t *testing.T) {
	m, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.Plaintext, TokenPrefix) {
		t.Errorf("plaintext %q lacks prefix %q", m.Plaintext, TokenPrefix)
	}
	gotID, secret, err := ParseToken(m.Plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != m.ID {
		t.Errorf("id round trip: got %q, want %q", gotID, m.ID)
	}
	if HashSecret(secret) != m.Hash {
		t.Error("hash of parsed secret does not match the minted hash")
	}
	// The plaintext secret must not be derivable from what gets stored.
	if strings.Contains(m.Hash, secret) {
		t.Error("stored hash contains the secret")
	}

	other, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	if other.Plaintext == m.Plaintext || other.ID == m.ID {
		t.Error("Mint must not repeat itself")
	}
}

func TestParseTokenRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"no prefix":     "abcdefghijkl_secret",
		"wrong prefix":  "ghp_abcdefghijkl_secret",
		"no separator":  TokenPrefix + "abcdefghijklsecret",
		"empty id":      TokenPrefix + "_secret",
		"empty secret":  TokenPrefix + "abcdefghijkl_",
		"short id":      TokenPrefix + "abc_secret",
		"long id":       TokenPrefix + "abcdefghijklmnop_secret",
		"prefix only":   TokenPrefix,
		"separator max": TokenPrefix + "_",
	}
	for name, in := range cases {
		if _, _, err := ParseToken(in); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s (%q): got %v, want ErrInvalidToken", name, in, err)
		}
	}
}

func TestStoreValidatorAcceptsGoodToken(t *testing.T) {
	f := &fakeTokenStore{}
	plaintext := mintInto(t, f)

	id, err := NewStoreValidator(f).Validate(context.Background(), plaintext)
	if err != nil {
		t.Fatal(err)
	}
	want := Identity{Subject: "sub-1", Email: "alice@example.org", Login: "alice"}
	if id != want {
		t.Fatalf("identity: got %+v, want %+v", id, want)
	}
	if len(f.touched) != 1 {
		t.Errorf("first use must record last_used_at, got %d writes", len(f.touched))
	}
}

func TestStoreValidatorRejections(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC()
	future := time.Now().Add(time.Hour).UTC()

	t.Run("wrong secret", func(t *testing.T) {
		f := &fakeTokenStore{}
		plaintext := mintInto(t, f)
		id, _, _ := ParseToken(plaintext)
		forged := TokenPrefix + id + "_" + "not-the-secret"
		if _, err := NewStoreValidator(f).Validate(context.Background(), forged); err == nil {
			t.Fatal("a forged secret must be rejected")
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		f := &fakeTokenStore{}
		mintInto(t, f)
		other, _ := Mint()
		if _, err := NewStoreValidator(f).Validate(context.Background(), other.Plaintext); err == nil {
			t.Fatal("an unknown id must be rejected")
		}
	})

	t.Run("revoked", func(t *testing.T) {
		f := &fakeTokenStore{}
		plaintext := mintInto(t, f)
		f.rec.RevokedAt = &past
		if _, err := NewStoreValidator(f).Validate(context.Background(), plaintext); err == nil {
			t.Fatal("a revoked token must be rejected")
		}
	})

	t.Run("expired", func(t *testing.T) {
		f := &fakeTokenStore{}
		plaintext := mintInto(t, f)
		f.rec.ExpiresAt = &past
		if _, err := NewStoreValidator(f).Validate(context.Background(), plaintext); err == nil {
			t.Fatal("an expired token must be rejected")
		}
	})

	t.Run("not yet expired", func(t *testing.T) {
		f := &fakeTokenStore{}
		plaintext := mintInto(t, f)
		f.rec.ExpiresAt = &future
		if _, err := NewStoreValidator(f).Validate(context.Background(), plaintext); err != nil {
			t.Fatalf("a live token must be accepted: %v", err)
		}
	})
}

// last_used_at is display-only bookkeeping; it must not cost a write on every
// authenticated request against a single-connection database.
func TestStoreValidatorThrottlesTouch(t *testing.T) {
	f := &fakeTokenStore{}
	plaintext := mintInto(t, f)
	recent := time.Now().UTC()
	f.rec.LastUsedAt = &recent

	v := NewStoreValidator(f)
	if _, err := v.Validate(context.Background(), plaintext); err != nil {
		t.Fatal(err)
	}
	if len(f.touched) != 0 {
		t.Errorf("recent use must not be rewritten, got %d writes", len(f.touched))
	}

	stale := time.Now().Add(-2 * touchInterval).UTC()
	f.rec.LastUsedAt = &stale
	if _, err := v.Validate(context.Background(), plaintext); err != nil {
		t.Fatal(err)
	}
	if len(f.touched) != 1 {
		t.Errorf("stale use must be refreshed, got %d writes", len(f.touched))
	}
}
