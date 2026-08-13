package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/auth"
	"github.com/dusan/page-report/internal/store"
)

// Version is the server build version, surfaced by GetServerInfo. It is
// overridden at build time with -ldflags "-X .../internal/server.Version=...".
var Version = "dev"

const (
	// maxTokenNameLen keeps a label a label.
	maxTokenNameLen = 64
	// minTokenTTL rejects expiries too short to be usable, which are almost
	// always a units mistake (seconds where hours were meant).
	minTokenTTL = time.Hour
	// reauthRequired is the sentinel the SPA matches on to send the user back
	// through the identity provider before minting.
	reauthRequired = "reauth_required"
)

type authTimeKey struct{}

// dashboardService implements pagereportv1connect.DashboardServiceHandler.
type dashboardService struct {
	s *Server
}

func (s *Server) dashboardOptions() []connect.HandlerOption {
	// No interceptor: guardDashboard runs as HTTP middleware instead, because
	// it must reject non-same-origin requests before any RPC dispatch.
	return nil
}

// guardDashboard authenticates the browser API. It is the only place in the
// server where script-initiated requests are accepted, so every check that the
// Sec-Fetch-Dest gate would otherwise provide lives here instead:
//
//   - Origin must equal the configured base URL. It is required outright on
//     anything other than GET, which covers every Connect RPC.
//   - the session cookie must decode to an allowlisted identity.
//
// No CORS handler is registered anywhere in the server, so a cross-origin
// caller also fails its preflight: connect-es always sends the
// Connect-Protocol-Version header, which makes the request non-simple.
func (s *Server) guardDashboard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		present, ok := s.sameOrigin(r)
		if !ok && (present || r.Method != http.MethodGet) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		ctx := r.Context()
		if sess, ok := s.sessions.Get(r); ok && s.allow.Match(sess.Identity) {
			ctx = context.WithValue(ctx, identityKey{}, sess.Identity)
			ctx = context.WithValue(ctx, authTimeKey{}, sess.AuthTime)
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireSession is the per-RPC half of the guard. GetSession deliberately
// does not call it: the landing page asks who it is talking to before login.
func requireSession(ctx context.Context) (auth.Identity, error) {
	identity, ok := IdentityFrom(ctx)
	if !ok {
		return auth.Identity{}, connect.NewError(connect.CodeUnauthenticated,
			errors.New("not signed in"))
	}
	return identity, nil
}

// requireFreshAuth gates token minting. A CLI token outlives the session that
// created it, has no expiry unless one is chosen, and works from any machine,
// so a long-idle session should not be able to quietly produce one. Requiring
// a recent authentication forces that upgrade through a visible top-level
// navigation to the identity provider.
//
// Known limit: an identity provider that already has the user signed in will
// usually complete the flow without prompting, so this buys friction and
// visibility rather than a real credential re-check.
func (d *dashboardService) requireFreshAuth(ctx context.Context) error {
	window := d.s.cfg.TokenMintReauthWindow
	if window <= 0 {
		return nil
	}
	authTime, ok := ctx.Value(authTimeKey{}).(time.Time)
	if !ok || authTime.IsZero() || time.Since(authTime) > window {
		return connect.NewError(connect.CodePermissionDenied, errors.New(reauthRequired))
	}
	return nil
}

func (d *dashboardService) GetSession(
	ctx context.Context,
	_ *connect.Request[pagereportv1.GetSessionRequest],
) (*connect.Response[pagereportv1.GetSessionResponse], error) {
	resp := &pagereportv1.GetSessionResponse{
		LoginUrl:               "/auth/login",
		LogoutUrl:              "/auth/logout",
		TokenDefaultTtlSeconds: int64(d.s.cfg.TokenDefaultTTL.Seconds()),
	}
	identity, ok := IdentityFrom(ctx)
	if !ok {
		return connect.NewResponse(resp), nil
	}
	resp.Authenticated = true
	resp.Allowed = true // guardDashboard only sets an identity when allowlisted
	resp.Email = identity.Email
	resp.Login = identity.Login
	if identity.Login != "" {
		resp.AvatarUrl = "https://avatars.githubusercontent.com/" + identity.Login
	}
	resp.AuthIsFresh = d.requireFreshAuth(ctx) == nil
	return connect.NewResponse(resp), nil
}

func (d *dashboardService) ListPages(
	ctx context.Context,
	_ *connect.Request[pagereportv1.DashboardServiceListPagesRequest],
) (*connect.Response[pagereportv1.DashboardServiceListPagesResponse], error) {
	if _, err := requireSession(ctx); err != nil {
		return nil, err
	}
	pages, err := d.s.store.ListPagesByOwner(ctx, OwnerFrom(ctx))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &pagereportv1.DashboardServiceListPagesResponse{}
	for _, p := range pages {
		resp.Pages = append(resp.Pages, d.s.pageMeta(p))
	}
	return connect.NewResponse(resp), nil
}

func (d *dashboardService) DeletePage(
	ctx context.Context,
	req *connect.Request[pagereportv1.DashboardServiceDeletePageRequest],
) (*connect.Response[pagereportv1.DashboardServiceDeletePageResponse], error) {
	if _, err := requireSession(ctx); err != nil {
		return nil, err
	}
	err := d.s.store.DeletePageForOwner(ctx, req.Msg.GetId(), OwnerFrom(ctx))
	if errors.Is(err, store.ErrNotFound) {
		// Not PermissionDenied: someone else's page must be indistinguishable
		// from one that does not exist.
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.DashboardServiceDeletePageResponse{}), nil
}

func (d *dashboardService) ListTokens(
	ctx context.Context,
	_ *connect.Request[pagereportv1.ListTokensRequest],
) (*connect.Response[pagereportv1.ListTokensResponse], error) {
	identity, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := d.s.store.ListTokensByOwner(ctx, identity.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &pagereportv1.ListTokensResponse{}
	for _, t := range tokens {
		resp.Tokens = append(resp.Tokens, tokenMeta(t))
	}
	return connect.NewResponse(resp), nil
}

func (d *dashboardService) CreateToken(
	ctx context.Context,
	req *connect.Request[pagereportv1.CreateTokenRequest],
) (*connect.Response[pagereportv1.CreateTokenResponse], error) {
	identity, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.requireFreshAuth(ctx); err != nil {
		return nil, err
	}
	name, err := validTokenName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	expiresAt, err := expiryFrom(req.Msg.GetExpiresInSeconds())
	if err != nil {
		return nil, err
	}

	minted, err := auth.Mint()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rec := store.Token{
		ID:           minted.ID,
		Name:         name,
		Hash:         minted.Hash,
		OwnerSubject: identity.Subject,
		OwnerLogin:   identity.Login,
		OwnerEmail:   identity.Email,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    expiresAt,
	}
	if err := d.s.store.CreateToken(ctx, rec); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.CreateTokenResponse{
		Meta:  tokenMeta(rec),
		Token: minted.Plaintext,
	}), nil
}

func (d *dashboardService) RotateToken(
	ctx context.Context,
	req *connect.Request[pagereportv1.RotateTokenRequest],
) (*connect.Response[pagereportv1.RotateTokenResponse], error) {
	identity, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.requireFreshAuth(ctx); err != nil {
		return nil, err
	}

	minted, err := auth.Mint()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	// The id is kept so the token keeps its identity in the UI; only the
	// secret changes, and the previous one stops working immediately.
	if err := d.s.store.UpdateTokenHash(ctx, req.Msg.GetId(), identity.Subject,
		minted.Hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rotated, err := d.s.store.GetToken(ctx, req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.RotateTokenResponse{
		Meta: tokenMeta(rotated),
		// The rotated secret is prefixed with the original id, so the full
		// plaintext must be rebuilt around it rather than taken from Mint.
		Token: strings.Replace(minted.Plaintext, minted.ID, rotated.ID, 1),
	}), nil
}

func (d *dashboardService) DeleteToken(
	ctx context.Context,
	req *connect.Request[pagereportv1.DeleteTokenRequest],
) (*connect.Response[pagereportv1.DeleteTokenResponse], error) {
	identity, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}
	err = d.s.store.DeleteToken(ctx, req.Msg.GetId(), identity.Subject)
	if errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.DeleteTokenResponse{}), nil
}

