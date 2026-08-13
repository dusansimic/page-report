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

const baseURL = "https://reports.example.org"

// --- fakes ---

type fakeStore struct {
	pages  map[string]store.Page
	tokens map[string]store.Token
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		pages:  map[string]store.Page{},
		tokens: map[string]store.Token{},
	}
}

// owns mirrors the SQL ownership predicate, including the legacy fallback for
// rows written before pages carried an owner.
func owns(p store.Page, o store.Owner) bool {
	if p.OwnerSubject != "" {
		return p.OwnerSubject == o.Subject
	}
	for _, v := range []string{o.Email, o.Login, o.Subject} {
		if v != "" && p.CreatedBy == v {
			return true
		}
	}
	return false
}

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

func (f *fakeStore) GetPageForOwner(_ context.Context, id string, o store.Owner) (store.Page, error) {
	p, ok := f.pages[id]
	if !ok || !owns(p, o) {
		return store.Page{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) ListPagesByOwner(_ context.Context, o store.Owner) ([]store.Page, error) {
	var out []store.Page
	for _, p := range f.pages {
		if !owns(p, o) {
			continue
		}
		p.Content = nil
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) DeletePageForOwner(_ context.Context, id string, o store.Owner) error {
	p, ok := f.pages[id]
	if !ok || !owns(p, o) {
		return store.ErrNotFound
	}
	delete(f.pages, id)
	return nil
}

func (f *fakeStore) PrunePagesForOwner(_ context.Context, cutoff time.Time, o store.Owner) (int64, error) {
	var n int64
	for id, p := range f.pages {
		if owns(p, o) && p.CreatedAt.Before(cutoff) {
			delete(f.pages, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) CreateToken(_ context.Context, t store.Token) error {
	f.tokens[t.ID] = t
	return nil
}

func (f *fakeStore) GetToken(_ context.Context, id string) (store.Token, error) {
	t, ok := f.tokens[id]
	if !ok {
		return store.Token{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) ListTokensByOwner(_ context.Context, subject string) ([]store.Token, error) {
	var out []store.Token
	for _, t := range f.tokens {
		if t.OwnerSubject == subject {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeStore) UpdateTokenHash(_ context.Context, id, subject, hash string) error {
	t, ok := f.tokens[id]
	if !ok || t.OwnerSubject != subject {
		return store.ErrNotFound
	}
	t.Hash = hash
	t.LastUsedAt = nil
	t.RevokedAt = nil
	f.tokens[id] = t
	return nil
}

func (f *fakeStore) RevokeToken(_ context.Context, id, subject string) error {
	t, ok := f.tokens[id]
	if !ok || t.OwnerSubject != subject || t.RevokedAt != nil {
		return store.ErrNotFound
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	f.tokens[id] = t
	return nil
}

func (f *fakeStore) DeleteToken(_ context.Context, id, subject string) error {
	t, ok := f.tokens[id]
	if !ok || t.OwnerSubject != subject {
		return store.ErrNotFound
	}
	delete(f.tokens, id)
	return nil
}

func (f *fakeStore) TouchToken(_ context.Context, id string, at time.Time) error {
	if t, ok := f.tokens[id]; ok {
		t.LastUsedAt = &at
		f.tokens[id] = t
	}
	return nil
}

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) Close() error               { return nil }

type fakeSessions struct {
	id       auth.Identity
	ok       bool
	authTime time.Time
}

func (f fakeSessions) Get(*http.Request) (auth.Session, bool) {
	return auth.Session{Identity: f.id, AuthTime: f.authTime}, f.ok
}

func (f fakeSessions) Identity(*http.Request) (auth.Identity, bool) { return f.id, f.ok }

// signedIn is a session that authenticated just now, so token minting is
// allowed.
func signedIn(email string) fakeSessions {
	return fakeSessions{
		id:       auth.Identity{Subject: "sub-" + email, Email: email},
		ok:       true,
		authTime: time.Now(),
	}
}

type fakeValidator struct {
	id  auth.Identity
	err error
}

func (f fakeValidator) Validate(context.Context, string) (auth.Identity, error) {
	return f.id, f.err
}

// --- harness ---

func newTestServer(t *testing.T, st store.Store, sessions SessionReader,
	validator auth.TokenValidator) *Server {
	t.Helper()
	cfg := &config.Config{
		BaseURL:               baseURL,
		MaxUploadBytes:        1024,
		TokenMintReauthWindow: 10 * time.Minute,
	}
	allow := auth.NewAllowlist([]string{"me@example.org", "other@example.org"})
	s := New(cfg, st, validator, allow, sessions, nil)
	// Whether web/app/dist happens to hold a build is a property of the
	// checkout, not of the code under test. Start from "not built" and let
	// testServerWithSPA opt in.
	s.spa.index = nil
	return s
}

func testServer(t *testing.T, st store.Store, sessions SessionReader,
	validator auth.TokenValidator) http.Handler {
	t.Helper()
	return newTestServer(t, st, sessions, validator).Handler()
}

// testServerWithSPA stands in for a binary built with the frontend bundled;
// the embedded dist is empty during tests.
func testServerWithSPA(t *testing.T, st store.Store, sessions SessionReader,
	validator auth.TokenValidator) http.Handler {
	t.Helper()
	s := newTestServer(t, st, sessions, validator)
	s.spa.index = []byte("<!doctype html><title>page-report</title>")
	return s.Handler()
}

func doReq(h http.Handler, method, path string) *httptest.ResponseRecorder {
	return doReqHeaders(h, method, path, nil)
}

func doReqHeaders(h http.Handler, method, path string,
	headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func rpcTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

// bearerClient is the authenticated CLI client, with optional extra headers on
// every request.
func bearerClient(ts *httptest.Server, headers map[string]string) pagereportv1connect.PageServiceClient {
	interceptor := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer tok")
			for k, v := range headers {
				req.Header().Set(k, v)
			}
			return next(ctx, req)
		}
	})
	return pagereportv1connect.NewPageServiceClient(http.DefaultClient, ts.URL,
		connect.WithProtoJSON(), connect.WithInterceptors(interceptor))
}

// dashClient is the browser client. It sends the same-origin Origin header a
// real browser would; tests that check the guard override it.
func dashClient(ts *httptest.Server, headers map[string]string) pagereportv1connect.DashboardServiceClient {
	interceptor := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Origin", baseURL)
			for k, v := range headers {
				if v == "" {
					req.Header().Del(k)
					continue
				}
				req.Header().Set(k, v)
			}
			return next(ctx, req)
		}
	})
	return pagereportv1connect.NewDashboardServiceClient(http.DefaultClient, ts.URL,
		connect.WithProtoJSON(), connect.WithInterceptors(interceptor))
}

// --- routing ---

func TestHealthz(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	if rec := doReq(h, "GET", "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("healthz: got %d", rec.Code)
	}
}

// The SPA owns client-side routes, but never a path a real handler claims: a
// typo'd RPC must 404 rather than get a 200 full of HTML.
func TestSPAFallback(t *testing.T) {
	h := testServerWithSPA(t, newFakeStore(), fakeSessions{}, fakeValidator{})

	for _, path := range []string{"/", "/dashboard", "/tokens", "/some/deep/route"} {
		rec := doReq(h, "GET", path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "page-report") {
			t.Errorf("%s: got %d, want the SPA shell", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", path, got)
		}
	}

	for _, path := range []string{
		"/pagereport.v1.PageService/Bogus",
		"/pagereport.v1.Nope/ListPages",
		"/auth/nope",
		"/assets/missing.js",
	} {
		if rec := doReq(h, "GET", path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404 rather than the SPA shell", path, rec.Code)
		}
	}
}

// The SPA shell and a report page share an origin, so they must not share a
// policy. This asserts the SPA's; TestPageServedWithHeaders asserts the
// report's.
func TestSPACSP(t *testing.T) {
	h := testServerWithSPA(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	rec := doReq(h, "GET", "/dashboard")

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("SPA CSP must allow its own bundle: %q", csp)
	}
	if strings.Contains(csp, "sandbox") {
		t.Errorf("SPA must not be sandboxed: %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("SPA CSP must deny framing: %q", csp)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
}

// A binary built without running the frontend build must say so rather than
// serving an empty page or panicking at startup.
func TestSPANotBuilt(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{})
	rec := doReq(h, "GET", "/dashboard")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pnpm") {
		t.Errorf("503 body should name the missing build step: %q", rec.Body.String())
	}
	// The rest of the server still works.
	if rec := doReq(h, "GET", "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("healthz with an unbuilt SPA: got %d", rec.Code)
	}
}

// --- report pages ---

func TestPageRequiresSession(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{ok: false}, fakeValidator{})
	rec := doReq(h, "GET", "/p/abc123")
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
	h := testServer(t, st, signedIn("me@example.org"), fakeValidator{})

	rec := doReq(h, "GET", "/p/abc123")
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>hi</html>" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	// Reports are quarantined in an opaque origin and may not script. With the
	// API on this same origin, this policy is what keeps them apart.
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.HasPrefix(csp, "sandbox ") {
		t.Errorf("CSP does not sandbox the report: %q", csp)
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("sandbox retains the origin: %q", csp)
	}
	if strings.Contains(csp, "allow-scripts") || strings.Contains(csp, "script-src") {
		t.Errorf("CSP permits scripts: %q", csp)
	}

	if rec := doReq(h, "GET", "/p/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing page: got %d, want 404", rec.Code)
	}

	h403 := testServer(t, st, signedIn("intruder@example.org"), fakeValidator{})
	if rec := doReq(h403, "GET", "/p/abc123"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-allowlisted: got %d, want 403", rec.Code)
	}
}

// /p/{id} is registered as a more specific pattern than the SPA fallback. If
// that ever inverts, reports would be served as the SPA shell — with the SPA's
// CSP instead of the sandbox.
func TestReportBeatsSPAFallback(t *testing.T) {
	st := newFakeStore()
	st.pages["abc123"] = store.Page{
		ID: "abc123", Content: []byte("<html>hi</html>"), ContentType: contentTypeHTML,
	}
	h := testServerWithSPA(t, st, signedIn("me@example.org"), fakeValidator{})

	rec := doReq(h, "GET", "/p/abc123")
	if rec.Body.String() != "<html>hi</html>" {
		t.Fatalf("report path served %q", rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox ") {
		t.Fatalf("report served without the sandbox: %q", csp)
	}
}

// Sharing is the point of a report link: any allowlisted human may open one,
// even though the dashboard only lists their own.
func TestReportViewableByAnyAllowlistedUser(t *testing.T) {
	st := newFakeStore()
	st.pages["abc123"] = store.Page{
		ID: "abc123", Content: []byte("<html>hi</html>"), ContentType: contentTypeHTML,
		OwnerSubject: "sub-me@example.org", CreatedBy: "me@example.org",
	}
	h := testServer(t, st, signedIn("other@example.org"), fakeValidator{})
	if rec := doReq(h, "GET", "/p/abc123"); rec.Code != http.StatusOK {
		t.Fatalf("another allowlisted user cannot open a shared report: %d", rec.Code)
	}
}

// Rows written before the allowlist existed must never be echoed back unvetted.
func TestPageContentTypeDowngraded(t *testing.T) {
	st := newFakeStore()
	st.pages["legacy"] = store.Page{
		ID: "legacy", Content: []byte("<svg/>"), ContentType: "image/svg+xml",
	}
	st.pages["plain"] = store.Page{
		ID: "plain", Content: []byte("hi"), ContentType: "text/plain",
	}
	h := testServer(t, st, signedIn("me@example.org"), fakeValidator{})

	rec := doReq(h, "GET", "/p/legacy")
	if got := rec.Header().Get("Content-Type"); got != contentTypeText {
		t.Errorf("legacy row served as %q, want %q", got, contentTypeText)
	}
	rec = doReq(h, "GET", "/p/plain")
	if got := rec.Header().Get("Content-Type"); got != contentTypeText {
		t.Errorf("text/plain row served as %q, want %q", got, contentTypeText)
	}
}

// The sandbox is backed up server-side: a report scripting its way to another
// report sends Sec-Fetch-Dest: empty and is refused before the store is read.
func TestSecFetchDestGate(t *testing.T) {
	st := newFakeStore()
	st.pages["abc123"] = store.Page{
		ID: "abc123", Content: []byte("<html>hi</html>"), ContentType: contentTypeHTML,
	}
	h := testServer(t, st, signedIn("me@example.org"), fakeValidator{})

	for _, tc := range []struct {
		dest string
		want int
	}{
		{"", http.StatusOK},         // non-browser client, header absent
		{"document", http.StatusOK}, // top-level navigation
		{"empty", http.StatusForbidden},
		{"iframe", http.StatusForbidden},
		{"image", http.StatusForbidden},
	} {
		headers := map[string]string{}
		if tc.dest != "" {
			headers["Sec-Fetch-Dest"] = tc.dest
		}
		rec := doReqHeaders(h, "GET", "/p/abc123", headers)
		if rec.Code != tc.want {
			t.Errorf("Sec-Fetch-Dest %q: got %d, want %d", tc.dest, rec.Code, tc.want)
		}
	}
}

// --- CLI API ---

func TestRPCRequiresBearer(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{}, fakeValidator{err: errors.New("bad token")})
	ts := rpcTestServer(t, h)

	c := pagereportv1connect.NewPageServiceClient(http.DefaultClient, ts.URL, connect.WithProtoJSON())
	_, err := c.ListPages(context.Background(), connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("got %v, want unauthenticated", err)
	}

	// GetServerInfo must work without a token: `page-report login` calls it
	// before the user has one.
	resp, err := c.GetServerInfo(context.Background(),
		connect.NewRequest(&pagereportv1.GetServerInfoRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetBaseUrl() != baseURL || resp.Msg.GetTokensUrl() != baseURL+"/tokens" {
		t.Fatalf("server info = %+v", resp.Msg)
	}
}

func TestWhoAmI(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{},
		fakeValidator{id: auth.Identity{Subject: "s1", Email: "me@example.org", Login: "me"}})
	c := bearerClient(rpcTestServer(t, h), nil)

	resp, err := c.WhoAmI(context.Background(), connect.NewRequest(&pagereportv1.WhoAmIRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetEmail() != "me@example.org" || resp.Msg.GetSubject() != "s1" {
		t.Fatalf("whoami = %+v", resp.Msg)
	}
}

func TestUploadAndLifecycle(t *testing.T) {
	st := newFakeStore()
	validator := fakeValidator{id: auth.Identity{Subject: "s1", Email: "me@example.org"}}
	h := testServer(t, st, fakeSessions{}, validator)
	c := bearerClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	up, err := c.UploadPage(ctx, connect.NewRequest(&pagereportv1.UploadPageRequest{
		Content: []byte("<html>report</html>"),
		Title:   "Report",
	}))
	if err != nil {
		t.Fatal(err)
	}
	id := up.Msg.GetId()
	if id == "" || up.Msg.GetUrl() != baseURL+"/p/"+id {
		t.Fatalf("upload: id=%q url=%q", id, up.Msg.GetUrl())
	}
	if st.pages[id].CreatedBy != "me@example.org" {
		t.Fatalf("created_by = %q", st.pages[id].CreatedBy)
	}
	if st.pages[id].OwnerSubject != "s1" {
		t.Fatalf("owner_subject = %q, want the uploader's subject", st.pages[id].OwnerSubject)
	}

	if _, err := c.UploadPage(ctx, connect.NewRequest(&pagereportv1.UploadPageRequest{
		Content: make([]byte, 2048),
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("oversized: got %v, want invalid_argument", err)
	}

	list, err := c.ListPages(ctx, connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if err != nil || len(list.Msg.GetPages()) != 1 {
		t.Fatalf("list: %v, %d pages", err, len(list.Msg.GetPages()))
	}

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
		meta.GetUrl() != baseURL+"/p/"+id ||
		meta.GetSizeBytes() != int64(len("<html>report</html>")) ||
		meta.GetCreatedBy() != "me@example.org" {
		t.Fatalf("get meta = %+v", meta)
	}

	metaOnly, err := c.GetPage(ctx, connect.NewRequest(&pagereportv1.GetPageRequest{Id: id}))
	if err != nil {
		t.Fatal(err)
	}
	if len(metaOnly.Msg.GetContent()) != 0 {
		t.Fatalf("get without include_content returned %d bytes", len(metaOnly.Msg.GetContent()))
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

// Another user's page must be indistinguishable from one that does not exist.
func TestCLIAPIIsOwnerScoped(t *testing.T) {
	st := newFakeStore()
	st.pages["theirs"] = store.Page{
		ID: "theirs", Content: []byte("x"), ContentType: contentTypeHTML,
		OwnerSubject: "s-other", CreatedBy: "other@example.org",
	}
	h := testServer(t, st, fakeSessions{},
		fakeValidator{id: auth.Identity{Subject: "s1", Email: "me@example.org"}})
	c := bearerClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	list, err := c.ListPages(ctx, connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if err != nil || len(list.Msg.GetPages()) != 0 {
		t.Fatalf("list leaked another owner's pages: %v, %d", err, len(list.Msg.GetPages()))
	}
	if _, err := c.GetPage(ctx, connect.NewRequest(&pagereportv1.GetPageRequest{
		Id: "theirs",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("get: got %v, want not_found", err)
	}
	if _, err := c.DeletePage(ctx, connect.NewRequest(&pagereportv1.DeletePageRequest{
		Id: "theirs",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("delete: got %v, want not_found", err)
	}
	if _, err := c.PrunePages(ctx, connect.NewRequest(&pagereportv1.PrunePagesRequest{
		OlderThanSeconds: 1,
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.pages["theirs"]; !ok {
		t.Error("prune deleted another owner's page")
	}
}

// Uploads may only name content types that browsers render as inert documents.
// image/svg+xml is the one that matters: it renders as a scriptable document.
func TestUploadContentTypeAllowlist(t *testing.T) {
	st := newFakeStore()
	h := testServer(t, st, fakeSessions{}, fakeValidator{id: auth.Identity{Email: "me@example.org"}})
	c := bearerClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	for _, bad := range []string{
		"image/svg+xml",
		"application/xhtml+xml",
		"text/xml",
		"application/octet-stream",
		"not a media type",
	} {
		_, err := c.UploadPage(ctx, connect.NewRequest(&pagereportv1.UploadPageRequest{
			Content: []byte("x"), ContentType: bad,
		}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("content type %q: got %v, want invalid_argument", bad, err)
		}
	}

	for _, tc := range []struct{ sent, want string }{
		{"", contentTypeHTML},
		{"text/html", contentTypeHTML},
		{"text/html; charset=iso-8859-1", contentTypeHTML},
		{"TEXT/HTML", contentTypeHTML},
		{"text/plain", contentTypeText},
	} {
		up, err := c.UploadPage(ctx, connect.NewRequest(&pagereportv1.UploadPageRequest{
			Content: []byte("x"), ContentType: tc.sent,
		}))
		if err != nil {
			t.Fatalf("content type %q: %v", tc.sent, err)
		}
		if got := st.pages[up.Msg.GetId()].ContentType; got != tc.want {
			t.Errorf("content type %q stored as %q, want %q", tc.sent, got, tc.want)
		}
	}
}

// The CLI API stays unreachable from a browser even though the SPA now shares
// its origin. The SPA uses DashboardService instead.
func TestRPCRejectsBrowserFetch(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{},
		fakeValidator{id: auth.Identity{Email: "me@example.org"}})
	ts := rpcTestServer(t, h)

	if _, err := bearerClient(ts, nil).ListPages(context.Background(),
		connect.NewRequest(&pagereportv1.ListPagesRequest{})); err != nil {
		t.Fatalf("CLI client without Sec-Fetch headers: %v", err)
	}

	c := bearerClient(ts, map[string]string{"Sec-Fetch-Dest": "empty"})
	if _, err := c.ListPages(context.Background(),
		connect.NewRequest(&pagereportv1.ListPagesRequest{})); err == nil {
		t.Fatal("browser-originated RPC call was allowed")
	}
}

func TestRPCPermissionDenied(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{},
		fakeValidator{id: auth.Identity{Email: "intruder@example.org"}})
	c := bearerClient(rpcTestServer(t, h), nil)

	_, err := c.ListPages(context.Background(), connect.NewRequest(&pagereportv1.ListPagesRequest{}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", err)
	}
}

// --- dashboard API ---

func TestDashboardRequiresSameOrigin(t *testing.T) {
	h := testServer(t, newFakeStore(), signedIn("me@example.org"), fakeValidator{})
	ts := rpcTestServer(t, h)
	ctx := context.Background()

	for name, origin := range map[string]string{
		"foreign origin": "https://evil.example.org",
		"missing origin": "",
	} {
		c := dashClient(ts, map[string]string{"Origin": origin})
		if _, err := c.ListTokens(ctx, connect.NewRequest(&pagereportv1.ListTokensRequest{})); err == nil {
			t.Errorf("%s: request was allowed", name)
		}
	}

	// The same call from the app's own origin works.
	if _, err := dashClient(ts, nil).ListTokens(ctx,
		connect.NewRequest(&pagereportv1.ListTokensRequest{})); err != nil {
		t.Fatalf("same-origin call rejected: %v", err)
	}
}

func TestDashboardRequiresSession(t *testing.T) {
	h := testServer(t, newFakeStore(), fakeSessions{ok: false}, fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	if _, err := c.ListTokens(ctx, connect.NewRequest(&pagereportv1.ListTokensRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("ListTokens without a session: got %v, want unauthenticated", err)
	}

	// GetSession is the exception: the landing page asks before login.
	resp, err := c.GetSession(ctx, connect.NewRequest(&pagereportv1.GetSessionRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetAuthenticated() {
		t.Error("anonymous GetSession reports authenticated")
	}
	if resp.Msg.GetLoginUrl() == "" {
		t.Error("anonymous GetSession must offer a login URL")
	}
}

// A signed-in identity that is not (or no longer) allowlisted gets no identity
// in context, so it reads as not signed in rather than half-privileged.
func TestDashboardRejectsNonAllowlisted(t *testing.T) {
	h := testServer(t, newFakeStore(), signedIn("intruder@example.org"), fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)

	if _, err := c.ListTokens(context.Background(),
		connect.NewRequest(&pagereportv1.ListTokensRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("got %v, want unauthenticated", err)
	}
}

func TestDashboardSession(t *testing.T) {
	h := testServer(t, newFakeStore(), signedIn("me@example.org"), fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)

	resp, err := c.GetSession(context.Background(),
		connect.NewRequest(&pagereportv1.GetSessionRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	m := resp.Msg
	if !m.GetAuthenticated() || !m.GetAllowed() || m.GetEmail() != "me@example.org" {
		t.Fatalf("session = %+v", m)
	}
	if !m.GetAuthIsFresh() {
		t.Error("a session that just authenticated must read as fresh")
	}
}

func TestDashboardTokenLifecycle(t *testing.T) {
	st := newFakeStore()
	h := testServer(t, st, signedIn("me@example.org"), fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	created, err := c.CreateToken(ctx, connect.NewRequest(&pagereportv1.CreateTokenRequest{
		Name: "laptop",
	}))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := created.Msg.GetToken()
	if !strings.HasPrefix(plaintext, auth.TokenPrefix) {
		t.Fatalf("minted token = %q", plaintext)
	}
	tokenID := created.Msg.GetMeta().GetId()

	// The plaintext is returned once and is not recoverable from storage.
	if stored := st.tokens[tokenID].Hash; stored == "" || strings.Contains(plaintext, stored) {
		t.Errorf("stored hash %q is not a hash of the token", stored)
	}
	list, err := c.ListTokens(ctx, connect.NewRequest(&pagereportv1.ListTokensRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Msg.GetTokens()) != 1 {
		t.Fatalf("list returned %d tokens", len(list.Msg.GetTokens()))
	}
	meta := list.Msg.GetTokens()[0]
	if meta.GetName() != "laptop" || meta.GetDisplayPrefix() != auth.DisplayPrefix(tokenID) {
		t.Errorf("token meta = %+v", meta)
	}
	if strings.Contains(meta.String(), plaintext) {
		t.Error("token listing leaks the plaintext")
	}

	// Rotation keeps the id and issues a new secret under it.
	rotated, err := c.RotateToken(ctx, connect.NewRequest(&pagereportv1.RotateTokenRequest{
		Id: tokenID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Msg.GetMeta().GetId() != tokenID {
		t.Errorf("rotation changed the id: %q", rotated.Msg.GetMeta().GetId())
	}
	if rotated.Msg.GetToken() == plaintext {
		t.Error("rotation returned the same secret")
	}
	gotID, secret, err := auth.ParseToken(rotated.Msg.GetToken())
	if err != nil || gotID != tokenID {
		t.Fatalf("rotated token %q parses to %q, %v", rotated.Msg.GetToken(), gotID, err)
	}
	if auth.HashSecret(secret) != st.tokens[tokenID].Hash {
		t.Error("rotated plaintext does not match the stored hash")
	}

	if _, err := c.DeleteToken(ctx, connect.NewRequest(&pagereportv1.DeleteTokenRequest{
		Id: tokenID,
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.tokens[tokenID]; ok {
		t.Error("token survived delete")
	}
}

func TestDashboardTokenValidation(t *testing.T) {
	h := testServer(t, newFakeStore(), signedIn("me@example.org"), fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	for name, req := range map[string]*pagereportv1.CreateTokenRequest{
		"empty name":      {Name: "   "},
		"overlong name":   {Name: strings.Repeat("x", maxTokenNameLen+1)},
		"too short a ttl": {Name: "ok", ExpiresInSeconds: 60},
	} {
		if _, err := c.CreateToken(ctx, connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid_argument", name, err)
		}
	}

	resp, err := c.CreateToken(ctx, connect.NewRequest(&pagereportv1.CreateTokenRequest{
		Name: "ci", ExpiresInSeconds: int64((48 * time.Hour).Seconds()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetMeta().GetExpiresAt() == 0 {
		t.Error("expiry was not recorded")
	}
}

// A CLI token outlives the session that mints it, so a long-idle session must
// re-authenticate before it can produce one.
func TestTokenMintRequiresFreshAuth(t *testing.T) {
	stale := fakeSessions{
		id:       auth.Identity{Subject: "s1", Email: "me@example.org"},
		ok:       true,
		authTime: time.Now().Add(-time.Hour),
	}
	h := testServer(t, newFakeStore(), stale, fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	_, err := c.CreateToken(ctx, connect.NewRequest(&pagereportv1.CreateTokenRequest{Name: "x"}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("stale session mint: got %v, want permission_denied", err)
	}
	if !strings.Contains(err.Error(), reauthRequired) {
		t.Errorf("error must carry the %q sentinel for the SPA: %v", reauthRequired, err)
	}
	if _, err := c.RotateToken(ctx, connect.NewRequest(&pagereportv1.RotateTokenRequest{
		Id: "whatever",
	})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("stale session rotate: got %v, want permission_denied", err)
	}

	// Reading is unaffected; only minting is gated.
	if _, err := c.ListTokens(ctx, connect.NewRequest(&pagereportv1.ListTokensRequest{})); err != nil {
		t.Errorf("stale session must still list tokens: %v", err)
	}

	sess, err := c.GetSession(ctx, connect.NewRequest(&pagereportv1.GetSessionRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Msg.GetAuthIsFresh() {
		t.Error("stale session reported as fresh")
	}
}

func TestDashboardIsOwnerScoped(t *testing.T) {
	st := newFakeStore()
	st.pages["theirs"] = store.Page{
		ID: "theirs", Content: []byte("x"), ContentType: contentTypeHTML,
		OwnerSubject: "s-other", CreatedBy: "other@example.org",
	}
	st.tokens["theirtok"] = store.Token{ID: "theirtok", OwnerSubject: "s-other"}

	h := testServer(t, st, signedIn("me@example.org"), fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)
	ctx := context.Background()

	pages, err := c.ListPages(ctx, connect.NewRequest(&pagereportv1.DashboardServiceListPagesRequest{}))
	if err != nil || len(pages.Msg.GetPages()) != 0 {
		t.Fatalf("list leaked another owner's pages: %v, %d", err, len(pages.Msg.GetPages()))
	}
	if _, err := c.DeletePage(ctx, connect.NewRequest(&pagereportv1.DashboardServiceDeletePageRequest{
		Id: "theirs",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("cross-owner page delete: got %v, want not_found", err)
	}

	tokens, err := c.ListTokens(ctx, connect.NewRequest(&pagereportv1.ListTokensRequest{}))
	if err != nil || len(tokens.Msg.GetTokens()) != 0 {
		t.Fatalf("list leaked another owner's tokens: %v, %d", err, len(tokens.Msg.GetTokens()))
	}
	if _, err := c.DeleteToken(ctx, connect.NewRequest(&pagereportv1.DeleteTokenRequest{
		Id: "theirtok",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("cross-owner token delete: got %v, want not_found", err)
	}
	if _, err := c.RotateToken(ctx, connect.NewRequest(&pagereportv1.RotateTokenRequest{
		Id: "theirtok",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("cross-owner token rotate: got %v, want not_found", err)
	}
}

// A page owned by nobody (written before the owner column existed) stays with
// the user its created_by label names.
func TestLegacyPageOwnership(t *testing.T) {
	st := newFakeStore()
	st.pages["legacy"] = store.Page{
		ID: "legacy", Content: []byte("x"), ContentType: contentTypeHTML,
		CreatedBy: "me@example.org",
	}
	h := testServer(t, st, signedIn("me@example.org"), fakeValidator{})
	c := dashClient(rpcTestServer(t, h), nil)

	pages, err := c.ListPages(context.Background(),
		connect.NewRequest(&pagereportv1.DashboardServiceListPagesRequest{}))
	if err != nil || len(pages.Msg.GetPages()) != 1 {
		t.Fatalf("legacy page not visible to its creator: %v, %d", err, len(pages.Msg.GetPages()))
	}
}
