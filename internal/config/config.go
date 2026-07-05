// Package config loads server configuration from a YAML file with PR_*
// environment-variable overrides via viper.
package config

import (
	"errors"
	"fmt"
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
	// Audience is the expected aud claim on CLI tokens. Defaults to ClientID.
	Audience string `mapstructure:"audience"`
}

type Config struct {
	ListenAddr     string        `mapstructure:"listen_addr"`
	AppBaseURL     string        `mapstructure:"app_base_url"`
	PagesBaseURL   string        `mapstructure:"pages_base_url"`
	DBPath         string        `mapstructure:"db_path"`
	SessionSecret  string        `mapstructure:"session_secret"`
	SessionTTL     time.Duration `mapstructure:"session_ttl"`
	MaxUploadBytes int64         `mapstructure:"max_upload_bytes"`
	Provider       ProviderType  `mapstructure:"provider"`
	Allowlist      []string      `mapstructure:"allowlist"`
	GitHub         GitHubConfig  `mapstructure:"github"`
	OIDC           OIDCConfig    `mapstructure:"oidc"`
	// Dev disables the https requirement on base URLs for local runs.
	Dev bool `mapstructure:"dev"`
}

// keys lists every config key so each can be explicitly bound to its PR_* env
// var; viper's AutomaticEnv alone does not surface env-only nested keys during
// Unmarshal.
var keys = []string{
	"listen_addr",
	"app_base_url",
	"pages_base_url",
	"db_path",
	"session_secret",
	"session_ttl",
	"max_upload_bytes",
	"provider",
	"allowlist",
	"dev",
	"github.client_id",
	"github.client_secret",
	"oidc.issuer",
	"oidc.client_id",
	"oidc.client_secret",
	"oidc.audience",
}

// Load reads configuration from the given YAML file (optional; empty path or a
// missing default file is fine) and applies PR_* environment overrides.
func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetDefault("listen_addr", ":8080")
	v.SetDefault("db_path", "page-report.db")
	v.SetDefault("session_ttl", "24h")
	v.SetDefault("max_upload_bytes", 5*1024*1024)

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
	// Allowlist may arrive as a single comma-separated env value.
	if len(cfg.Allowlist) == 1 && strings.Contains(cfg.Allowlist[0], ",") {
		parts := strings.Split(cfg.Allowlist[0], ",")
		cfg.Allowlist = cfg.Allowlist[:0]
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				cfg.Allowlist = append(cfg.Allowlist, p)
			}
		}
	}
	if cfg.Provider == ProviderOIDC && cfg.OIDC.Audience == "" {
		cfg.OIDC.Audience = cfg.OIDC.ClientID
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	appHost := checkBaseURL(&errs, "app_base_url", c.AppBaseURL, c.Dev)
	pagesHost := checkBaseURL(&errs, "pages_base_url", c.PagesBaseURL, c.Dev)
	if appHost != "" && appHost == pagesHost {
		add("app_base_url and pages_base_url must be different hosts (got %s)", appHost)
	}

	if len(c.SessionSecret) < 32 {
		add("session_secret must be at least 32 bytes")
	}
	if len(c.Allowlist) == 0 {
		add("allowlist must not be empty")
	}
	if c.MaxUploadBytes <= 0 {
		add("max_upload_bytes must be positive")
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

// PageURL builds the public URL for a page id on the pages domain.
func (c *Config) PageURL(id string) string {
	return strings.TrimRight(c.PagesBaseURL, "/") + "/p/" + id
}

func checkBaseURL(errs *[]string, name, raw string, dev bool) (host string) {
	if raw == "" {
		*errs = append(*errs, name+" is required")
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		*errs = append(*errs, name+" must be a valid absolute URL")
		return ""
	}
	if !dev && u.Scheme != "https" {
		*errs = append(*errs, name+" must use https (set dev: true for local runs)")
	}
	return u.Host
}
