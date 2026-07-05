package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/openidConnect"

	"github.com/dusan/page-report/internal/config"
)

// SetupGoth registers exactly one goth provider based on cfg.Provider, points
// gothic at the app's session store and pins the provider name so goth does
// not require a ?provider= URL parameter.
func SetupGoth(cfg *config.Config, m *SessionManager) error {
	callbackURL := strings.TrimRight(cfg.PagesBaseURL, "/") + "/auth/callback"

	var p goth.Provider
	switch cfg.Provider {
	case config.ProviderGitHub:
		p = github.New(cfg.GitHub.ClientID, cfg.GitHub.ClientSecret, callbackURL,
			"read:user", "user:email")
	case config.ProviderOIDC:
		discoveryURL := strings.TrimRight(cfg.OIDC.Issuer, "/") + "/.well-known/openid-configuration"
		oidcProvider, err := openidConnect.New(cfg.OIDC.ClientID, cfg.OIDC.ClientSecret,
			callbackURL, discoveryURL, "openid", "email", "profile")
		if err != nil {
			return fmt.Errorf("configure oidc provider: %w", err)
		}
		p = oidcProvider
	default:
		return fmt.Errorf("unsupported auth provider %q", cfg.Provider)
	}

	goth.UseProviders(p)
	gothic.Store = m.Store
	name := p.Name()
	gothic.GetProviderName = func(*http.Request) (string, error) { return name, nil }
	return nil
}
