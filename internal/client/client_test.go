package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
)

func TestCredentialsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := Load(); err != ErrNotLoggedIn {
		t.Fatalf("Load without file = %v, want ErrNotLoggedIn", err)
	}

	creds := Credentials{
		ServerURL: "https://reports.example.org",
		Token:     "prt_abcdefghijkl_secret",
	}
	if err := Save(creds); err != nil {
		t.Fatal(err)
	}

	path, _ := credentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credentials file mode = %o, want 600", perm)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != creds {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, creds)
	}

	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != ErrNotLoggedIn {
		t.Fatalf("Load after delete = %v, want ErrNotLoggedIn", err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("second delete must be a no-op, got %v", err)
	}
}

// A credentials file from the device-flow era carries an access_token and no
// token field. Sending an empty bearer would surface as an opaque 401, so the
// mismatch has to be named where it happens.
func TestLoadRejectsLegacyCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "page-report", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"server_url":    "https://reports.example.org",
		"provider":      "github",
		"client_id":     "cid",
		"access_token":  "gho_legacy",
		"refresh_token": "r",
		"expiry":        time.Now().Add(time.Hour).Format(time.RFC3339),
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); !errors.Is(err, ErrLegacyCredentials) {
		t.Fatalf("Load = %v, want ErrLegacyCredentials", err)
	}
	if _, err := (StoredTokenSource{}).Token(context.Background()); !errors.Is(err, ErrLegacyCredentials) {
		t.Fatalf("Token = %v, want ErrLegacyCredentials", err)
	}
}

// PR_TOKEN lets an agent or CI job run without writing a credentials file, so
// it has to win over one that happens to exist.
func TestTokenEnvBeatsFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Credentials{ServerURL: "https://x", Token: "from-file"}); err != nil {
		t.Fatal(err)
	}

	got, err := (StoredTokenSource{}).Token(context.Background())
	if err != nil || got != "from-file" {
		t.Fatalf("without env: got %q, %v", got, err)
	}

	t.Setenv(TokenEnvVar, "  from-env  ")
	got, err = (StoredTokenSource{}).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Fatalf("PR_TOKEN ignored or untrimmed: got %q", got)
	}
}

func TestTokenEnvWithoutFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(TokenEnvVar, "from-env")
	got, err := (StoredTokenSource{}).Token(context.Background())
	if err != nil || got != "from-env" {
		t.Fatalf("got %q, %v; want the env token with no credentials file", got, err)
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"1.5d", 36 * time.Hour, false},
		{"720h", 720 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"", 0, true},
		{"-1d", 0, true},
		{"0h", 0, true},
		{"bogus", 0, true},
	}
	for _, c := range cases {
		got, err := ParseDuration(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseDuration(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBearerInterceptor(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	ctx := context.Background()
	req := connect.NewRequest(&pagereportv1.GetServerInfoRequest{})

	if _, err := New(ts.URL, StaticToken("tok-123")).GetServerInfo(ctx, req); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want Bearer tok-123", gotAuth)
	}

	if _, err := New(ts.URL, nil).GetServerInfo(ctx,
		connect.NewRequest(&pagereportv1.GetServerInfoRequest{})); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Fatalf("nil TokenSource must send no Authorization header, got %q", gotAuth)
	}
}
