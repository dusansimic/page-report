package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dusan/page-report/internal/store"
)

// pageCSP allows inline style/script inside reports but blocks all external
// loads and framing; reports must be self-contained.
const pageCSP = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'"

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
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

	h := w.Header()
	h.Set("Content-Type", p.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(p.Content)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pageCSP)
	h.Set("Referrer-Policy", "no-referrer")
	w.Write(p.Content)
}
