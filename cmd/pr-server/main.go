// Command pr-server runs the page-report HTTP service: one origin serving the
// React dashboard, the ConnectRPC APIs, OAuth login, and the sandboxed report
// pages at /p/{id}.
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

// Build metadata, injected via -ldflags "-X main.version=..." by the release
// workflow (and by the Dockerfile's VERSION/COMMIT/DATE build args). Without
// ldflags these keep their dev defaults.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// tokenStore adapts store.Store to the narrow interface the token validator
// needs. The adapter lives here so internal/auth stays free of a dependency on
// the persistence layer.
type tokenStore struct {
	st store.Store
}

func (t tokenStore) GetToken(ctx context.Context, id string) (auth.TokenRecord, error) {
	rec, err := t.st.GetToken(ctx, id)
	if err != nil {
		return auth.TokenRecord{}, err
	}
	return auth.TokenRecord{
		ID:           rec.ID,
		Name:         rec.Name,
		Hash:         rec.Hash,
		OwnerSubject: rec.OwnerSubject,
		OwnerLogin:   rec.OwnerLogin,
		OwnerEmail:   rec.OwnerEmail,
		LastUsedAt:   rec.LastUsedAt,
		ExpiresAt:    rec.ExpiresAt,
		RevokedAt:    rec.RevokedAt,
	}, nil
}

func (t tokenStore) TouchToken(ctx context.Context, id string, at time.Time) error {
	return t.st.TouchToken(ctx, id, at)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to YAML config file (default: ./config.yml if present)")
	showVersion := flag.Bool("version", false, "print build metadata and exit")
	flag.Parse()

	// Answered before touching config or the database so that `-version` works
	// on a container that is otherwise unconfigured.
	if *showVersion {
		fmt.Printf("pr-server %s (%s, %s)\n", version, commit, date)
		return nil
	}

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

	// CLI credentials are minted by this server, so bearer validation is a
	// local lookup: there is no call out to the identity provider on the
	// request path. The provider is only involved in human web login.
	validator := auth.NewStoreValidator(tokenStore{st: st})

	sessions := auth.NewSessionManager(cfg)
	if err := auth.SetupGoth(cfg, sessions); err != nil {
		return err
	}
	allow := auth.NewAllowlist(cfg.Allowlist)

	srv := server.New(cfg, st, validator, allow, sessions, auth.Handlers(sessions, allow))

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("pr-server %s listening on %s (base url: %s)",
			version, cfg.ListenAddr, cfg.BaseURL)
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
