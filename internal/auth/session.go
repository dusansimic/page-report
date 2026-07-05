package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/sessions"

	"github.com/dusan/page-report/internal/config"
)

// SessionName is the cookie name for the human web session.
const SessionName = "pr_session"

// SessionManager manages the app session cookie on the pages domain.
type SessionManager struct {
	// Store is exported so SetupGoth can reuse it as gothic.Store.
	Store *sessions.CookieStore
	ttl   time.Duration
}

// NewSessionManager builds a cookie-backed session store from the config.
// The cookie is host-only (no Domain attribute), HttpOnly, SameSite=Lax and
// Secure unless running in dev mode.
func NewSessionManager(cfg *config.Config) *SessionManager {
	store := sessions.NewCookieStore([]byte(cfg.SessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   int(cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   !cfg.Dev,
		SameSite: http.SameSiteLaxMode,
	}
	return &SessionManager{Store: store, ttl: cfg.SessionTTL}
}

// Identity decodes the session cookie and returns the identity stored in it.
// It returns false for missing, malformed or expired sessions. This satisfies
// the server package's SessionReader interface.
func (m *SessionManager) Identity(r *http.Request) (Identity, bool) {
	sess, err := m.Store.Get(r, SessionName)
	if err != nil || sess.IsNew {
		return Identity{}, false
	}
	exp, ok := sess.Values["exp"].(int64)
	if !ok || time.Now().Unix() >= exp {
		return Identity{}, false
	}
	sub, _ := sess.Values["sub"].(string)
	if sub == "" {
		return Identity{}, false
	}
	email, _ := sess.Values["email"].(string)
	login, _ := sess.Values["login"].(string)
	return Identity{Subject: sub, Email: email, Login: login}, true
}

// Save writes the identity into a fresh session cookie with the configured TTL.
func (m *SessionManager) Save(w http.ResponseWriter, r *http.Request, id Identity) error {
	sess, err := m.Store.New(r, SessionName)
	if err != nil {
		// A decode error on an existing cookie still yields a usable new
		// session; only fail if we got no session at all.
		if sess == nil {
			return fmt.Errorf("new session: %w", err)
		}
	}
	sess.Values["sub"] = id.Subject
	sess.Values["email"] = id.Email
	sess.Values["login"] = id.Login
	sess.Values["exp"] = time.Now().Add(m.ttl).Unix()
	if err := sess.Save(r, w); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// Clear expires the session cookie.
func (m *SessionManager) Clear(w http.ResponseWriter, r *http.Request) {
	sess, err := m.Store.Get(r, SessionName)
	if err != nil && sess == nil {
		return
	}
	sess.Options.MaxAge = -1
	sess.Values = map[any]any{}
	_ = sess.Save(r, w)
}
