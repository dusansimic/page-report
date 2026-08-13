// Package config loads server configuration from a YAML file with PR_*
// environment-variable overrides via viper.
package config

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type ProviderType string

const (
	ProviderGitHub ProviderType = "github"
	ProviderOIDC   ProviderType = "oidc"
)

type GitHubConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

type OIDCConfig struct {
	Issuer       string `mapstructure:"issuer"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

type Config struct {
	ListenAddr string `mapstructure:"listen_addr"`
	// BaseURL is the single public origin: SPA, API, auth and report pages are
	// all served from it.
	BaseURL string `mapstructure:"base_url"`
	// AppBaseURL is the pre-single-origin name for BaseURL, accepted for one
	// release so an existing deployment still boots after upgrading. See Load.
	AppBaseURL string `mapstructure:"app_base_url"`

	DBPath         string        `mapstructure:"db_path"`
	SessionSecret  string        `mapstructure:"session_secret"`
	SessionTTL     time.Duration `mapstructure:"session_ttl"`
	MaxUploadBytes int64         `mapstructure:"max_upload_bytes"`
	Provider       ProviderType  `mapstructure:"provider"`
	Allowlist      []string      `mapstructure:"allowlist"`
	GitHub         GitHubConfig  `mapstructure:"github"`
	OIDC           OIDCConfig    `mapstructure:"oidc"`

	// TokenDefaultTTL is the expiry preselected in the dashboard for new CLI
	// tokens. Zero means the default offered is "never expires".
	TokenDefaultTTL time.Duration `mapstructure:"token_default_ttl"`
	// TokenMintReauthWindow is how recently a session must have authenticated
	// before it may mint or rotate a CLI token. A token outlives the session
	// that created it and works from anywhere, so a stale session should not
	// be able to silently produce one. Zero disables the check.
	TokenMintReauthWindow time.Duration `mapstructure:"token_mint_reauth_window"`

	// Dev disables the https requirement on the base URL for local runs.
	Dev bool `mapstructure:"dev"`
}

// keys lists every config key so each can be explicitly bound to its PR_* env
// var; viper's AutomaticEnv alone does not surface env-only nested keys during
// Unmarshal.
var keys = []string{
	"listen_addr",
	"base_url",
	"app_base_url",
	"db_path",
	"session_secret",
	"session_ttl",
	"max_upload_bytes",
	"provider",
	"allowlist",
	"token_default_ttl",
	"token_mint_reauth_window",
	"dev",
	"github.client_id",
	"github.client_secret",
	"oidc.issuer",
	"oidc.client_id",
	"oidc.client_secret",
}

// Load reads configuration from the given YAML file (optional; empty path or a
// missing default file is fine) and applies PR_* environment overrides.
func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetDefault("listen_addr", ":8080")
	v.SetDefault("db_path", "page-report.db")
	v.SetDefault("session_ttl", "24h")
	v.SetDefault("max_upload_bytes", 5*1024*1024)
	v.SetDefault("token_mint_reauth_window", "10m")

	v.SetEnvPrefix("PR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, k := range keys {
		if err := v.BindEnv(k); err != nil {
			return nil, fmt.Errorf("bind env for %s: %w", k, err)
		}
	}

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				return nil, fmt.Errorf("read config: %w", err)
			}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Deprecation shim: the two-domain split collapsed into one origin, so
	// app_base_url became base_url and pages_base_url disappeared. Keep an
	// existing config booting rather than failing on an unset key.
	if cfg.BaseURL == "" && cfg.AppBaseURL != "" {
		log.Printf("config: app_base_url is deprecated, rename it to base_url " +
			"(pages_base_url is no longer used: reports are served from the same origin)")
		cfg.BaseURL = cfg.AppBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	// An env-provided allowlist arrives as one comma-separated string, which
	// viper may additionally split on spaces; renormalize either shape.
	var allowlist []string
	for _, p := range strings.Split(strings.Join(cfg.Allowlist, ","), ",") {
		if p = strings.TrimSpace(p); p != "" {
			allowlist = append(allowlist, p)
		}
	}
	cfg.Allowlist = allowlist

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	checkBaseURL(&errs, "base_url", c.BaseURL, c.Dev)

	if len(c.SessionSecret) < 32 {
		add("session_secret must be at least 32 bytes")
	}
	if len(c.Allowlist) == 0 {
		add("allowlist must not be empty")
	}
	if c.MaxUploadBytes <= 0 {
		add("max_upload_bytes must be positive")
	}
	if c.TokenDefaultTTL < 0 {
		add("token_default_ttl must not be negative")
	}
	if c.TokenMintReauthWindow < 0 {
		add("token_mint_reauth_window must not be negative")
	}

	switch c.Provider {
	case ProviderGitHub:
		if c.GitHub.ClientID == "" || c.GitHub.ClientSecret == "" {
			add("provider github requires github.client_id and github.client_secret")
		}
	case ProviderOIDC:
		if c.OIDC.Issuer == "" || c.OIDC.ClientID == "" || c.OIDC.ClientSecret == "" {
			add("provider oidc requires oidc.issuer, oidc.client_id and oidc.client_secret")
		}
	default:
		add("provider must be %q or %q (got %q)", ProviderGitHub, ProviderOIDC, c.Provider)
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// PageURL builds the public URL for a page id. URLs are built only from
// base_url, never from a request's Host header.
func (c *Config) PageURL(id string) string {
	return c.BaseURL + "/p/" + id
}

// TokensURL is the dashboard page where a user mints CLI tokens. The CLI
// prints it during `page-report login`.
func (c *Config) TokensURL() string {
	return c.BaseURL + "/tokens"
}

// CallbackURL is the OAuth redirect target registered with the identity
// provider.
func (c *Config) CallbackURL() string {
	return c.BaseURL + "/auth/callback"
}

func checkBaseURL(errs *[]string, name, raw string, dev bool) {
	if raw == "" {
		*errs = append(*errs, name+" is required")
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		*errs = append(*errs, name+" must be a valid absolute URL")
		return
	}
	if !dev && u.Scheme != "https" {
		*errs = append(*errs, name+" must use https (set dev: true for local runs)")
	}
}
