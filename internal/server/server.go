// Package server implements the HTTP surface: a public app domain (homepage,
// health, ConnectRPC API) and an isolated pages domain (web login + report
// serving). Routing is by request host; all URLs are built from configured
// base URLs, never from the request.
package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/auth"
	"github.com/dusan/page-report/internal/config"
	"github.com/dusan/page-report/internal/store"
	"github.com/dusan/page-report/web"
)

// SessionReader extracts the authenticated identity from a request's session
// cookie, if any. ok=false means not logged in.
type SessionReader interface {
	Identity(r *http.Request) (auth.Identity, bool)
}

// AuthConfig is the device-flow configuration served to the CLI.
type AuthConfig struct {
	Provider       string
	Issuer         string
	ClientID       string
	Scopes         []string
	DeviceEndpoint string
	TokenEndpoint  string
}

// AuthConfigProvider supplies the settings returned by the GetAuthConfig RPC.
type AuthConfigProvider interface {
	AuthConfig() AuthConfig
}

type Server struct {
	cfg        *config.Config
	store      store.Store
	validator  auth.TokenValidator
	allow      *auth.Allowlist
	sessions   SessionReader
	authCfg    AuthConfigProvider
	authRoutes http.Handler

	appHost   string
	pagesHost string
}

func New(cfg *config.Config, st store.Store, validator auth.TokenValidator,
	allow *auth.Allowlist, sessions SessionReader, authCfg AuthConfigProvider,
	authRoutes http.Handler) *Server {
	return &Server{
		cfg:        cfg,
		store:      st,
		validator:  validator,
		allow:      allow,
		sessions:   sessions,
		authCfg:    authCfg,
		authRoutes: authRoutes,
		appHost:    hostOf(cfg.AppBaseURL),
		pagesHost:  hostOf(cfg.PagesBaseURL),
	}
}

// Handler returns the root handler dispatching between the app and pages
// muxes by request host.
func (s *Server) Handler() http.Handler {
	appMux := http.NewServeMux()
	appMux.HandleFunc("GET /{$}", s.handleHome)
	appMux.HandleFunc("GET /healthz", s.handleHealth)
	appMux.Handle("GET /static/", http.FileServerFS(web.StaticFS))
	rpcPath, rpcHandler := pagereportv1connect.NewPageServiceHandler(
		&rpcService{s: s},
		s.connectOptions()...,
	)
	appMux.Handle(rpcPath, rpcHandler)

	pagesMux := http.NewServeMux()
	if s.authRoutes != nil {
		pagesMux.Handle("/auth/", s.authRoutes)
	}
	pagesMux.HandleFunc("GET /p/{id}", s.handlePage)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestHost(r) {
		case s.appHost:
			appMux.ServeHTTP(w, r)
		case s.pagesHost:
			pagesMux.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func hostOf(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func requestHost(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}
