package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TokenEnvVar overrides the stored credentials. Agents and CI use it to run
// without a login step.
const TokenEnvVar = "PR_TOKEN"

// Credentials is the on-disk shape of ~/.config/page-report/credentials.json.
// The server mints tokens itself now, so there is nothing to refresh and
// nothing provider-specific to remember: the file holds the token and the
// server it belongs to.
type Credentials struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

// ErrNotLoggedIn is returned when no stored credentials exist.
var ErrNotLoggedIn = errors.New("not logged in: run `page-report login` first")

// ErrLegacyCredentials is returned for a credentials file written by a version
// that authenticated through the identity provider's device flow. Those
// tokens are not accepted any more, and silently sending an empty bearer would
// surface as a confusing 401.
var ErrLegacyCredentials = errors.New(
	"credentials are from an older version that used device-flow login: " +
		"run `page-report login` to create a token in the web dashboard")

func credentialsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

func Save(creds Credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	// WriteFile does not change the mode of a pre-existing file.
	return os.Chmod(path, 0o600)
}

func Load() (Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return Credentials{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotLoggedIn
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials %s: %w", path, err)
	}
	if creds.Token == "" {
		// The file exists but carries no usable token: either the old
		// device-flow shape, or something truncated. Say so explicitly.
		return Credentials{}, ErrLegacyCredentials
	}
	return creds, nil
}

func Delete() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove credentials: %w", err)
	}
	return nil
}

// StoredTokenSource supplies the bearer token for authenticated RPCs. PR_TOKEN
// wins over the credentials file so a one-off or CI invocation does not need
// to touch the user's config.
type StoredTokenSource struct{}

func (StoredTokenSource) Token(context.Context) (string, error) {
	if tok := strings.TrimSpace(os.Getenv(TokenEnvVar)); tok != "" {
		return tok, nil
	}
	creds, err := Load()
	if err != nil {
		return "", err
	}
	return creds.Token, nil
}
