package client

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig places a config.yml in the XDG config dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigFromFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PR_SERVER_URL", "")
	writeConfig(t, "server_url: https://app.example.org/\n")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://app.example.org" {
		t.Fatalf("ServerURL = %q, want trailing slash trimmed", cfg.ServerURL)
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfig(t, "server_url: https://file.example.org\n")
	t.Setenv("PR_SERVER_URL", "https://env.example.org")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://env.example.org" {
		t.Fatalf("ServerURL = %q, want the env value", cfg.ServerURL)
	}
}

func TestLoadConfigEnvWithoutFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PR_SERVER_URL", "https://env.example.org/")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://env.example.org" {
		t.Fatalf("ServerURL = %q, want the env value with trailing slash trimmed", cfg.ServerURL)
	}
}

func TestLoadConfigMissingFileIsFine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PR_SERVER_URL", "")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("missing config file must not error, got %v", err)
	}
	if cfg.ServerURL != "" {
		t.Fatalf("ServerURL = %q, want empty", cfg.ServerURL)
	}
}

func TestLoadConfigExplicitPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PR_SERVER_URL", "")

	path := filepath.Join(t.TempDir(), "alt.yml")
	if err := os.WriteFile(path, []byte("server_url: https://alt.example.org\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://alt.example.org" {
		t.Fatalf("ServerURL = %q, want the explicit file value", cfg.ServerURL)
	}

	if _, err := LoadConfig(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("explicit missing path must error")
	}
}
