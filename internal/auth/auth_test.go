package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestGitHubValidator(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		switch r.Header.Get("Authorization") {
		case "Bearer good":
			w.Write([]byte(`{"login":"octocat","id":42,"email":"octo@example.org"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer ts.Close()

	v := NewGitHubValidator()
	v.BaseURL = ts.URL
	v.HTTPClient = ts.Client()
	ctx := context.Background()

	id, err := v.Validate(ctx, "good")
	if err != nil {
		t.Fatal(err)
	}
	want := Identity{Subject: "42", Login: "octocat", Email: "octo@example.org"}
	if id != want {
		t.Fatalf("got %+v, want %+v", id, want)
	}

	// Second call must hit the cache.
	if _, err := v.Validate(ctx, "good"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 API call (cache hit on second), got %d", calls)
	}

	if _, err := v.Validate(ctx, "bad"); err == nil {
		t.Fatal("invalid token must fail")
	}
}
