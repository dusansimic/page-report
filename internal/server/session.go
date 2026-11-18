package server

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
)

const (
	maxSessionIDBytes   = 128
	maxSessionTitleRune = 200
)

// sessionSources is the allowlist of agent hosts a session may name.
var sessionSources = map[string]bool{
	"t3code":      true,
	"claude-code": true,
	"codex":       true,
	"opencode":    true,
	"manual":      true,
}

// validateSession checks the optional session label on an upload or list
// request. A nil or all-empty session means "no session" and is not an error.
// Source and id must be set together. The title is sanitised, never rejected.
// Session fields are labels, not access control: ownership is the only scope.
func validateSession(s *pagereportv1.Session) (source, id, title string, err error) {
	if s == nil || (s.GetSource() == "" && s.GetId() == "" && s.GetTitle() == "") {
		return "", "", "", nil
	}
	source, id = s.GetSource(), s.GetId()
	if source == "" && id == "" {
		// Title alone carries no grouping key; drop it.
		return "", "", "", nil
	}
	if source == "" || id == "" {
		return "", "", "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("session source and id must be set together"))
	}
	if !sessionSources[source] {
		return "", "", "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("session source %q is not allowed", source))
	}
	if len(id) > maxSessionIDBytes {
		return "", "", "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("session id exceeds %d bytes", maxSessionIDBytes))
	}
	for i := 0; i < len(id); i++ {
		if !sessionIDChar(id[i]) {
			return "", "", "", connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("session id may only contain letters, digits and . _ : -"))
		}
	}
	return source, id, sanitizeSessionTitle(s.GetTitle()), nil
}

func sessionIDChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return c == '.' || c == '_' || c == ':' || c == '-'
}

func sanitizeSessionTitle(t string) string {
	t = strings.ToValidUTF8(t, "")
	t = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, t)
	t = strings.TrimSpace(t)
	if utf8.RuneCountInString(t) > maxSessionTitleRune {
		t = string([]rune(t)[:maxSessionTitleRune])
		t = strings.TrimSpace(t)
	}
	return t
}
