package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
)

// AuthConfig holds the identity-provider settings the server hands out via
// GetAuthConfig.
type AuthConfig struct {
	Provider       string // "github" or "oidc"
	Issuer         string // OIDC issuer URL (empty for github)
	ClientID       string
	Scopes         []string
	DeviceEndpoint string
	TokenEndpoint  string
}

// AuthConfigFromProto converts a GetAuthConfig response into an AuthConfig.
func AuthConfigFromProto(r *pagereportv1.GetAuthConfigResponse) AuthConfig {
	return AuthConfig{
		Provider:       r.GetProvider(),
		Issuer:         r.GetIssuer(),
		ClientID:       r.GetClientId(),
		Scopes:         r.GetScopes(),
		DeviceEndpoint: r.GetDeviceEndpoint(),
		TokenEndpoint:  r.GetTokenEndpoint(),
	}
}

// DeviceLogin runs the RFC 8628 device authorization flow. It requests a
// device code, invokes promptFn with the verification URI and user code
// (completeURI may be empty if the provider does not supply one), and then
// polls the token endpoint until the user approves or ctx is done.
//
// x/oauth2 handles the polling interval and slow_down responses. It also
// handles GitHub's token endpoint, which responds form-encoded unless asked
// for JSON: the library sends form-encoded requests and parses both formats.
//
// For OIDC providers the ID token, if issued, is available via
// token.Extra("id_token").
func DeviceLogin(ctx context.Context, cfg AuthConfig, promptFn func(verificationURI, userCode, completeURI string)) (*oauth2.Token, error) {
	oc := &oauth2.Config{
		ClientID: cfg.ClientID,
		Scopes:   cfg.Scopes,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: cfg.DeviceEndpoint,
			TokenURL:      cfg.TokenEndpoint,
		},
	}
	da, err := oc.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("device authorization request: %w", err)
	}
	if promptFn != nil {
		promptFn(da.VerificationURI, da.UserCode, da.VerificationURIComplete)
	}
	tok, err := oc.DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, fmt.Errorf("waiting for device authorization: %w", err)
	}
	return tok, nil
}

// CredentialsFromToken builds storable credentials from a freshly granted
// token. The OIDC id_token, when present, is preserved because it is the
// bearer the server expects from OIDC-authenticated clients.
func CredentialsFromToken(serverURL string, cfg AuthConfig, tok *oauth2.Token) Credentials {
	idToken, _ := tok.Extra("id_token").(string)
	return Credentials{
		ServerURL:     serverURL,
		Provider:      cfg.Provider,
		ClientID:      cfg.ClientID,
		TokenEndpoint: cfg.TokenEndpoint,
		AccessToken:   tok.AccessToken,
		IDToken:       idToken,
		RefreshToken:  tok.RefreshToken,
		Expiry:        tok.Expiry,
	}
}

// ParseDuration parses a duration like time.ParseDuration but additionally
// accepts a "d" (days) suffix, e.g. "30d" = 720h. The result must be
// positive.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("empty duration")
	}
	var d time.Duration
	if strings.HasSuffix(s, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 30d or 720h)", s)
		}
		d = time.Duration(days * 24 * float64(time.Hour))
	} else {
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 30d or 720h)", s)
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}
