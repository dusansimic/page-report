// Command page-report is the CLI AI agents use to publish and manage report pages on a
// page-report server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
	"github.com/dusan/page-report/gen/pagereport/v1/pagereportv1connect"
	"github.com/dusan/page-report/internal/client"
)

// Build metadata, injected via -ldflags "-X main.version=..." by the release
// workflow. Without ldflags they fall back to the embedded build info.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

var (
	serverURL  string
	configPath string
	verbose    bool
	cfg        *client.Config
)

func main() {
	root := &cobra.Command{
		Use:           "page-report",
		Short:         "Publish and manage HTML report pages on a page-report server",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       versionString(),
	}
	root.SetVersionTemplate("page-report {{.Version}}\n")
	root.PersistentFlags().StringVar(&serverURL, "server", "",
		"page-report server base URL; falls back to PR_SERVER_URL, then the config file")
	root.PersistentFlags().StringVar(&configPath, "config", "",
		"path to config file (default: $XDG_CONFIG_HOME/page-report/config.yml)")
	root.PersistentFlags().BoolVar(&verbose, "verbose", false,
		"print non-fatal diagnostics (e.g. session lookup failures) to stderr")
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		var err error
		cfg, err = client.LoadConfig(configPath)
		return err
	}

	root.AddCommand(loginCmd(), logoutCmd(), whoamiCmd(), uploadCmd(), listCmd(), getCmd(),
		deleteCmd(), pruneCmd(), updateCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		msg := err.Error()
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			msg += "\nhint: run `page-report login` first"
		}
		fmt.Fprintln(os.Stderr, "error:", msg)
		os.Exit(1)
	}
}

// server resolves the server base URL: the --server flag wins, otherwise the
// loaded config, which already applied PR_SERVER_URL over the config file.
func server() (string, error) {
	raw := strings.TrimRight(serverURL, "/")
	if raw == "" && cfg != nil {
		raw = cfg.ServerURL
	}
	if raw == "" {
		return "", errors.New("server URL required: pass --server, set PR_SERVER_URL, " +
			"or set server_url in ~/.config/page-report/config.yml")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid server URL %q: must be an absolute http(s) URL", raw)
	}
	return raw, nil
}

func authedClient() (pagereportv1connect.PageServiceClient, error) {
	url, err := server()
	if err != nil {
		return nil, err
	}
	return client.New(url, client.StoredTokenSource{}), nil
}

func loginCmd() *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store an API token created in the server's web dashboard",
		Long: "Store an API token for this server.\n\n" +
			"Tokens are created in the web dashboard, on the Tokens page. The token is\n" +
			"shown there once: paste it here and it is saved to the credentials file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := server()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			// Best effort: the server tells us where tokens are minted. A
			// server too old (or briefly down) to answer must not block a
			// login the user can complete from the URL they already know.
			tokensURL := strings.TrimRight(url, "/") + "/tokens"
			if resp, err := client.New(url, nil).GetServerInfo(ctx,
				connect.NewRequest(&pagereportv1.GetServerInfoRequest{})); err == nil {
				if u := resp.Msg.GetTokensUrl(); u != "" {
					tokensURL = u
				}
			}

			token, err := readToken(cmd, fromStdin, tokensURL)
			if err != nil {
				return err
			}

			// Verify before storing, so a mistyped or already-revoked token
			// fails here rather than on the user's next upload.
			who, err := client.New(url, client.StaticToken(token)).WhoAmI(ctx,
				connect.NewRequest(&pagereportv1.WhoAmIRequest{}))
			if err != nil {
				return fmt.Errorf("verify token against %s: %w", url, err)
			}

			if err := client.Save(client.Credentials{ServerURL: url, Token: token}); err != nil {
				return err
			}
			fmt.Printf("Logged in to %s as %s.\n", url, describe(who.Msg))
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "token-stdin", false,
		"read the token from stdin instead of prompting")
	return cmd
}

// readToken collects the token without echoing it to the terminal. Piped input
// is read verbatim so `echo "$TOK" | page-report login --token-stdin` works in
// scripts.
func readToken(cmd *cobra.Command, fromStdin bool, tokensURL string) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("read token from stdin: %w", err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", errors.New("no token on stdin")
		}
		return token, nil
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("stdin is not a terminal: pass --token-stdin to pipe the token in")
	}
	fmt.Fprintf(os.Stderr, "Create a token at %s\nPaste it here (input is hidden): ", tokensURL)
	data, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("no token entered")
	}
	return token, nil
}

