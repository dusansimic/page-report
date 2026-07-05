// Package auth handles both authentication surfaces: human web login via goth
// (pages domain) and CLI bearer-token validation (app-domain API).
package auth

import "context"

// Identity is a normalized authenticated principal from either provider.
type Identity struct {
	// Subject is the stable IdP subject (OIDC sub or GitHub user id/login).
	Subject string
	// Email is set for OIDC; may be empty for GitHub.
	Email string
	// Login is the GitHub login; empty for OIDC.
	Login string
}

// TokenValidator validates a bearer token presented by the CLI and returns the
// identity it belongs to. The server acts as a resource server: OIDC tokens
// are verified as JWTs against the issuer's JWKS; GitHub tokens are opaque and
// verified by calling the GitHub API.
type TokenValidator interface {
	Validate(ctx context.Context, token string) (Identity, error)
}
