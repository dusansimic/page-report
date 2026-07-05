package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dusan/page-report/internal/config"
)

// DeviceAuthConfig describes the OAuth device-authorization flow parameters
// the CLI needs. The server package adapts this to its own AuthConfigProvider
// interface with a small wrapper; this package deliberately does not import
// internal/server.
type DeviceAuthConfig struct {
	Provider       string
	Issuer         string
	ClientID       string
	Scopes         []string
	DeviceEndpoint string
	TokenEndpoint  string
}

// NewDeviceAuthConfig resolves the device-flow endpoints for the configured
// provider. For OIDC the issuer's discovery document is fetched; an error is
// returned if the IdP does not advertise a device authorization endpoint.
func NewDeviceAuthConfig(ctx context.Context, cfg *config.Config) (DeviceAuthConfig, error) {
	switch cfg.Provider {
	case config.ProviderGitHub:
		return DeviceAuthConfig{
			Provider:       string(config.ProviderGitHub),
			ClientID:       cfg.GitHub.ClientID,
			Scopes:         []string{"read:user"},
			DeviceEndpoint: "https://github.com/login/device/code",
			TokenEndpoint:  "https://github.com/login/oauth/access_token",
		}, nil
	case config.ProviderOIDC:
		doc, err := fetchDiscovery(ctx, cfg.OIDC.Issuer)
		if err != nil {
			return DeviceAuthConfig{}, err
		}
		if doc.DeviceAuthorizationEndpoint == "" {
			return DeviceAuthConfig{}, fmt.Errorf(
				"oidc issuer %s does not advertise a device_authorization_endpoint (device flow unsupported)",
				cfg.OIDC.Issuer)
		}
		if doc.TokenEndpoint == "" {
			return DeviceAuthConfig{}, fmt.Errorf(
				"oidc issuer %s does not advertise a token_endpoint", cfg.OIDC.Issuer)
		}
		return DeviceAuthConfig{
			Provider:       string(config.ProviderOIDC),
			Issuer:         cfg.OIDC.Issuer,
			ClientID:       cfg.OIDC.ClientID,
			Scopes:         []string{"openid", "email", "profile", "offline_access"},
			DeviceEndpoint: doc.DeviceAuthorizationEndpoint,
			TokenEndpoint:  doc.TokenEndpoint,
		}, nil
	default:
		return DeviceAuthConfig{}, fmt.Errorf("unsupported auth provider %q", cfg.Provider)
	}
}

type discoveryDoc struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

func fetchDiscovery(ctx context.Context, issuer string) (discoveryDoc, error) {
	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return discoveryDoc{}, fmt.Errorf("build discovery request: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return discoveryDoc{}, fmt.Errorf("fetch oidc discovery %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return discoveryDoc{}, fmt.Errorf("fetch oidc discovery %s: status %d", url, resp.StatusCode)
	}
	var doc discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return discoveryDoc{}, fmt.Errorf("decode oidc discovery %s: %w", url, err)
	}
	return doc, nil
}
