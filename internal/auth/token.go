package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dusan/page-report/internal/id"
)

// Tokens are the CLI's only credential. They are minted by the server, shown
// to the user exactly once, and stored as a hash — the plaintext exists in the
// database at no point.
//
//	prt_<id>_<secret>
//
// The id half is the row's primary key, so validation is a single indexed
// lookup rather than a scan-and-compare over every row. The secret half is the
// only part that is hashed; the id is public and appears in the UI.
const (
	// TokenPrefix marks the string as a page-report token in logs and secret
	// scanners.
	TokenPrefix = "prt_"
	// secretBytes is the entropy of the secret half. 32 bytes is well past
	// anything brute-forceable against a server-side lookup.
	secretBytes = 32
)

// ErrInvalidToken covers every rejection reason a caller may not distinguish:
// malformed, unknown, wrong secret, revoked, expired. Telling them apart would
// let an attacker enumerate valid ids.
var ErrInvalidToken = errors.New("invalid token")

// MintedToken is the result of minting: the plaintext to hand the user once,
// plus the id and hash to persist.
type MintedToken struct {
	// Plaintext is the full prt_… string. Never store or log it.
	Plaintext string
	ID        string
	Hash      string
}

// Mint generates a new token.
func Mint() (MintedToken, error) {
	tokenID, err := id.New()
	if err != nil {
		return MintedToken{}, fmt.Errorf("generate token id: %w", err)
	}
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return MintedToken{}, fmt.Errorf("generate token secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	return MintedToken{
		Plaintext: TokenPrefix + tokenID + "_" + secret,
		ID:        tokenID,
		Hash:      HashSecret(secret),
	}, nil
}

// HashSecret returns the hex SHA-256 of a token's secret half. A plain hash is
// the right primitive here, not a password KDF: the secret is 256 bits of
// server-generated entropy, so there is no dictionary to slow down.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ParseToken splits a token string into its id and secret halves. The split is
// on the FIRST underscore after the prefix, not the last: the id is base62 and
// so never contains one, whereas the secret is base64url and often does.
func ParseToken(s string) (tokenID, secret string, err error) {
	rest, ok := strings.CutPrefix(s, TokenPrefix)
	if !ok {
		return "", "", ErrInvalidToken
	}
	i := strings.Index(rest, "_")
	if i <= 0 || i == len(rest)-1 {
		return "", "", ErrInvalidToken
	}
	tokenID, secret = rest[:i], rest[i+1:]
	if len(tokenID) != id.Length {
		return "", "", ErrInvalidToken
	}
	return tokenID, secret, nil
}

// DisplayPrefix renders a token id for the UI. The secret half is
// unrecoverable, so this is all a token list can ever show.
func DisplayPrefix(tokenID string) string {
	return TokenPrefix + tokenID
}

// TokenRecord is the stored side of a token, as the validator needs it. It
// mirrors store.Token without importing that package: internal/auth stays
// free of a dependency on the persistence layer.
type TokenRecord struct {
	ID           string
	Name         string
	Hash         string
	OwnerSubject string
	OwnerLogin   string
	OwnerEmail   string
	LastUsedAt   *time.Time
	ExpiresAt    *time.Time
	RevokedAt    *time.Time
}

// TokenStore is the slice of the persistence layer the validator needs.
type TokenStore interface {
	GetToken(ctx context.Context, id string) (TokenRecord, error)
	TouchToken(ctx context.Context, id string, at time.Time) error
}

// touchInterval throttles last_used_at writes. Without it every authenticated
// request would issue an UPDATE against a single-connection SQLite database
// purely for a display field.
const touchInterval = time.Minute

// StoreValidator authenticates CLI bearer tokens against the token table. It
// replaces the previous IdP-backed validators: the server issues its own
// credentials now, so it no longer calls out to GitHub or an OIDC issuer on
// the request path.
type StoreValidator struct {
	store TokenStore
	now   func() time.Time
}

func NewStoreValidator(s TokenStore) *StoreValidator {
	return &StoreValidator{store: s, now: time.Now}
}

// TokenInfo names the token a request authenticated with, so `page-report
// whoami` can report which credential is in play on a machine.
type TokenInfo struct {
	Identity  Identity
	TokenID   string
	TokenName string
}

// TokenIntrospector is an optional capability of a TokenValidator: it reports
// which token was used, not just who it belongs to.
type TokenIntrospector interface {
	Introspect(ctx context.Context, token string) (TokenInfo, error)
}

// Validate implements TokenValidator.
func (v *StoreValidator) Validate(ctx context.Context, token string) (Identity, error) {
	info, err := v.Introspect(ctx, token)
	return info.Identity, err
}

// Introspect implements TokenIntrospector.
func (v *StoreValidator) Introspect(ctx context.Context, token string) (TokenInfo, error) {
	tokenID, secret, err := ParseToken(token)
	if err != nil {
		return TokenInfo{}, err
	}
	rec, err := v.store.GetToken(ctx, tokenID)
	if err != nil {
		return TokenInfo{}, ErrInvalidToken
	}
	// Constant time so a wrong secret cannot be narrowed down byte by byte.
	if subtle.ConstantTimeCompare([]byte(HashSecret(secret)), []byte(rec.Hash)) != 1 {
		return TokenInfo{}, ErrInvalidToken
	}

	now := v.now().UTC()
	if rec.RevokedAt != nil {
		return TokenInfo{}, ErrInvalidToken
	}
	if rec.ExpiresAt != nil && !now.Before(*rec.ExpiresAt) {
		return TokenInfo{}, ErrInvalidToken
	}

	if rec.LastUsedAt == nil || now.Sub(*rec.LastUsedAt) > touchInterval {
		// Best effort: a failed bookkeeping write must not fail the request.
		_ = v.store.TouchToken(ctx, rec.ID, now)
	}

	return TokenInfo{
		Identity: Identity{
			Subject: rec.OwnerSubject,
			Email:   rec.OwnerEmail,
			Login:   rec.OwnerLogin,
		},
		TokenID:   rec.ID,
		TokenName: rec.Name,
	}, nil
}
