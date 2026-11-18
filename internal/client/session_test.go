package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// threads builds a fake T3Show from id -> (title, parent). Unknown ids error.
func threads(m map[string][2]string, calls *[]string) func(context.Context, string, string) ([]byte, error) {
	return func(_ context.Context, bin, id string) ([]byte, error) {
		if calls != nil {
			*calls = append(*calls, bin+" "+id)
		}
		t, ok := m[id]
		if !ok {
			return nil, errors.New("not found")
		}
		parent := "null"
		if t[1] != "" {
			parent = fmt.Sprintf("%q", t[1])
		}
		return []byte(fmt.Sprintf(`{"thread":{"threadId":%q,"title":%q,"parentThreadId":%s}}`, id, t[0], parent)), nil
	}
}

func TestDetect(t *testing.T) {
	failT3 := func(context.Context, string, string) ([]byte, error) { return nil, errors.New("boom") }

	chain := map[string][2]string{}
	for i := 0; i < 8; i++ { // t0 -> t1 -> ... -> t7 (root)
		parent := ""
		if i < 7 {
			parent = fmt.Sprintf("t%d", i+1)
		}
		chain[fmt.Sprintf("t%d", i)] = [2]string{fmt.Sprintf("title%d", i), parent}
	}

	tests := []struct {
		name     string
		env      map[string]string
		explicit Session
		auto     bool
		t3       func(context.Context, string, string) ([]byte, error)
		want     Session
		wantOK   bool
	}{
		{name: "none", auto: true, want: Session{}},
		{name: "claude", auto: true, env: map[string]string{"CLAUDE_CODE_SESSION_ID": "c1"},
			want: Session{"claude-code", "c1", ""}, wantOK: true},
		{name: "codex", auto: true, env: map[string]string{"CODEX_THREAD_ID": "x1"},
			want: Session{"codex", "x1", ""}, wantOK: true},
		{name: "precedence t3 over rest", auto: true, t3: failT3,
			env:  map[string]string{"T3CODE_THREAD_ID": "t", "CLAUDE_CODE_SESSION_ID": "c", "CODEX_THREAD_ID": "x", "OPENCODE_SESSION_ID": "o"},
			want: Session{"t3code", "t", ""}, wantOK: true},
		{name: "precedence claude over codex", auto: true,
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": "c", "CODEX_THREAD_ID": "x", "OPENCODE_SESSION_ID": "o"},
			want: Session{"claude-code", "c", ""}, wantOK: true},
		{name: "precedence codex over opencode", auto: true,
			env:  map[string]string{"CODEX_THREAD_ID": "x", "OPENCODE_SESSION_ID": "o"},
			want: Session{"codex", "x", ""}, wantOK: true},
		{name: "blank skipped", auto: true,
			env:  map[string]string{"T3CODE_THREAD_ID": "  ", "CLAUDE_CODE_SESSION_ID": "", "CODEX_THREAD_ID": " x "},
			want: Session{"codex", "x", ""}, wantOK: true},
		{name: "detect disabled", auto: false, env: map[string]string{"CLAUDE_CODE_SESSION_ID": "c"}},
		{name: "explicit wins", auto: true, explicit: Session{ID: "m1"}, env: map[string]string{"CLAUDE_CODE_SESSION_ID": "c"},
			want: Session{"manual", "m1", ""}, wantOK: true},
		{name: "explicit works with detect disabled", auto: false, explicit: Session{ID: "m1", Title: "T"},
			want: Session{"manual", "m1", "T"}, wantOK: true},
		{name: "blank explicit ignored", auto: true, explicit: Session{ID: "  "}, env: map[string]string{"CODEX_THREAD_ID": "x"},
			want: Session{"codex", "x", ""}, wantOK: true},
		{name: "explicit title overrides detected", auto: true, explicit: Session{Title: "Mine"},
			env:  map[string]string{"OPENCODE_SESSION_ID": "o", "OPENCODE_SESSION_TITLE": "theirs"},
			want: Session{"opencode", "o", "Mine"}, wantOK: true},
		{name: "opencode title", auto: true,
			env:  map[string]string{"OPENCODE_SESSION_ID": "o", "OPENCODE_SESSION_TITLE": " Fix bug "},
			want: Session{"opencode", "o", "Fix bug"}, wantOK: true},
		{name: "opencode without title", auto: true, env: map[string]string{"OPENCODE_SESSION_ID": "o"},
			want: Session{"opencode", "o", ""}, wantOK: true},
		{name: "t3 title", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "a"},
			t3:   threads(map[string][2]string{"a": {"Hello", ""}}, nil),
			want: Session{"t3code", "a", "Hello"}, wantOK: true},
		{name: "t3 walks to root", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "c"},
			t3:   threads(map[string][2]string{"c": {"child", "b"}, "b": {"mid", "a"}, "a": {"root", ""}}, nil),
			want: Session{"t3code", "a", "root"}, wantOK: true},
		{name: "t3 cycle keeps last distinct", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "a"},
			t3:   threads(map[string][2]string{"a": {"A", "b"}, "b": {"B", "a"}}, nil),
			want: Session{"t3code", "b", "B"}, wantOK: true},
		{name: "t3 5-hop cap", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "t0"},
			t3:   threads(chain, nil),
			want: Session{"t3code", "t5", "title5"}, wantOK: true},
		{name: "t3 failure falls back to raw id", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "raw"},
			t3: failT3, want: Session{"t3code", "raw", ""}, wantOK: true},
		{name: "t3 failure mid-walk keeps child", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "c"},
			t3:   threads(map[string][2]string{"c": {"child", "gone"}}, nil),
			want: Session{"t3code", "c", "child"}, wantOK: true},
		{name: "t3 bad json", auto: true, env: map[string]string{"T3CODE_THREAD_ID": "raw"},
			t3:   func(context.Context, string, string) ([]byte, error) { return []byte("nope"), nil },
			want: Session{"t3code", "raw", ""}, wantOK: true},
		{name: "t3 explicit title overrides", auto: true, explicit: Session{Title: "Mine"},
			env: map[string]string{"T3CODE_THREAD_ID": "a"}, t3: threads(map[string][2]string{"a": {"Hello", ""}}, nil),
			want: Session{"t3code", "a", "Mine"}, wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Detector{Getenv: envMap(tc.env), T3Show: tc.t3}
			if d.T3Show == nil {
				d.T3Show = func(context.Context, string, string) ([]byte, error) {
					t.Fatal("T3Show must not be called")
					return nil, nil
				}
			}
			got, ok := d.Detect(context.Background(), tc.explicit, tc.auto)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("Detect = %+v, %v; want %+v, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDetectT3Binary(t *testing.T) {
	for _, tc := range []struct{ cli, want string }{{"/opt/t3", "/opt/t3"}, {"", "t3"}, {"  ", "t3"}} {
		var calls []string
		d := Detector{
			Getenv: envMap(map[string]string{"T3CODE_THREAD_ID": "a", "T3CODE_CLI_PATH": tc.cli}),
			T3Show: threads(map[string][2]string{"a": {"x", ""}}, &calls),
		}
		d.Detect(context.Background(), Session{}, true)
		if len(calls) != 1 || calls[0] != tc.want+" a" {
			t.Fatalf("cli=%q calls = %v, want %q", tc.cli, calls, tc.want+" a")
		}
	}
}

func TestDetectDebugReason(t *testing.T) {
	var msgs []string
	d := Detector{
		Getenv: envMap(map[string]string{"T3CODE_THREAD_ID": "a"}),
		T3Show: func(context.Context, string, string) ([]byte, error) { return nil, errors.New("boom") },
		Debugf: func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) },
	}
	d.Detect(context.Background(), Session{}, true)
	if len(msgs) != 1 {
		t.Fatalf("debug messages = %v, want one", msgs)
	}
}