func describe(who *pagereportv1.WhoAmIResponse) string {
	name := who.GetEmail()
	if name == "" {
		name = who.GetLogin()
	}
	if name == "" {
		name = who.GetSubject()
	}
	if tn := who.GetTokenName(); tn != "" {
		return fmt.Sprintf("%s (token %q)", name, tn)
	}
	return name
}

func whoamiCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show which identity and token the stored credentials belong to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := authedClient()
			if err != nil {
				return err
			}
			resp, err := c.WhoAmI(cmd.Context(), connect.NewRequest(&pagereportv1.WhoAmIRequest{}))
			if err != nil {
				return err
			}
			m := resp.Msg
			if asJSON {
				return printJSON(map[string]string{
					"subject":    m.GetSubject(),
					"email":      m.GetEmail(),
					"login":      m.GetLogin(),
					"token_id":   m.GetTokenId(),
					"token_name": m.GetTokenName(),
				})
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "SUBJECT\t%s\n", m.GetSubject())
			fmt.Fprintf(tw, "EMAIL\t%s\n", m.GetEmail())
			fmt.Fprintf(tw, "LOGIN\t%s\n", m.GetLogin())
			fmt.Fprintf(tw, "TOKEN_ID\t%s\n", m.GetTokenId())
			fmt.Fprintf(tw, "TOKEN_NAME\t%s\n", m.GetTokenName())
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := client.Delete(); err != nil {
				return err
			}
			fmt.Println("Logged out.")
			return nil
		},
	}
}

func uploadCmd() *cobra.Command {
	var title string
	var asJSON bool
	var sf sessionFlags
	cmd := &cobra.Command{
		Use:   "upload <file.html>",
		Short: "Upload an HTML report; prints its shareable URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			if title == "" {
				base := filepath.Base(args[0])
				title = strings.TrimSuffix(base, filepath.Ext(base))
			}
			c, err := authedClient()
			if err != nil {
				return err
			}
			sess, grouped := sf.resolve(cmd.Context())
			req := &pagereportv1.UploadPageRequest{Content: content, Title: title}
			if grouped {
				req.Session = &pagereportv1.Session{Source: sess.Source, Id: sess.ID, Title: sess.Title}
			}
			resp, err := c.UploadPage(cmd.Context(), connect.NewRequest(req))
			if err != nil {
				return err
			}
			if asJSON {
				out := map[string]any{"id": resp.Msg.GetId(), "url": resp.Msg.GetUrl()}
				if grouped {
					out["session"] = sessionJSON(req.Session)
				}
				return printJSON(out)
			}
			fmt.Println(resp.Msg.GetUrl())
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "page title (default: filename without extension)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	sf.register(cmd)
	return cmd
}

// sessionFlags are the upload flags that control session grouping.
type sessionFlags struct {
	id, title string
	none      bool
}

func (f *sessionFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.id, "session", "",
		"group under this session id (source \"manual\"); falls back to PR_SESSION_ID, then auto-detection")
	cmd.Flags().StringVar(&f.title, "session-title", "",
		"session title; falls back to PR_SESSION_TITLE, then the agent's own title")
	cmd.Flags().BoolVar(&f.none, "no-session", false,
		"do not attach this report to any session")
}

// resolve applies flag > config/env > auto-detection. Never fails: session
// lookup is best effort and must not block an upload.
func (f *sessionFlags) resolve(ctx context.Context) (client.Session, bool) {
	if f.none {
		return client.Session{}, false
	}
	return detectSession(ctx, f.id, f.title)
}

// detectSession merges the explicit id/title flags over the config values and
// runs the detector.
func detectSession(ctx context.Context, id, title string) (client.Session, bool) {
	autoDetect := true
	if cfg != nil {
		autoDetect = cfg.Session.Detect
		if strings.TrimSpace(id) == "" {
			id = cfg.Session.ID
		}
		if strings.TrimSpace(title) == "" {
			title = cfg.Session.Title
		}
	}
	var debugf func(string, ...any)
	if verbose {
		debugf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
	}
	return client.NewDetector(debugf).Detect(ctx, client.Session{ID: id, Title: title}, autoDetect)
}

