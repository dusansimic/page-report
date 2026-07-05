package auth

import (
	"net/http"
	"strings"

	"github.com/markbates/goth/gothic"
)

// nextCookie stashes the post-login redirect target across the OAuth dance.
const nextCookie = "pr_next"

// Handlers returns the /auth/* HTTP handlers. Patterns are registered with
// full paths so the mux works whether it is mounted at "/auth/" or used as
// the root handler.
func Handlers(m *SessionManager, allow *Allowlist) http.Handler {
	mux := http.NewServeMux()
	secure := m.Store.Options != nil && m.Store.Options.Secure

	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		if next := sanitizeNext(r.URL.Query().Get("next")); next != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     nextCookie,
				Value:    next,
				Path:     "/",
				MaxAge:   600,
				HttpOnly: true,
				Secure:   secure,
				SameSite: http.SameSiteLaxMode,
			})
		}
		gothic.BeginAuthHandler(w, r)
	})

	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		user, err := gothic.CompleteUserAuth(w, r)
		if err != nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		id := Identity{Subject: user.UserID, Email: user.Email, Login: user.NickName}
		if !allow.Match(id) {
			http.Error(w, "forbidden: not on the allowlist", http.StatusForbidden)
			return
		}
		if err := m.Save(w, r, id); err != nil {
			http.Error(w, "failed to establish session", http.StatusInternalServerError)
			return
		}

		next := "/"
		if c, err := r.Cookie(nextCookie); err == nil {
			if v := sanitizeNext(c.Value); v != "" {
				next = v
			}
			http.SetCookie(w, &http.Cookie{
				Name:     nextCookie,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   secure,
				SameSite: http.SameSiteLaxMode,
			})
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	})

	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		m.Clear(w, r)
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}

// sanitizeNext accepts only same-origin absolute paths: must start with "/",
// must not be scheme-relative ("//...") and must not contain backslashes.
// Anything else returns "" (callers default to "/").
func sanitizeNext(s string) string {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.Contains(s, "\\") {
		return ""
	}
	return s
}
