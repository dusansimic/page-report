package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
)

// Credentials is the on-disk shape of ~/.config/page-report/credentials.json.
type Credentials struct {
	ServerURL     string    `json:"server_url"`
	Provider      string    `json:"provider"`
	ClientID      string    `json:"client_id"`
	TokenEndpoint string    `json:"token_endpoint"`
	AccessToken   string    `json:"access_token"`
	IDToken       string    `json:"id_token,omitempty"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	Expiry        time.Time `json:"expiry,omitempty"`
}

// ErrNotLoggedIn is returned when no stored credentials exist.
var ErrNotLoggedIn = errors.New("not logged in: run `pr login` first")

func credentialsPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "page-report", "credentials.json"), nil
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

// StoredTokenSource implements TokenSource on top of the credentials file.
// For OIDC it refreshes the token when it is about to expire and persists the
// rotated credentials; the bearer sent to the server is the id_token (falling
// back to the access token). For GitHub the long-lived access token is
// returned as-is.
type StoredTokenSource struct{}

func (StoredTokenSource) Token(ctx context.Context) (string, error) {
	creds, err := Load()
	if err != nil {
		return "", err
	}

	if creds.Provider == "oidc" && !creds.Expiry.IsZero() &&
		time.Until(creds.Expiry) < time.Minute {
		if creds.RefreshToken == "" {
			return "", errors.New("token expired and no refresh token stored: run `pr login`")
		}
		creds, err = refresh(ctx, creds)
		if err != nil {
			return "", fmt.Errorf("token refresh failed (run `pr login`): %w", err)
		}
	}

	if creds.Provider == "oidc" && creds.IDToken != "" {
		return creds.IDToken, nil
	}
	return creds.AccessToken, nil
}

func refresh(ctx context.Context, creds Credentials) (Credentials, error) {
	oc := &oauth2.Config{
		ClientID: creds.ClientID,
		Endpoint: oauth2.Endpoint{TokenURL: creds.TokenEndpoint},
	}
	stale := &oauth2.Token{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		Expiry:       creds.Expiry,
	}
	fresh, err := oc.TokenSource(ctx, stale).Token()
	if err != nil {
		return Credentials{}, err
	}
	creds.AccessToken = fresh.AccessToken
	creds.Expiry = fresh.Expiry
	if rt := fresh.RefreshToken; rt != "" {
		creds.RefreshToken = rt
	}
	if idt, _ := fresh.Extra("id_token").(string); idt != "" {
		creds.IDToken = idt
	}
	if err := Save(creds); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}