func sessionJSON(s *pagereportv1.Session) map[string]string {
	return map[string]string{"source": s.GetSource(), "id": s.GetId(), "title": s.GetTitle()}
}

// sessionLabel is the SESSION column: title, else source:first-8-of-id, else "-".
func sessionLabel(s *pagereportv1.Session) string {
	if s.GetId() == "" {
		return "-"
	}
	if t := s.GetTitle(); t != "" {
		return t
	}
	id := s.GetId()
	if len(id) > 8 {
		id = id[:8]
	}
	return s.GetSource() + ":" + id
}

// parseSessionFilter turns --session on `list` into a request filter.
func parseSessionFilter(ctx context.Context, v string) (*pagereportv1.Session, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	if v == "current" {
		s, ok := detectSession(ctx, "", "")
		if !ok {
			return nil, errors.New("--session current: no session detected " +
				"(set PR_SESSION_ID or run inside a supported agent)")
		}
		return &pagereportv1.Session{Source: s.Source, Id: s.ID}, nil
	}
	source, id, ok := strings.Cut(v, ":")
	if !ok || source == "" || id == "" {
		return nil, fmt.Errorf("invalid --session %q: want \"current\" or <source>:<id>", v)
	}
	return &pagereportv1.Session{Source: source, Id: id}, nil
}

func listCmd() *cobra.Command {
	var asJSON bool
	var sessionFilter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all pages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := authedClient()
			if err != nil {
				return err
			}
			filter, err := parseSessionFilter(cmd.Context(), sessionFilter)
			if err != nil {
				return err
			}
			resp, err := c.ListPages(cmd.Context(), connect.NewRequest(&pagereportv1.ListPagesRequest{Session: filter}))
			if err != nil {
				return err
			}
			pages := resp.Msg.GetPages()
			if asJSON {
				out := make([]map[string]any, 0, len(pages))
				for _, p := range pages {
					m := map[string]any{
						"id":         p.GetId(),
						"title":      p.GetTitle(),
						"size_bytes": p.GetSizeBytes(),
						"created_at": time.Unix(p.GetCreatedAt(), 0).UTC().Format(time.RFC3339),
						"created_by": p.GetCreatedBy(),
						"url":        p.GetUrl(),
					}
					if p.GetSession().GetId() != "" {
						m["session"] = sessionJSON(p.GetSession())
					}
					out = append(out, m)
				}
				return printJSON(out)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tCREATED\tSIZE\tCREATED_BY\tSESSION\tTITLE")
			for _, p := range pages {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
					p.GetId(),
					time.Unix(p.GetCreatedAt(), 0).UTC().Format(time.RFC3339),
					p.GetSizeBytes(),
					p.GetCreatedBy(),
					sessionLabel(p.GetSession()),
					p.GetTitle())
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.Flags().StringVar(&sessionFilter, "session", "",
		`only pages of one session: "current" (detected) or "<source>:<id>"`)
	return cmd
}

func getCmd() *cobra.Command {
	var output string
	var meta, asJSON, force bool
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Download a page's HTML (stdout by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON && !meta {
				return errors.New("--json requires --meta")
			}
			c, err := authedClient()
			if err != nil {
				return err
			}
			resp, err := c.GetPage(cmd.Context(), connect.NewRequest(&pagereportv1.GetPageRequest{
				Id:             args[0],
				IncludeContent: !meta,
			}))
			if err != nil {
				return err
			}
			if meta {
				return printMeta(resp.Msg.GetMeta(), asJSON)
			}

			content := resp.Msg.GetContent()
			if len(content) == 0 {
				return fmt.Errorf("page %s returned no content", args[0])
			}
			if output == "" || output == "-" {
				_, err = os.Stdout.Write(content)
				return err
			}
			if !force {
				if _, err := os.Stat(output); err == nil {
					return fmt.Errorf("%s already exists; pass --force to overwrite", output)
				} else if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			if err := os.WriteFile(output, content, 0o644); err != nil {
				return err
			}
			// stderr, so stdout stays clean for pipelines.
			fmt.Fprintf(os.Stderr, "Wrote %d bytes to %s\n", len(content), output)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "",
		`write HTML to this file ("-" means stdout)`)
	cmd.Flags().BoolVar(&meta, "meta", false, "print page metadata only, no content")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON metadata (requires --meta)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	return cmd
}

