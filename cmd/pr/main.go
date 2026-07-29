// Command pr is the CLI AI agents use to publish and manage report pages on a
// page-report server.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
	cfg        *client.Config
)

func main() {
	root := &cobra.Command{
		Use:           "pr",
		Short:         "Publish and manage HTML report pages on a page-report server",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       versionString(),
	}
	root.SetVersionTemplate("pr {{.Version}}\n")
	root.PersistentFlags().StringVar(&serverURL, "server", "",
		"page-report server base URL (app domain); falls back to PR_SERVER_URL, then the config file")
	root.PersistentFlags().StringVar(&configPath, "config", "",
		"path to config file (default: $XDG_CONFIG_HOME/page-report/config.yml)")
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		var err error
		cfg, err = client.LoadConfig(configPath)
		return err
	}

	root.AddCommand(loginCmd(), logoutCmd(), uploadCmd(), listCmd(), getCmd(), deleteCmd(),
		pruneCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		msg := err.Error()
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			msg += "\nhint: run `pr login` first"
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
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate against the server's identity provider (device flow)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := server()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			resp, err := client.New(url, nil).GetAuthConfig(ctx,
				connect.NewRequest(&pagereportv1.GetAuthConfigRequest{}))
			if err != nil {
				return fmt.Errorf("fetch auth config from %s: %w", url, err)
			}
			ac := client.AuthConfigFromProto(resp.Msg)

			tok, err := client.DeviceLogin(ctx, ac, func(uri, code, complete string) {
				fmt.Printf("Open %s and enter code: %s\n", uri, code)
				if complete != "" {
					fmt.Printf("Or open directly: %s\n", complete)
				}
				fmt.Println("Waiting for authorization...")
			})
			if err != nil {
				return err
			}
			if err := client.Save(client.CredentialsFromToken(url, ac, tok)); err != nil {
				return err
			}
			fmt.Println("Logged in.")
			return nil
		},
	}
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
			resp, err := c.UploadPage(cmd.Context(), connect.NewRequest(&pagereportv1.UploadPageRequest{
				Content: content,
				Title:   title,
			}))
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(map[string]string{"id": resp.Msg.GetId(), "url": resp.Msg.GetUrl()})
			}
			fmt.Println(resp.Msg.GetUrl())
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "page title (default: filename without extension)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func listCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all pages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := authedClient()
			if err != nil {
				return err
			}
			resp, err := c.ListPages(cmd.Context(), connect.NewRequest(&pagereportv1.ListPagesRequest{}))
			if err != nil {
				return err
			}
			pages := resp.Msg.GetPages()
			if asJSON {
				out := make([]map[string]any, 0, len(pages))
				for _, p := range pages {
					out = append(out, map[string]any{
						"id":         p.GetId(),
						"title":      p.GetTitle(),
						"size_bytes": p.GetSizeBytes(),
						"created_at": time.Unix(p.GetCreatedAt(), 0).UTC().Format(time.RFC3339),
						"created_by": p.GetCreatedBy(),
						"url":        p.GetUrl(),
					})
				}
				return printJSON(out)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tCREATED\tSIZE\tCREATED_BY\tTITLE")
			for _, p := range pages {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					p.GetId(),
					time.Unix(p.GetCreatedAt(), 0).UTC().Format(time.RFC3339),
					p.GetSizeBytes(),
					p.GetCreatedBy(),
					p.GetTitle())
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
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
			m := resp.Msg.GetMeta()
			if meta {
				return printMeta(m, asJSON)
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
// `pr list --json` plus content_type.
func printMeta(p *pagereportv1.PageMeta, asJSON bool) error {
	createdAt := time.Unix(p.GetCreatedAt(), 0).UTC().Format(time.RFC3339)
	if asJSON {
		return printJSON(map[string]any{
			"id":           p.GetId(),
			"title":        p.GetTitle(),
			"content_type": p.GetContentType(),
			"size_bytes":   p.GetSizeBytes(),
			"created_at":   createdAt,
			"created_by":   p.GetCreatedBy(),
			"url":          p.GetUrl(),
		})
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\t%s\n", p.GetId())
	fmt.Fprintf(tw, "TITLE\t%s\n", p.GetTitle())
	fmt.Fprintf(tw, "CONTENT_TYPE\t%s\n", p.GetContentType())
	fmt.Fprintf(tw, "SIZE\t%d\n", p.GetSizeBytes())
	fmt.Fprintf(tw, "CREATED\t%s\n", createdAt)
	fmt.Fprintf(tw, "CREATED_BY\t%s\n", p.GetCreatedBy())
	fmt.Fprintf(tw, "URL\t%s\n", p.GetUrl())
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
			fmt.Printf("pr %s\n", v)
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