func validTokenName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", connect.NewError(connect.CodeInvalidArgument,
			errors.New("token name must not be empty"))
	}
	if len(name) > maxTokenNameLen {
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("token name must be at most %d characters", maxTokenNameLen))
	}
	return name, nil
}

func expiryFrom(seconds int64) (*time.Time, error) {
	if seconds == 0 {
		return nil, nil
	}
	d := time.Duration(seconds) * time.Second
	if d < minTokenTTL {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("token expiry must be 0 (never) or at least %s", minTokenTTL))
	}
	t := time.Now().UTC().Add(d)
	return &t, nil
}

func tokenMeta(t store.Token) *pagereportv1.TokenMeta {
	m := &pagereportv1.TokenMeta{
		Id:            t.ID,
		Name:          t.Name,
		DisplayPrefix: auth.DisplayPrefix(t.ID),
		CreatedAt:     t.CreatedAt.Unix(),
		Revoked:       t.Revoked(time.Now()),
	}
	if t.LastUsedAt != nil {
		m.LastUsedAt = t.LastUsedAt.Unix()
	}
	if t.ExpiresAt != nil {
		m.ExpiresAt = t.ExpiresAt.Unix()
	}
	return m
}

// compile-time check that the service satisfies the generated interface.
var _ pagereportv1connect.DashboardServiceHandler = (*dashboardService)(nil)
