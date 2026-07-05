package server

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/auth"
)

type identityKey struct{}

// IdentityFrom returns the authenticated identity stored by the bearer
// interceptor.
func IdentityFrom(ctx context.Context) (auth.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(auth.Identity)
	return id, ok
}

func (s *Server) connectOptions() []connect.HandlerOption {
	return []connect.HandlerOption{connect.WithInterceptors(s.bearerInterceptor())}
}

func (s *Server) bearerInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().Procedure == pagereportv1connect.PageServiceGetAuthConfigProcedure {
				return next(ctx, req)
			}
			header := req.Header().Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("missing bearer token"))
			}
			identity, err := s.validator.Validate(ctx, token)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("invalid token"))
			}
			if !s.allow.Match(identity) {
				return nil, connect.NewError(connect.CodePermissionDenied,
					errors.New("identity not allowlisted"))
			}
			return next(context.WithValue(ctx, identityKey{}, identity), req)
		}
	}
}
