package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Session identifies the agent session an upload belongs to. Source and ID are
// an opaque pair to the server; Title is a human label, possibly empty.
type Session struct {
	Source string
	ID     string
	Title  string
}

const (
	// t3ShowTimeout bounds the whole parent walk, so a hung t3 never slows an
	// upload by more than this.
	t3ShowTimeout = 3 * time.Second
	// t3MaxHops caps how far parentThreadId is followed towards the root.
	t3MaxHops = 5
)

// Detector resolves the current session from the environment. The functions
// are injected so tests never read the real environment or spawn processes.
type Detector struct {
	// Getenv reads an environment variable (os.Getenv in production).
	Getenv func(string) string
	// T3Show runs `<bin> thread show <id> --json` and returns its stdout.
	T3Show func(ctx context.Context, bin, id string) ([]byte, error)
	// Debugf, when non-nil, receives the reason a lookup was skipped or failed.
	Debugf func(format string, args ...any)
}

// NewDetector returns a Detector backed by os.Getenv and a real t3 subprocess.
// debugf may be nil.
func NewDetector(debugf func(format string, args ...any)) Detector {
	return Detector{Getenv: os.Getenv, T3Show: execT3Show, Debugf: debugf}
}

func execT3Show(ctx context.Context, bin, id string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, "thread", "show", id, "--json").Output()
}

func (d Detector) debugf(format string, args ...any) {
	if d.Debugf != nil {
		d.Debugf(format, args...)
	}
}

func (d Detector) env(name string) string { return strings.TrimSpace(d.Getenv(name)) }

// autoSources is the auto-detection precedence order.
var autoSources = []struct{ source, envVar string }{
	{"t3code", "T3CODE_THREAD_ID"},
	{"claude-code", "CLAUDE_CODE_SESSION_ID"},
	{"codex", "CODEX_THREAD_ID"},
	{"opencode", "OPENCODE_SESSION_ID"},
}

// Detect resolves the session for an upload. explicit carries values from
// flags/config (ID and/or Title; Source is ignored). An explicit ID always wins
// and is reported as source "manual"; autoDetect=false only disables the
// environment sources. An explicit Title overrides any resolved title. The
// boolean is false when the upload is ungrouped. Detect never fails: every
// lookup problem degrades to a less specific result.
func (d Detector) Detect(ctx context.Context, explicit Session, autoDetect bool) (Session, bool) {
	id := strings.TrimSpace(explicit.ID)
	title := strings.TrimSpace(explicit.Title)

	if id != "" {
		return Session{Source: "manual", ID: id, Title: title}, true
	}
	if !autoDetect {
		d.debugf("session: auto-detection disabled")
		return Session{}, false
	}

	for _, s := range autoSources {
		val := d.env(s.envVar)
		if val == "" {
			continue
		}
		res := Session{Source: s.source, ID: val}
		switch s.source {
		case "t3code":
			res = d.resolveT3(ctx, val)
		case "opencode":
			res.Title = d.env("OPENCODE_SESSION_TITLE")
		}
		if title != "" {
			res.Title = title
		}
		return res, true
	}
	d.debugf("session: no session environment variable set")
	return Session{}, false
}

// resolveT3 walks parentThreadId from the given thread to its root and returns
// the root as the session. On any failure it keeps the last good result, which
// for a failure on the first hop is the raw id with no title.
func (d Detector) resolveT3(ctx context.Context, id string) Session {
	res := Session{Source: "t3code", ID: id}

	bin := d.env("T3CODE_CLI_PATH")
	if bin == "" {
		bin = "t3"
	}
	ctx, cancel := context.WithTimeout(ctx, t3ShowTimeout)
	defer cancel()

	seen := map[string]bool{}
	cur := id
	for hop := 0; hop <= t3MaxHops; hop++ {
		if seen[cur] {
			d.debugf("session: t3 parent cycle at %s", cur)
			return res
		}
		seen[cur] = true

		out, err := d.T3Show(ctx, bin, cur)
		if err != nil {
			d.debugf("session: %s thread show %s: %v", bin, cur, err)
			return res
		}
		var doc struct {
			Thread struct {
				ThreadID       string  `json:"threadId"`
				Title          string  `json:"title"`
				ParentThreadID *string `json:"parentThreadId"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			d.debugf("session: parse thread show %s: %v", cur, err)
			return res
		}
		res = Session{Source: "t3code", ID: cur, Title: strings.TrimSpace(doc.Thread.Title)}

		parent := ""
		if doc.Thread.ParentThreadID != nil {
			parent = strings.TrimSpace(*doc.Thread.ParentThreadID)
		}
		if parent == "" {
			return res
		}
		cur = parent
	}
	d.debugf("session: t3 parent chain longer than %d hops", t3MaxHops)
	return res
}

// String renders a session as source:id for diagnostics.
func (s Session) String() string { return fmt.Sprintf("%s:%s", s.Source, s.ID) }
