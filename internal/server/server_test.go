package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/auth"
	"github.com/dusan/page-report/internal/config"
	"github.com/dusan/page-report/internal/store"
)

// --- fakes ---

type fakeStore struct {
	pages map[string]store.Page
}

func newFakeStore() *fakeStore { return &fakeStore{pages: map[string]store.Page{}} }

func (f *fakeStore) CreatePage(_ context.Context, p store.Page) error {
	if _, ok := f.pages[p.ID]; ok {
		return errors.New("UNIQUE constraint failed: pages.id")
	}
	f.pages[p.ID] = p
	return nil
}

func (f *fakeStore) GetPage(_ context.Context, id string) (store.Page, error) {
	p, ok := f.pages[id]
	if !ok {
		return store.Page{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) ListPages(context.Context) ([]store.Page, error) {
	var out []store.Page
	for _, p := range f.pages {
		p.Content = nil
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) DeletePage(_ context.Context, id string) error {
	if _, ok := f.pages[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.pages, id)
	return nil
}

func (f *fakeStore) PrunePages(_ context.Context, cutoff time.Time) (int64, error) {
	var n int64
	for id, p := range f.pages {
		if p.CreatedAt.Before(cutoff) {
			delete(f.pages, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) Close() error               { return nil }

type fakeSessions struct {
	id auth.Identity
	ok bool
}

func (f fakeSessions) Identity(*http.Request) (auth.Identity, bool) { return f.id, f.ok }

type fakeValidator struct {
	id  auth.Identity
	err error
}

func (f fakeValidator) Validate(context.Context, string) (auth.Identity, error) {
	return f.id, f.err
}

type fakeAuthCfg struct{}

func (fakeAuthCfg) AuthConfig() AuthConfig {
	return AuthConfig{Provider: "oidc", Issuer: "https://idp.example.org", ClientID: "cid"}
}

func testServer(t *testing.T, st store.Store, sessions SessionReader, validator auth.TokenValidator) http.Handler {
	t.Helper()
	cfg := &config.Config{
		AppBaseURL:     "https://app.example.org",
		PagesBaseURL:   "https://pages.example.org",
		MaxUploadBytes: 1024,
	}
	allow := auth.NewAllowlist([]string{"me@example.org"})
	return New(cfg, st, validator, allow, sessions, fakeAuthCfg{}, nil).Handler()
}

func doReq(h http.Handler, method, host, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// --- tests ---

func TestUnknownHost(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	if rec := doReq(h, "GET", "evil.example.org", "/"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown host: got %d, want 404", rec.Code)
	}
}

func TestHomepagePerDomain(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	if rec := doReq(h, "GET", "app.example.org", "/"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "page-report") {
		t.Fatalf("app homepage: got %d", rec.Code)
	}
	// Port must be stripped when matching hosts.
	if rec := doReq(h, "GET", "app.example.org:8443", "/"); rec.Code != http.StatusOK {
		t.Fatalf("app homepage with port: got %d, want 200", rec.Code)
	}
	if rec := doReq(h, "GET", "pages.example.org", "/"); rec.Code != http.StatusOK {
		t.Fatalf("pages landing: got %d, want 200", rec.Code)
	}
}

func TestPagesLandingAnonymous(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{ok: false}, fakeValidator{})
	rec := doReq(h, "GET", "pages.example.org", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("landing: got %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"export PR_SERVER_URL=https://app.example.org",
		"https://github.com/dusansimic/page-report",
		`href="/auth/login"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing body missing %q", want)
		}
	}
	if strings.Contains(body, "/auth/logout") {
		t.Error("anonymous landing offers a sign out control")
	}

	hd := rec.Header()
	if ct := hd.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := hd.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if hd.Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
	if got := hd.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestPagesLandingSignedIn(t *testing.T) {
	h := testServer(t, newFakeStore(),
		fakeSessions{id: auth.Identity{Email: "me@example.org"}, ok: true}, fakeValidator{})
	rec := doReq(h, "GET", "pages.example.org", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("landing: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "me@example.org") || !strings.Contains(body, `action="/auth/logout"`) {
		t.Error("signed-in landing missing account or sign out control")
	}
	if strings.Contains(body, `href="/auth/login"`) {
		t.Error("signed-in landing still offers sign in")
	}

	// A session outlives the allowlist entry that minted it.
	h = testServer(t, newFakeStore(),
		fakeSessions{id: auth.Identity{Email: "intruder@example.org"}, ok: true}, fakeValidator{})
	rec = doReq(h, "GET", "pages.example.org", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("landing: got %d, want 200", rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "not on the allowlist") {
		t.Error("non-allowlisted session not warned")
	}
	if !strings.Contains(body, `action="/auth/logout"`) {
		t.Error("non-allowlisted session cannot clear its stale cookie")
	}
}

func TestPagesLandingEscapesIdentity(t *testing.T) {
	h := testServer(t, newFakeStore(),
		fakeSessions{id: auth.Identity{Email: "<script>alert(1)</script>@x"}, ok: true},
		fakeValidator{})
	body := doReq(h, "GET", "pages.example.org", "/").Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("identity rendered unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("escaped identity not rendered at all")
	}
}

func TestPagesLandingRoutingIsExact(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	if rec := doReq(h, "GET", "pages.example.org", "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown pages path: got %d, want 404", rec.Code)
	}
	if rec := doReq(h, "POST", "pages.example.org", "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST landing: got %d, want 405", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	if rec := doReq(h, "GET", "app.example.org", "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("healthz: got %d", rec.Code)
	}
}

func TestPageRequiresSession(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{ok: false}, fakeValidator{})
	rec := doReq(h, "GET", "pages.example.org", "/p/abc123")
	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login?next=%2Fp%2Fabc123" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestPageServedWithHeaders(t *testing.T) {
	st := newFakeStore()
	st.pages["abc123"] = store.Page{
		ID: "abc123", Content: []byte("<html>hi</html>"),
		ContentType: "text/html; charset=utf-8",
	}
	sess := fakeSessions{id: auth.Identity{Email: "me@example.org"}, ok: true}
	h := testServer(t, st, sess, fakeValidator{})

	rec := doReq(h, "GET", "pages.example.org", "/p/abc123")
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>hi</html>" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing CSP")
	}

	// Allowlisted session but unknown page id.
	if rec := doReq(h, "GET", "pages.example.org", "/p/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing page: got %d, want 404", rec.Code)
	}

	// Session present but identity not allowlisted.
	h403 := testServer(t, st, fakeSessions{id: auth.Identity{Email: "intruder@example.org"}, ok: true}, fakeValidator{})
	if rec := doReq(h403, "GET", "pages.example.org", "/p/abc123"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-allowlisted: got %d, want 403", rec.Code)
	}
}

// rpcTestServer wraps the app-domain handler in an httptest server, forcing
// the request host so host routing selects the app mux.
func rpcTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "app.example.org"
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestRPCRequiresBearer(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{err: errors.New("bad token")})
	ts := rpcTestServer(t, h)

	c := pagereportv1connect.NewPageServiceClient(http.DefaultClient, ts.URL, connect.WithProtoJSON())
	_, err := c.ListPages(context.Background(), connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("got %v, want unauthenticated", err)
	}

	// GetAuthConfig must work without a token.
	resp, err := c.GetAuthConfig(context.Background(), connect.NewRequest(&pagereportv1.GetAuthConfigRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetProvider() != "oidc" {
		t.Fatalf("provider = %q", resp.Msg.GetProvider())
	}
}

func TestUploadAndLifecycle(t *testing.T) {
	st := newFakeStore()
	validator := fakeValidator{id: auth.Identity{Email: "me@example.org"}}
	h := testServer(t, st, fakeSessions{}, validator)
	ts := rpcTestServer(t, h)

	authInterceptor := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer tok")
			return next(ctx, req)
		}
	})
	c := pagereportv1connect.NewPageServiceClient(http.DefaultClient, ts.URL,
		connect.WithProtoJSON(), connect.WithInterceptors(authInterceptor))
	ctx := context.Background()

	up, err := c.UploadPage(ctx, connect.NewRequest(&pagereportv1.UploadPageRequest{
		Content: []byte("<html>report</html>"),
		Title:   "Report",
	}))
	if err != nil {
		t.Fatal(err)
	}
	id := up.Msg.GetId()
	if id == "" || up.Msg.GetUrl() != "https://pages.example.org/p/"+id {
		t.Fatalf("upload: id=%q url=%q", id, up.Msg.GetUrl())
	}
	if st.pages[id].CreatedBy != "me@example.org" {
		t.Fatalf("created_by = %q", st.pages[id].CreatedBy)
	}

	// Oversized content rejected.
	if _, err := c.UploadPage(ctx, connect.NewRequest(&pagereportv1.UploadPageRequest{
		Content: make([]byte, 2048),
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("oversized: got %v, want invalid_argument", err)
	}

	list, err := c.ListPages(ctx, connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if err != nil || len(list.Msg.GetPages()) != 1 {
		t.Fatalf("list: %v, %d pages", err, len(list.Msg.GetPages()))
	}

	// GetPage with content: the exact bytes uploaded, plus full metadata. This
	// is the contract `page-report get` relies on.
	got, err := c.GetPage(ctx, connect.NewRequest(&pagereportv1.GetPageRequest{
		Id:             id,
		IncludeContent: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Msg.GetContent()) != "<html>report</html>" {
		t.Fatalf("get content = %q", got.Msg.GetContent())
	}
	meta := got.Msg.GetMeta()
	if meta.GetId() != id || meta.GetTitle() != "Report" ||
		meta.GetUrl() != "https://pages.example.org/p/"+id ||
		meta.GetSizeBytes() != int64(len("<html>report</html>")) ||
		meta.GetCreatedBy() != "me@example.org" {
		t.Fatalf("get meta = %+v", meta)
	}

	// Without include_content the metadata is still complete, content omitted.
	metaOnly, err := c.GetPage(ctx, connect.NewRequest(&pagereportv1.GetPageRequest{Id: id}))
	if err != nil {
		t.Fatal(err)
	}
	if len(metaOnly.Msg.GetContent()) != 0 {
		t.Fatalf("get without include_content returned %d bytes", len(metaOnly.Msg.GetContent()))
	}
	if metaOnly.Msg.GetMeta().GetId() != id {
		t.Fatalf("meta-only id = %q", metaOnly.Msg.GetMeta().GetId())
	}

	if _, err := c.GetPage(ctx, connect.NewRequest(&pagereportv1.GetPageRequest{
		Id: "nosuchid",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("get unknown id: got %v, want not_found", err)
	}

	if _, err := c.DeletePage(ctx, connect.NewRequest(&pagereportv1.DeletePageRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeletePage(ctx, connect.NewRequest(&pagereportv1.DeletePageRequest{Id: id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("double delete: got %v, want not_found", err)
	}
}

func TestRPCPermissionDenied(t *testing.T) {
	validator := fakeValidator{id: auth.Identity{Email: "intruder@example.org"}}
	h := testServer(t, newFakeStore(), fakeSessions{}, validator)
	ts := rpcTestServer(t, h)

	authInterceptor := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer tok")
			return next(ctx, req)
		}
	})
	c := pagereportv1connect.NewPageServiceClient(http.DefaultClient, ts.URL,
		connect.WithProtoJSON(), connect.WithInterceptors(authInterceptor))

	_, err := c.ListPages(context.Background(), connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", err)
	}
}