// printMeta renders one page's metadata, using the same field names as
// `page-report list --json` plus content_type.
func printMeta(p *pagereportv1.PageMeta, asJSON bool) error {
	createdAt := time.Unix(p.GetCreatedAt(), 0).UTC().Format(time.RFC3339)
	if asJSON {
		m := map[string]any{
			"id":           p.GetId(),
			"title":        p.GetTitle(),
			"content_type": p.GetContentType(),
			"size_bytes":   p.GetSizeBytes(),
			"created_at":   createdAt,
			"created_by":   p.GetCreatedBy(),
			"url":          p.GetUrl(),
		}
		if p.GetSession().GetId() != "" {
			m["session"] = sessionJSON(p.GetSession())
		}
		return printJSON(m)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\t%s\n", p.GetId())
	fmt.Fprintf(tw, "TITLE\t%s\n", p.GetTitle())
	fmt.Fprintf(tw, "CONTENT_TYPE\t%s\n", p.GetContentType())
	fmt.Fprintf(tw, "SIZE\t%d\n", p.GetSizeBytes())
	fmt.Fprintf(tw, "CREATED\t%s\n", createdAt)
	fmt.Fprintf(tw, "CREATED_BY\t%s\n", p.GetCreatedBy())
	fmt.Fprintf(tw, "URL\t%s\n", p.GetUrl())
	if sess := p.GetSession(); sess.GetId() != "" {
		fmt.Fprintf(tw, "SESSION_SOURCE\t%s\n", sess.GetSource())
		fmt.Fprintf(tw, "SESSION_ID\t%s\n", sess.GetId())
		fmt.Fprintf(tw, "SESSION_TITLE\t%s\n", sess.GetTitle())
	}
	return tw.Flush()
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := authedClient()
			if err != nil {
				return err
			}
			if _, err := c.DeletePage(cmd.Context(), connect.NewRequest(
				&pagereportv1.DeletePageRequest{Id: args[0]})); err != nil {
				return err
			}
			fmt.Println("Deleted", args[0])
			return nil
		},
	}
}

func pruneCmd() *cobra.Command {
	var olderThan string
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete pages older than a duration (e.g. 30d, 720h)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := client.ParseDuration(olderThan)
			if err != nil {
				return err
			}
			c, err := authedClient()
			if err != nil {
				return err
			}
			resp, err := c.PrunePages(cmd.Context(), connect.NewRequest(
				&pagereportv1.PrunePagesRequest{OlderThanSeconds: int64(d.Seconds())}))
			if err != nil {
				return err
			}
			fmt.Printf("Deleted %d page(s).\n", resp.Msg.GetDeletedCount())
			return nil
		},
	}
	cmd.Flags().StringVar(&olderThan, "older-than", "", "age threshold, e.g. 30d or 720h (required)")
	cmd.MarkFlagRequired("older-than")
	return cmd
}

func versionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version and build information",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			v, c, d := buildMeta()
			if asJSON {
				return printJSON(map[string]string{
					"version":  v,
					"commit":   c,
					"date":     d,
					"go":       runtime.Version(),
					"platform": runtime.GOOS + "/" + runtime.GOARCH,
				})
			}
			fmt.Printf("page-report %s\n", v)
			if c != "" {
				fmt.Printf("commit:   %s\n", c)
			}
			if d != "" {
				fmt.Printf("built:    %s\n", d)
			}
			fmt.Printf("go:       %s\n", runtime.Version())
			fmt.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

// buildMeta resolves the ldflags-injected build metadata, falling back to the
// build info the toolchain embeds (populated for `go install`-built binaries).
func buildMeta() (v, c, d string) {
	v, c, d = version, commit, date
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if v == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		v = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" {
				c = s.Value
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		}
	}
	return v, c, d
}

func versionString() string {
	v, c, _ := buildMeta()
	if c != "" {
		if len(c) > 12 {
			c = c[:12]
		}
		return v + " (" + c + ")"
	}
	return v
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
