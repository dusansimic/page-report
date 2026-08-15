// Package server implements the HTTP surface. Everything is served from one
// origin: the React SPA, the ConnectRPC APIs, the OAuth routes, and the
// sandboxed report pages at /p/{id}. All URLs are built from the configured
// base URL, never from the request's Host header.
package server

import (
	"net/http"
	"strings"

	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/auth"
	"github.com/dusan/page-report/internal/config"
	"github.com/dusan/page-report/internal/store"
)

// SessionReader decodes the session cookie. ok=false means not logged in.
type SessionReader interface {
	Get(r *http.Request) (auth.Session, bool)
	Identity(r *http.Request) (auth.Identity, bool)
}

type Server struct {
	cfg        *config.Config
	store      store.Store
	validator  auth.TokenValidator
	allow      *auth.Allowlist
	sessions   SessionReader
	authRoutes http.Handler
	spa        *spaHandler
}

func New(cfg *config.Config, st store.Store, validator auth.TokenValidator,
	allow *auth.Allowlist, sessions SessionReader, authRoutes http.Handler) *Server {
	return &Server{
		cfg:        cfg,
		store:      st,
		validator:  validator,
		allow:      allow,
		sessions:   sessions,
		authRoutes: authRoutes,
		spa:        newSPAHandler(),
	}
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	if s.authRoutes != nil {
		mux.Handle("/auth/", s.authRoutes)
	}

	// The CLI API: bearer-authenticated and, by design, unreachable from a
	// browser. guardScriptedFetch is what enforces the second half.
	pagePath, pageHandler := pagereportv1connect.NewPageServiceHandler(
		&rpcService{s: s}, s.connectOptions()...,
	)
	mux.Handle(pagePath, guardScriptedFetch(pageHandler))

	// The browser API. This is the one surface that accepts script-initiated
	// requests, so it carries its own origin + session checks.
	dashPath, dashHandler := pagereportv1connect.NewDashboardServiceHandler(
		&dashboardService{s: s}, s.dashboardOptions()...,
	)
	mux.Handle(dashPath, s.guardDashboard(dashHandler))

	// Report pages. Registered as a more specific pattern than the SPA
	// fallback below, so ServeMux routes /p/{id} here and never to the SPA.
	mux.HandleFunc("GET /p/{id}", s.handlePage)

	mux.Handle("GET /assets/", s.spa.assets())
	mux.HandleFunc("/", s.serveSPA)

	return mux
}

// apiPrefixes are the paths that belong to a handler rather than to the SPA's
// client-side router. A request under one of these that reaches the fallback
// is a mistake — a typo'd RPC, a stale client — and must 404 rather than
// silently return the SPA's HTML shell with a 200.
var apiPrefixes = []string{
	"/pagereport.",
	"/auth/",
	"/healthz",
	"/p/",
	"/assets/",
}

func isAPIPath(p string) bool {
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// denyScriptedFetch rejects requests a browser marks as script-initiated. It
// backs up the report sandbox from the server side: a report doing
// fetch("/p/other-id") sends Sec-Fetch-Dest: empty and is refused here even if
// a CSP header is ever lost in transit. Non-browser clients — the CLI — omit
// the header entirely and pass through; reports are only ever loaded as
// top-level documents. It returns true when the request was handled.
//
// This also refuses browser-originated calls to the CLI API, which is
// bearer-auth and CLI-only by design. The SPA does not use that API; it talks
// to DashboardService, which is guarded by guardDashboard instead.
func denyScriptedFetch(w http.ResponseWriter, r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "", "document":
		return false
	}
	http.Error(w, "forbidden", http.StatusForbidden)
	return true
}

// guardScriptedFetch applies denyScriptedFetch in front of h.
func guardScriptedFetch(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if denyScriptedFetch(w, r) {
			return
		}
		h.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the request's Origin header matches the
// configured base URL. A missing Origin is reported separately so callers can
// require it on state-changing methods while tolerating its absence on GET.
func (s *Server) sameOrigin(r *http.Request) (present, ok bool) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false, false
	}
	return true, strings.EqualFold(strings.TrimRight(origin, "/"), s.cfg.BaseURL)
}
