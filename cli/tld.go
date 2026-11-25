package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/hostsync"
	"github.com/ebur-dev/ebur/core/sites"
)

// tldCmd shows or changes the site address ending (SITE-04): .test needs
// the hosts file; .localhost works without it, for PCs where security
// software locks the hosts file.
func tldCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tld [test|localhost]",
		Short: "Show or change how site addresses end: .test (default) or .localhost (no hosts file needed)",
		Long: "Sites are served at <folder>.test by default, which needs entries in the Windows hosts file.\n" +
			"Where security software locks the hosts file, use .localhost instead: browsers send *.localhost\n" +
			"to this computer by themselves. Limit: PHP code that calls its own site's address (WordPress\n" +
			"scheduled tasks and Site Health, a Laravel app calling its own URL) cannot reach .localhost names.",
		Example: "  ebur tld              show the current ending\n  ebur tld localhost    sites at https://shop.localhost\n  ebur tld test         back to https://shop.test",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			if len(args) == 0 {
				fmt.Printf("Sites end in .%s (e.g. https://shop.%s).\n", cfg.TLD, cfg.TLD)
				if config.SelfResolving(cfg.TLD) {
					fmt.Println("No hosts-file entries are needed. PHP's own requests to site addresses cannot reach .localhost names.")
				} else if hostsync.Blocked(root) {
					fmt.Println("The hosts file looks locked by security software; `ebur tld localhost` works without it.")
				}
				return nil
			}
			want := strings.TrimPrefix(strings.ToLower(args[0]), ".")
			if want != "test" && want != "localhost" {
				return &hintErr{fmt.Sprintf("%q is not supported", args[0]), "Use `ebur tld test` or `ebur tld localhost`."}
			}
			if want == cfg.TLD {
				fmt.Printf("Sites already end in .%s.\n", want)
				return nil
			}
			old := cfg.TLD
			cfg.TLD = want
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			fmt.Printf("Sites now end in .%s (e.g. https://shop.%s).\n", want, want)
			if config.SelfResolving(want) {
				fmt.Println("No hosts-file entries are needed. Limit: PHP code that calls its own site's address cannot reach .localhost names.")
			}
			if wp := wordpressSites(root, want); len(wp) > 0 {
				fmt.Printf("\nWordPress keeps its address in its database, so these sites still point at .%s: %s.\n", old, strings.Join(wp, ", "))
				fmt.Printf("Update them in Settings > General, or with WP-CLI: wp search-replace '.%s' '.%s'.\n", old, want)
			}
			fmt.Printf("Laravel and Symfony projects: update APP_URL / DEFAULT_URI in .env if they use .%s.\n\n", old)
			if err := restartIfRunning(root, "to serve the new addresses"); err != nil {
				return err
			}
			if !config.SelfResolving(want) {
				autoSyncHosts(root) // .test needs the hosts file again
			}
			return nil
		},
	}
}

// wordpressSites lists sites that hold a WordPress install.
func wordpressSites(root interface{ WWW() string }, tld string) []string {
	found, _, _ := sites.Scan(root.WWW(), tld)
	var out []string
	for _, s := range found {
		if _, err := os.Stat(filepath.Join(s.Dir, "wp-includes", "version.php")); err == nil {
			out = append(out, s.Name)
		}
	}
	return out
}
