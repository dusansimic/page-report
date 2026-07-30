package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dusan/page-report/internal/client"
)

func updateCmd() *cobra.Command {
	var check, force, asJSON bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update this CLI to the latest release",
		Long: "Download the latest release from GitHub and replace the running binary.\n" +
			"The existing binary is only replaced after the download is verified\n" +
			"against the release checksums.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current, _, _ := buildMeta()
			if current == client.DevVersion && !check && !force {
				return fmt.Errorf("this is a %s build, not a released binary; "+
					"reinstall with install.sh, or pass --force to overwrite it",
					client.DevVersion)
			}
			u := client.NewUpdater()

			rel, err := u.Latest(cmd.Context())
			if err != nil {
				return err
			}
			available := client.IsNewer(current, rel.Tag)

			if check {
				if asJSON {
					return printJSON(map[string]any{
						"current":          current,
						"latest":           rel.Tag,
						"update_available": available,
					})
				}
				fmt.Printf("current: %s\nlatest:  %s\n", current, rel.Tag)
				if available {
					fmt.Println("An update is available: run `page-report update`.")
				} else {
					fmt.Println("Up to date.")
				}
				return nil
			}

			if !available && !force {
				fmt.Printf("Already up to date (%s).\n", current)
				return nil
			}

			dest, err := client.ExecutablePath()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updating %s: %s -> %s\n", dest, current, rel.Tag)
			if err := u.Install(cmd.Context(), rel, dest); err != nil {
				return err
			}
			fmt.Printf("Updated to %s.\n", rel.Tag)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update is available")
	cmd.Flags().BoolVar(&force, "force", false, "install the latest release even if it is not newer")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON (with --check)")
	return cmd
}
