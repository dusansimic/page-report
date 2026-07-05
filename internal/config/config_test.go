package config

import (
	"os"
	"path/filepath"
	"testing"
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
app_base_url: https://app.example.org
pages_base_url: https://pages.example.org
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
	if cfg.OIDC.Audience != "cid" {
		t.Errorf("audience must default to client_id, got %q", cfg.OIDC.Audience)
	}
	if got := cfg.PageURL("x1"); got != "https://pages.example.org/p/x1" {
		t.Errorf("PageURL = %q", got)
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

func TestValidationFailures(t *testing.T) {
	cases := map[string]string{
		"same hosts": `
app_base_url: https://same.example.org
pages_base_url: https://same.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
		"short secret": `
app_base_url: https://app.example.org
pages_base_url: https://pages.example.org
session_secret: short
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
		"http without dev": `
app_base_url: http://app.example.org
pages_base_url: https://pages.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: oidc
allowlist: [me@example.org]
oidc: {issuer: https://idp.example.org, client_id: c, client_secret: s}
`,
		"bad provider": `
app_base_url: https://app.example.org
pages_base_url: https://pages.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: nope
allowlist: [me@example.org]
`,
		"github missing creds": `
app_base_url: https://app.example.org
pages_base_url: https://pages.example.org
session_secret: 0123456789abcdef0123456789abcdef
provider: github
allowlist: [me@example.org]
`,
		"empty allowlist": `
app_base_url: https://app.example.org
pages_base_url: https://pages.example.org
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
