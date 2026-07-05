// Package client contains the CLI-side pieces for talking to the
// page-report server: a connect RPC client factory, the OAuth 2.0 device
// flow, and the on-disk credential store.
package client

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
)

// TokenSource supplies the bearer token attached to authenticated RPCs.
// Implementations may refresh the token as needed. Returning an empty
// token (with a nil error) means "send no Authorization header".
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// New returns a PageService client for the given server base URL. It speaks
// the Connect protocol with JSON on the wire. If ts is non-nil, every request
// carries "Authorization: Bearer <token>" from the token source; pass nil for
// unauthenticated calls (e.g. GetAuthConfig).
func New(serverURL string, ts TokenSource) pagereportv1connect.PageServiceClient {
	return pagereportv1connect.NewPageServiceClient(
		http.DefaultClient,
		serverURL,
		connect.WithProtoJSON(),
		connect.WithInterceptors(bearerInterceptor(ts)),
	)
}

// bearerInterceptor injects the bearer token from ts into outgoing requests.
// It is a no-op when ts is nil or the token is empty.
func bearerInterceptor(ts TokenSource) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if ts != nil && req.Spec().IsClient {
				tok, err := ts.Token(ctx)
				if err != nil {
					return nil, fmt.Errorf("obtaining credentials: %w", err)
				}
				if tok != "" {
					req.Header().Set("Authorization", "Bearer "+tok)
				}
			}
			return next(ctx, req)
		}
	}
}
