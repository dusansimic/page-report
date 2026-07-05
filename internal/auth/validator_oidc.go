package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/dusan/page-report/internal/config"
)

type oidcValidator struct {
	verifier *oidc.IDTokenVerifier
}

// NewOIDCValidator builds a TokenValidator that verifies CLI-presented JWTs
// against the issuer's JWKS (fetched via OIDC discovery). The expected
// audience is cfg.OIDC.Audience.
func NewOIDCValidator(ctx context.Context, cfg *config.Config) (TokenValidator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", cfg.OIDC.Issuer, err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.Audience})
	return &oidcValidator{verifier: verifier}, nil
}

func (v *oidcValidator) Validate(ctx context.Context, token string) (Identity, error) {
	idToken, err := v.verifier.Verify(ctx, token)
	if err != nil {
		return Identity{}, fmt.Errorf("verify oidc token: %w", err)
	}
	var claims struct {
		Email             string `json:"email"`
		Sub               string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("parse oidc claims: %w", err)
	}
	return Identity{
		Subject: claims.Sub,
		Email:   claims.Email,
		Login:   claims.PreferredUsername,
	}, nil
}
