package auth

import "strings"

// Allowlist is the set of permitted identities: emails (OIDC) and/or GitHub
// logins, matched case-insensitively.
type Allowlist struct {
	entries map[string]struct{}
}

func NewAllowlist(entries []string) *Allowlist {
	m := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			m[e] = struct{}{}
		}
	}
	return &Allowlist{entries: m}
}

// Match reports whether the identity's email or login is allowlisted.
func (a *Allowlist) Match(id Identity) bool {
	if id.Email != "" {
		if _, ok := a.entries[strings.ToLower(id.Email)]; ok {
			return true
		}
	}
	if id.Login != "" {
		if _, ok := a.entries[strings.ToLower(id.Login)]; ok {
			return true
		}
	}
	return false
}
