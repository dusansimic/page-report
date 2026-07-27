package server

import (
	"bytes"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/dusan/page-report/web"
)

// repoURL is the canonical project repository, linked from the landing page.
const repoURL = "https://github.com/dusansimic/page-report"

// landingCSP: the landing page carries its own inline stylesheet and no
// scripts at all. It shares this origin with attacker-controlled report HTML,
// so it denies everything else, including framing and off-origin form posts.
const landingCSP = "default-src 'none'; style-src 'unsafe-inline'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

var landingTmpl = template.Must(template.ParseFS(web.TemplatesFS,
	"templates/pages_landing.html.tmpl"))

// landingView is the pages-domain landing page's data model. Account is the
// signed-in email, or the login when the provider exposes no email.
type landingView struct {
	AppBaseURL string
	RepoURL    string
	SignedIn   bool
	Allowed    bool
	Account    string
}

// handleLanding serves the public pages-domain landing page. It is
// unauthenticated but login-aware: a valid session adds the account and a sign
// out control. The allowlist can shrink after a session is minted, so a signed
// in visitor is told when their account can no longer open reports.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	v := landingView{
		AppBaseURL: strings.TrimRight(s.cfg.AppBaseURL, "/"),
		RepoURL:    repoURL,
	}
	if identity, ok := s.sessions.Identity(r); ok {
		v.SignedIn = true
		v.Allowed = s.allow.Match(identity)
		v.Account = identity.Email
		if v.Account == "" {
			v.Account = identity.Login // GitHub identities may have no email
		}
	}

	// Render into a buffer so a template error can still produce a 500.
	var buf bytes.Buffer
	if err := landingTmpl.Execute(&buf, v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", landingCSP)
	h.Set("Referrer-Policy", "no-referrer")
	// The body embeds the signed-in identity: never let a cache share it.
	h.Set("Cache-Control", "no-store")
	h.Set("Vary", "Cookie")
	w.Write(buf.Bytes())
}
