package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/newproject"
)

// newCmd creates a project from a starter (PROJ-01/02).
func newCmd() *cobra.Command {
	var php string
	kinds := map[newproject.Kind]*bool{}
	c := &cobra.Command{
		Use:   "new <name> [--laravel | --wordpress | --symfony | --blank]",
		Short: "Create a new Laravel, WordPress, Symfony or blank site with its own database",
		Example: "  ebur new shop --laravel        Laravel at shop.test, database shop, .env filled in\n" +
			"  ebur new blog --wordpress      latest WordPress, wp-config.php filled in\n" +
			"  ebur new api --symfony --php 8.3\n" +
			"  ebur new scratch               a blank site with an index.php",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := newproject.Blank
			var chosen []string
			for k, on := range kinds {
				if *on {
					kind = k
					chosen = append(chosen, "--"+string(k))
				}
			}
			if len(chosen) > 1 {
				return &hintErr{"choose one project type, not " + strings.Join(chosen, " and "), "Run `ebur new --help` for examples."}
			}
			root, err := findRoot()
			if err != nil {
				return err
			}
			if kind != newproject.Blank {
				// The database must be running to create one.
				if err := ensureHost(root); err != nil {
					return err
				}
				var st host.Status
				if err := call(root, "start", selection(nil), &st); err != nil {
					fmt.Fprintln(os.Stderr, "warning: starting services:", err)
				}
			}
			ctx, cancel := signalContext()
			defer cancel()
			fmt.Printf("Creating %s (%s)\n", args[0], kind.Label())
			res, err := newproject.Create(ctx, root, newproject.Options{Name: args[0], Kind: kind, PHP: php},
				func(step string) { fmt.Println("==>", step) }, os.Stdout)
			if err != nil {
				hint := "Nothing was kept: no folder in www\\ and no new database."
				if strings.HasPrefix(err.Error(), "www\\") {
					hint = "Run `ebur sites` to see existing sites."
				}
				return &hintErr{err.Error(), hint}
			}
			fmt.Printf("\nCreated %s at %s\n", res.Name, res.URL)
			if res.Database != "" {
				fmt.Printf("Database: %s (see `ebur db` for the login)\n", res.Database)
			}
			for _, n := range res.Next {
				fmt.Println("  -", n)
			}
			autoSyncHosts(root) // SITE-02: map the new name
			return nil
		},
	}
	for _, k := range newproject.Kinds {
		kinds[k] = c.Flags().Bool(string(k), false, "create a "+k.Label()+" project")
	}
	c.Flags().StringVar(&php, "php", "", "pin a PHP version for this site, e.g. 8.3 (default: Ebur's default PHP)")
	return c
}
