package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	githubCacheTTL      = 60 * time.Second
	githubCacheMaxSize  = 1000
	defaultGitHubAPIURL = "https://api.github.com"
)

type cachedIdentity struct {
	id      Identity
	expires time.Time
}

// GitHubValidator validates opaque GitHub tokens by calling the GitHub API.
// Successful validations are cached in memory for a short TTL.
type GitHubValidator struct {
	// HTTPClient is the client used for API calls; override in tests.
	HTTPClient *http.Client
	// BaseURL is the GitHub API base URL (no trailing slash); override in tests.
	BaseURL string

	mu    sync.Mutex
	cache map[[32]byte]cachedIdentity
}

// NewGitHubValidator returns a validator hitting the public GitHub API with a
// 10-second timeout. The concrete type is returned so tests can inject
// HTTPClient and BaseURL; it satisfies TokenValidator.
func NewGitHubValidator() *GitHubValidator {
	return &GitHubValidator{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		BaseURL:    defaultGitHubAPIURL,
		cache:      make(map[[32]byte]cachedIdentity),
	}
}

// Validate resolves the token to the GitHub user it belongs to.
func (v *GitHubValidator) Validate(ctx context.Context, token string) (Identity, error) {
	key := sha256.Sum256([]byte(token))

	v.mu.Lock()
	entry, ok := v.cache[key]
	v.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.id, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.BaseURL+"/user", nil)
	if err != nil {
		return Identity{}, fmt.Errorf("build github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := v.HTTPClient.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("call github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("github token validation failed: status %d", resp.StatusCode)
	}

	var user struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return Identity{}, fmt.Errorf("decode github user: %w", err)
	}

	id := Identity{Subject: fmt.Sprint(user.ID), Login: user.Login, Email: user.Email}

	v.mu.Lock()
	if len(v.cache) > githubCacheMaxSize {
		v.cache = make(map[[32]byte]cachedIdentity)
	}
	v.cache[key] = cachedIdentity{id: id, expires: time.Now().Add(githubCacheTTL)}
	v.mu.Unlock()

	return id, nil
}
