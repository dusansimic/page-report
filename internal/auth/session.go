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

// Session is a decoded, still-valid session cookie.
type Session struct {
	Identity Identity
	// AuthTime is when the user last completed the OAuth flow. It is set at
	// login and never refreshed by ordinary requests, so it measures the age
	// of the authentication rather than of the cookie. Minting a CLI token
	// checks it: see config.TokenMintReauthWindow.
	AuthTime time.Time
}

// SessionManager manages the session cookie.
type SessionManager struct {
	// Store is exported so SetupGoth can reuse it as gothic.Store.
	Store *sessions.CookieStore
	ttl   time.Duration
}

// NewSessionManager builds a cookie-backed session store from the config.
//
// The cookie is host-only (no Domain attribute), HttpOnly, and Secure unless
// running in dev mode. SameSite is Lax, deliberately, not Strict: report links
// get opened from chat clients and other external contexts, and under Strict
// the cookie would be withheld on that first top-level navigation, showing a
// logged-out page until the user reloaded. Lax still withholds the cookie from
// cross-site POST, which is what every Connect RPC is, so CSRF protection is
// not weakened by the choice.
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

// Get decodes the session cookie. It returns false for missing, malformed or
// expired sessions.
func (m *SessionManager) Get(r *http.Request) (Session, bool) {
	sess, err := m.Store.Get(r, SessionName)
	if err != nil || sess.IsNew {
		return Session{}, false
	}
	exp, ok := sess.Values["exp"].(int64)
	if !ok || time.Now().Unix() >= exp {
		return Session{}, false
	}
	sub, _ := sess.Values["sub"].(string)
	if sub == "" {
		return Session{}, false
	}
	email, _ := sess.Values["email"].(string)
	login, _ := sess.Values["login"].(string)
	out := Session{Identity: Identity{Subject: sub, Email: email, Login: login}}
	// Sessions minted before auth_time existed leave it zero, which reads as
	// "authenticated long ago" — the safe direction for the mint check.
	if at, ok := sess.Values["auth_time"].(int64); ok {
		out.AuthTime = time.Unix(at, 0).UTC()
	}
	return out, true
}

// Identity returns just the identity from the session cookie. This satisfies
// the server package's SessionReader interface.
func (m *SessionManager) Identity(r *http.Request) (Identity, bool) {
	sess, ok := m.Get(r)
	return sess.Identity, ok
}

// Save writes the identity into a fresh session cookie with the configured
// TTL, stamping the authentication time. Only the login callback calls this,
// so auth_time tracks real authentications and is not refreshed by traffic.
func (m *SessionManager) Save(w http.ResponseWriter, r *http.Request, id Identity) error {
	sess, err := m.Store.New(r, SessionName)
	if err != nil {
		// A decode error on an existing cookie still yields a usable new
		// session; only fail if we got no session at all.
		if sess == nil {
			return fmt.Errorf("new session: %w", err)
		}
	}
	now := time.Now()
	sess.Values["sub"] = id.Subject
	sess.Values["email"] = id.Email
	sess.Values["login"] = id.Login
	sess.Values["exp"] = now.Add(m.ttl).Unix()
	sess.Values["auth_time"] = now.Unix()
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
