package client

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"net/http"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
)

func TestCredentialsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := Load(); err != ErrNotLoggedIn {
		t.Fatalf("Load without file = %v, want ErrNotLoggedIn", err)
	}

	creds := Credentials{
		ServerURL:    "https://app.example.org",
		Provider:     "oidc",
		ClientID:     "cid",
		AccessToken:  "at",
		IDToken:      "idt",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(time.Hour).UTC().Truncate(time.Second),
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

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func TestBearerInterceptor(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	ctx := context.Background()
	req := connect.NewRequest(&pagereportv1.GetAuthConfigRequest{})

	if _, err := New(ts.URL, staticToken("tok-123")).GetAuthConfig(ctx, req); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want Bearer tok-123", gotAuth)
	}

	if _, err := New(ts.URL, nil).GetAuthConfig(ctx, connect.NewRequest(&pagereportv1.GetAuthConfigRequest{})); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Fatalf("nil TokenSource must send no Authorization header, got %q", gotAuth)
	}
}
