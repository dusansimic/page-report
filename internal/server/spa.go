package server

import (
	"io/fs"
	"net/http"
	"strconv"

	"github.com/dusan/page-report/web"
)

// spaCSP governs the dashboard. It shares an origin with attacker-controlled
// report HTML, so it grants only what the SPA actually needs: its own bundle,
// its own styles, XHR back to itself, and GitHub avatars. Everything else is
// denied, including framing and off-origin form posts.
//
// Reports are served by handlePage under a completely different policy
// (pageCSP), which sandboxes them into an opaque origin. Both policies are
// per-response; do not hoist either into shared middleware.
const spaCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https://avatars.githubusercontent.com; connect-src 'self'; " +
	"font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// spaHandler serves the embedded React build.
type spaHandler struct {
	files fs.FS
	index []byte
}

func newSPAHandler() *spaHandler {
	h := &spaHandler{}
	sub, err := fs.Sub(web.AppFS, "app/dist")
	if err != nil {
		return h
	}
	h.files = sub
	// Missing index.html means the binary was built without running the
	// frontend build. That is a deployment mistake, not a reason to refuse to
	// start: the API and report serving still work.
	if data, err := fs.ReadFile(sub, "index.html"); err == nil {
		h.index = data
	}
	return h
}

func (h *spaHandler) built() bool { return len(h.index) > 0 }

// assets serves Vite's content-hashed bundle output. The hash is in the
// filename, so these can be cached indefinitely.
func (h *spaHandler) assets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.files == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.FileServerFS(h.files).ServeHTTP(w, r)
	})
}

// serveSPA is the history fallback: any path the client-side router owns gets
// the same HTML shell. Paths belonging to a real handler are refused instead,
// so a typo'd RPC or a stale client gets a 404 rather than a 200 full of HTML.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if isAPIPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.spa.built() {
		http.Error(w, "the web interface was not built into this binary "+
			"(run `pnpm --dir web/app build` before `go build`)",
			http.StatusServiceUnavailable)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(s.spa.index)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", spaCSP)
	h.Set("Referrer-Policy", "no-referrer")
	// The shell is identical for everyone, but it bootstraps a session-aware
	// app; never let an intermediary cache it alongside a different user.
	h.Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	w.Write(s.spa.index)
}
