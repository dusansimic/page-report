// Command pr-server runs the page-report HTTP service: a public app domain
// (homepage + ConnectRPC API) and an auth-gated pages domain serving stored
// HTML reports.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dusan/page-report/internal/auth"
	"github.com/dusan/page-report/internal/config"
	"github.com/dusan/page-report/internal/server"
	"github.com/dusan/page-report/internal/store"
)

// authConfigAdapter bridges auth.DeviceAuthConfig to server.AuthConfigProvider.
type authConfigAdapter struct {
	dac auth.DeviceAuthConfig
}

func (a authConfigAdapter) AuthConfig() server.AuthConfig {
	return server.AuthConfig{
		Provider:       a.dac.Provider,
		Issuer:         a.dac.Issuer,
		ClientID:       a.dac.ClientID,
		Scopes:         a.dac.Scopes,
		DeviceEndpoint: a.dac.DeviceEndpoint,
		TokenEndpoint:  a.dac.TokenEndpoint,
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to YAML config file (default: ./config.yml if present)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var validator auth.TokenValidator
	switch cfg.Provider {
	case config.ProviderGitHub:
		validator = auth.NewGitHubValidator()
	case config.ProviderOIDC:
		validator, err = auth.NewOIDCValidator(ctx, cfg)
		if err != nil {
			return fmt.Errorf("init oidc validator: %w", err)
		}
	}

	dac, err := auth.NewDeviceAuthConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("resolve device auth config: %w", err)
	}

	sessions := auth.NewSessionManager(cfg)
	if err := auth.SetupGoth(cfg, sessions); err != nil {
		return err
	}
	allow := auth.NewAllowlist(cfg.Allowlist)

	srv := server.New(cfg, st, validator, allow, sessions,
		authConfigAdapter{dac}, auth.Handlers(sessions, allow))

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s (app: %s, pages: %s)",
			cfg.ListenAddr, cfg.AppBaseURL, cfg.PagesBaseURL)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Print("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
