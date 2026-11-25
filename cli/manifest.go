package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/catalog"
	"github.com/ebur-dev/ebur/core/config"
)

// manifestCmd shows which runtime list is in use and fetches a newer
// signed one on request (SEC-02). The host also checks daily.
func manifestCmd() *cobra.Command {
	c := supportsJSON(&cobra.Command{
		Use:   "manifest",
		Short: "Show which list of runtime versions Ebur uses, and check for a newer signed one",
		Long: "Ebur offers the PHP, Node, database and tool versions listed in its runtime manifest. The list\n" +
			"built into Ebur can be replaced by a newer one the project publishes, but only if its signature\n" +
			"verifies against the key built into Ebur. Ebur checks daily; `ebur manifest refresh` checks now.\n" +
			"Set \"manifest_url\": \"off\" in ebur.json to stop checking.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			_, src, err := catalog.LoadFrom(root)
			if err != nil {
				return err
			}
			cfg, _ := config.Load(root.Config())
			url := catalog.URL(cfg)
			if jsonOut {
				return emit(map[string]any{"source": src.Kind, "generated": src.Generated, "url": url})
			}
			fmt.Printf("In use: the %s list from %s.\n", src.Kind, src.Generated.Local().Format("2006-01-02 15:04"))
			if url == "" {
				fmt.Println("Checking for newer lists is off (manifest_url: off).")
			} else {
				fmt.Printf("Newer signed lists come from %s (checked daily).\n", url)
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "refresh",
		Short: "Check now for a newer signed list of runtime versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			ctx, cancel := signalContext()
			defer cancel()
			res, err := catalog.Refresh(ctx, root, nil)
			if err != nil {
				return &hintErr{err.Error(), "Ebur keeps using the list it has; this is safe. Check your connection, or run `ebur doctor`."}
			}
			if res.Updated {
				fmt.Printf("Updated: %s.\n", res.Message)
			} else {
				fmt.Printf("No newer list (%s). In use: %s.\n", res.Message, res.Generated.Local().Format("2006-01-02 15:04"))
			}
			return nil
		},
	})
	return c
}
