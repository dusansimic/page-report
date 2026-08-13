package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validYAML = `
base_url: https://reports.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc:
  issuer: https://idp.example.org
  client_id: cid
  client_secret: secret
`

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("default listen_addr = %q", cfg.ListenAddr)
	}
	if cfg.TokenMintReauthWindow != 10*time.Minute {
		t.Errorf("default token_mint_reauth_window = %v, want 10m", cfg.TokenMintReauthWindow)
	}
	if got := cfg.PageURL("x1"); got != "https://reports.example.org/p/x1" {
		t.Errorf("PageURL = %q", got)
	}
	if got := cfg.TokensURL(); got != "https://reports.example.org/tokens" {
		t.Errorf("TokensURL = %q", got)
	}
	if got := cfg.CallbackURL(); got != "https://reports.example.org/auth/callback" {
		t.Errorf("CallbackURL = %q", got)
	}
}

// A trailing slash on base_url must not produce "//p/id" in every page URL.
func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	cfg, err := Load(writeConfig(t, strings.Replace(validYAML,
		"https://reports.example.org", "https://reports.example.org/", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.PageURL("x1"); got != "https://reports.example.org/p/x1" {
		t.Errorf("PageURL = %q", got)
	}
}

// Deployments upgrading from the two-domain layout still have app_base_url set
// and no base_url. They must keep booting for one release.
func TestAppBaseURLDeprecationShim(t *testing.T) {
	yaml := `
app_base_url: https://reports.example.org
pages_base_url: https://pages.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://reports.example.org" {
		t.Errorf("shim did not adopt app_base_url: %q", cfg.BaseURL)
	}
	// An unknown key must be ignored, not rejected: pages_base_url is gone.
	if got := cfg.PageURL("x1"); got != "https://reports.example.org/p/x1" {
		t.Errorf("PageURL = %q, want the single origin", got)
	}
}

// base_url wins when both are present, so a half-migrated config does not
// silently keep using the old value.
func TestBaseURLBeatsDeprecatedKey(t *testing.T) {
	yaml := `
base_url: https://new.example.org
app_base_url: https://old.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://new.example.org" {
		t.Errorf("BaseURL = %q, want the base_url value", cfg.BaseURL)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("PR_OIDC_CLIENT_ID", "env-cid")
	t.Setenv("PR_ALLOWLIST", "a@example.org, b@example.org")
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDC.ClientID != "env-cid" {
		t.Errorf("env must override file: client_id = %q", cfg.OIDC.ClientID)
	}
	if len(cfg.Allowlist) != 2 || cfg.Allowlist[1] != "b@example.org" {
		t.Errorf("comma-separated env allowlist parsed wrong: %v", cfg.Allowlist)
	}
}

func TestEnvBaseURL(t *testing.T) {
	t.Setenv("PR_BASE_URL", "https://env.example.org")
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://env.example.org" {
		t.Errorf("PR_BASE_URL ignored: %q", cfg.BaseURL)
	}
}

func TestValidationFailures(t *testing.T) {
	cases := map[string]string{
		"missing base url": `
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
		"short secret": `
base_url: https://reports.example.org
session_secret: short
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
		"http without dev": `
base_url: http://reports.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
		"bad provider": `
base_url: https://reports.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: nope
allowlist: [me@example.org]
`,
		"github missing creds": `
base_url: https://reports.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: github
allowlist: [me@example.org]
`,
		"empty allowlist": `
base_url: https://reports.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
	}
	for name, yaml := range cases {
		if _, err := Load(writeConfig(t, yaml)); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

// http:// is only allowed with dev: true, which is how the vite dev server is
// pointed at.
func TestDevAllowsHTTP(t *testing.T) {
	yaml := `
dev: true
base_url: http://localhost:5173
session_secret: 0123456789abcdef0123456789abcdef
provider: github
allowlist: [me]
github: {client_id: c, client_secret: s}
`
	if _, err := Load(writeConfig(t, yaml)); err != nil {
		t.Fatalf("dev mode must accept http: %v", err)
	}
}
