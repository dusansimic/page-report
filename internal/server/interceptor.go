package server

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/auth"
	"github.com/dusan/page-report/internal/store"
)

type identityKey struct{}
type tokenInfoKey struct{}

// IdentityFrom returns the authenticated identity stored by the bearer
// interceptor or by guardDashboard.
func IdentityFrom(ctx context.Context) (auth.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(auth.Identity)
	return id, ok
}

// TokenInfoFrom returns which token authenticated the request, when the
// validator was able to say. Only WhoAmI needs it.
func TokenInfoFrom(ctx context.Context) (auth.TokenInfo, bool) {
	info, ok := ctx.Value(tokenInfoKey{}).(auth.TokenInfo)
	return info, ok
}

// OwnerFrom builds the page-ownership matcher for the authenticated caller.
func OwnerFrom(ctx context.Context) store.Owner {
	id, _ := IdentityFrom(ctx)
	return store.Owner{Subject: id.Subject, Email: id.Email, Login: id.Login}
}

func (s *Server) connectOptions() []connect.HandlerOption {
	return []connect.HandlerOption{connect.WithInterceptors(s.bearerInterceptor())}
}

func (s *Server) bearerInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().Procedure == pagereportv1connect.PageServiceGetServerInfoProcedure {
				return next(ctx, req)
			}
			header := req.Header().Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("missing bearer token"))
			}

			var identity auth.Identity
			var info auth.TokenInfo
			// A validator that can name the token lets WhoAmI report it; one
			// that cannot still authenticates normally.
			if introspector, ok := s.validator.(auth.TokenIntrospector); ok {
				var err error
				info, err = introspector.Introspect(ctx, token)
				identity = info.Identity
				if err != nil {
					return nil, connect.NewError(connect.CodeUnauthenticated,
						errors.New("invalid token"))
				}
			} else {
				var err error
				identity, err = s.validator.Validate(ctx, token)
				if err != nil {
					return nil, connect.NewError(connect.CodeUnauthenticated,
						errors.New("invalid token"))
				}
			}

			if !s.allow.Match(identity) {
				return nil, connect.NewError(connect.CodePermissionDenied,
					errors.New("identity not allowlisted"))
			}
			ctx = context.WithValue(ctx, identityKey{}, identity)
			ctx = context.WithValue(ctx, tokenInfoKey{}, info)
			return next(ctx, req)
		}
	}
}
