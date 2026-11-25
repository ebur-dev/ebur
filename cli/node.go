package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/sites"
)

// major returns "22" for "22.23.5".
func major(v string) string {
	m, _, _ := strings.Cut(v, ".")
	return m
}

// nodeCmd manages Node.js versions (PROJ-03). Node is not a service:
// switching only changes what Ebur's terminal and `ebur exec` put on PATH.
func nodeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "node [version]",
		Short: "Show the default Node.js version, or switch to another installed one",
		Example: "  ebur node              show default and installed versions\n" +
			"  ebur node 22           use Node 22 by default\n" +
			"  ebur node install 24\n" +
			"  ebur exec npm run dev  run npm with the site's Node (.ebur, .nvmrc or package.json)",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			installed, _ := runtimes.Installed(root, "node")
			if len(args) == 0 {
				fmt.Printf("Default Node: %s\nInstalled:    %s\n", orNone(cfg.Active["node"]), joinOr(installed, "none (run `ebur node install 24`)"))
				if len(installed) > 0 {
					fmt.Println("Node is on PATH in Ebur's terminal and in `ebur exec`; your system PATH is not changed.")
				}
				return nil
			}
			v, err := matchInstalled(root, "node", strings.TrimPrefix(args[0], "v"))
			if err != nil {
				return err
			}
			if cfg.Active["node"] == v {
				fmt.Printf("Node %s is already the default.\n", v)
				return nil
			}
			cfg.Active["node"] = v
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			fmt.Printf("Default Node set to %s. Open a new Ebur terminal to use it.\n", v)
			return nil
		},
	}
	c.AddCommand(
		&cobra.Command{
			Use:     "install <version>",
			Short:   "Download and install a Node.js LTS version, e.g. 24 or 22",
			Example: "  ebur node install 24\n  ebur node list        see every version Ebur offers",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := findRoot()
				if err != nil {
					return err
				}
				return installRuntime(root, "node", strings.TrimPrefix(args[0], "v"))
			},
		},
		supportsJSON(&cobra.Command{
			Use:   "list",
			Short: "List installed and available Node.js versions",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := findRoot()
				if err != nil {
					return err
				}
				return listRuntime(root, "node")
			},
		}),
		&cobra.Command{
			Use:   "remove <version>",
			Short: "Remove an installed Node.js version",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := findRoot()
				if err != nil {
					return err
				}
				v, err := matchInstalled(root, "node", strings.TrimPrefix(args[0], "v"))
				if err != nil {
					return err
				}
				// Node is not a service Ebur runs, but a terminal may be using it;
				// Windows then refuses to delete node.exe and Remove reports it.
				if err := runtimes.Remove(root, "node", v, nil); err != nil {
					return &hintErr{err.Error(), "Close Ebur terminals and stop `npm run dev`, then try again."}
				}
				cfg, _ := config.Load(root.Config())
				if cfg.Active["node"] == v {
					delete(cfg.Active, "node")
					config.Save(root.Config(), cfg)
					fmt.Println("That was the default version; the newest installed one will be used next.")
				}
				fmt.Printf("Removed Node %s.\n", v)
				return nil
			},
		},
	)
	return c
}

func sitesNodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node <site> <version|default>",
		Short: "Pin a site's Node version in its .ebur file (PROJ-03)",
		Example: "  ebur sites node shop 22        npm and node for shop use Node 22\n" +
			"  ebur sites node shop default   back to .nvmrc, package.json or the default",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			found, _, err := sites.Scan(root.WWW(), cfg.TLD)
			if err != nil {
				return err
			}
			var site *sites.Site
			for i := range found {
				if found[i].Name == strings.ToLower(args[0]) {
					site = &found[i]
				}
			}
			if site == nil {
				return &hintErr{fmt.Sprintf("no site named %q", args[0]), "Run `ebur sites` to see site names."}
			}
			want := strings.TrimPrefix(args[1], "v")
			if want == "default" {
				want = ""
			} else if _, err := matchInstalled(root, "node", want); err != nil {
				return err
			}
			if err := project.SetNode(site.Dir, want); err != nil {
				return err
			}
			if want == "" {
				fmt.Printf("%s no longer pins Node in .ebur; .nvmrc, package.json or the default decides.\n", site.Name)
			} else {
				fmt.Printf("%s pinned to Node %s in %s\n", site.Name, want, filepath.Join(site.Dir, project.FileName))
			}
			fmt.Println("Open a new Ebur terminal in the site to use it.")
			return nil
		},
	}
}
