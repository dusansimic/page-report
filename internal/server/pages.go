package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dusan/page-report/internal/store"
)

// pageCSP quarantines report HTML. `sandbox` without allow-same-origin puts
// the document in an opaque origin: it cannot read document.cookie, other
// reports, or the login page it shares a host with, and it cannot register a
// service worker. Omitting allow-scripts blocks JS outright — reports are
// self-contained static HTML by contract. `'self'` is deliberately absent from
// every directive, since an opaque origin matches it against nothing.
// allow-popups keeps target="_blank" links working.
const pageCSP = "sandbox allow-popups; " +
	"default-src 'none'; style-src 'unsafe-inline'; img-src data:; " +
	"font-src data:; form-action 'none'; base-uri 'none'; frame-ancestors 'none'"

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if denyScriptedFetch(w, r) {
		return
	}
	identity, ok := s.sessions.Identity(r)
	if !ok {
		http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
		return
	}
	if !s.allow.Match(identity) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	p, err := s.store.GetPage(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Rows written before the upload allowlist existed can hold anything, so
	// never echo a stored content type unvetted: degrade to plain text instead.
	contentType, ok := canonicalContentType(p.ContentType)
	if !ok {
		contentType = contentTypeText
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(p.Content)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pageCSP)
	h.Set("Referrer-Policy", "no-referrer")
	w.Write(p.Content)
}
