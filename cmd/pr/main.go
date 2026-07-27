// Command pr is the CLI AI agents use to publish and manage report pages on a
// page-report server.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
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

var serverURL string

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
		"page-report server base URL (app domain); falls back to PR_SERVER_URL")

	root.AddCommand(loginCmd(), logoutCmd(), uploadCmd(), listCmd(), deleteCmd(), pruneCmd(),
		versionCmd())

	if err := root.Execute(); err != nil {
		msg := err.Error()
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			msg += "\nhint: run `pr login` first"
		}
		fmt.Fprintln(os.Stderr, "error:", msg)
		os.Exit(1)
	}
}

func server() (string, error) {
	if serverURL != "" {
		return strings.TrimRight(serverURL, "/"), nil
	}
	if env := os.Getenv("PR_SERVER_URL"); env != "" {
		return strings.TrimRight(env, "/"), nil
	}
	return "", errors.New("server URL required: pass --server or set PR_SERVER_URL")
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
