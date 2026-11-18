package server

import (
	"strings"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
)

func TestValidateSession(t *testing.T) {
	sess := func(src, id, title string) *pagereportv1.Session {
		return &pagereportv1.Session{Source: src, Id: id, Title: title}
	}
	tests := []struct {
		name                   string
		in                     *pagereportv1.Session
		wantSrc, wantID, wantT string
		wantErr                bool
	}{
		{name: "nil", in: nil},
		{name: "all empty", in: sess("", "", "")},
		{name: "title only is dropped", in: sess("", "", "x")},
		{name: "valid", in: sess("t3code", "thread-1", "My title"), wantSrc: "t3code", wantID: "thread-1", wantT: "My title"},
		{name: "every source ok", in: sess("manual", "a", ""), wantSrc: "manual", wantID: "a"},
		{name: "id charset", in: sess("codex", "Az09._:-", ""), wantSrc: "codex", wantID: "Az09._:-"},
		{name: "id 128 bytes", in: sess("codex", strings.Repeat("a", 128), ""), wantSrc: "codex", wantID: strings.Repeat("a", 128)},
		{name: "id 129 bytes", in: sess("codex", strings.Repeat("a", 129), ""), wantErr: true},
		{name: "unknown source", in: sess("vim", "x", ""), wantErr: true},
		{name: "source without id", in: sess("t3code", "", ""), wantErr: true},
		{name: "id without source", in: sess("", "x", ""), wantErr: true},
		{name: "space in id", in: sess("manual", "a b", ""), wantErr: true},
		{name: "slash in id", in: sess("manual", "a/b", ""), wantErr: true},
		{name: "non-ascii id", in: sess("manual", "é", ""), wantErr: true},
		{name: "newline in id", in: sess("manual", "a\nb", ""), wantErr: true},
		{name: "title trimmed", in: sess("manual", "a", "  hi \t"), wantSrc: "manual", wantID: "a", wantT: "hi"},
		{name: "title control chars", in: sess("manual", "a", "a\x00b\x1b[0mc\nd"), wantSrc: "manual", wantID: "a", wantT: "ab[0mcd"},
		{name: "title invalid utf8", in: sess("manual", "a", "a\xffb"), wantSrc: "manual", wantID: "a", wantT: "ab"},
		{name: "title truncated", in: sess("manual", "a", strings.Repeat("é", 300)), wantSrc: "manual", wantID: "a", wantT: strings.Repeat("é", 200)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, id, title, err := validateSession(tc.in)
			if tc.wantErr {
				if connect.CodeOf(err) != connect.CodeInvalidArgument {
					t.Fatalf("got err %v, want invalid_argument", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if src != tc.wantSrc || id != tc.wantID || title != tc.wantT {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", src, id, title, tc.wantSrc, tc.wantID, tc.wantT)
			}
			if !utf8.ValidString(title) {
				t.Fatal("title is not valid UTF-8")
			}
		})
	}
}
