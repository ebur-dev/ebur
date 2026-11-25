package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/sites"
)

// linkCmd serves a project kept outside www\ (like Laravel Valet's link).
func linkCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "link [folder]",
		Short: "Serve a project kept outside www\\ (default: the current folder)",
		Example: "  ebur link                          serve the current folder\n" +
			"  ebur link D:\\code\\shop             served at shop.test\n" +
			"  ebur link D:\\code\\My_Shop --name shop",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			target, _ := os.Getwd()
			if len(args) == 1 {
				target = args[0]
			}
			got, err := sites.Link(root.Dir(), root.WWW(), target, name)
			if err != nil {
				return &hintErr{err.Error(), "Run `ebur link --help` for examples."}
			}
			cfg, _ := config.Load(root.Config())
			dest, _ := os.Readlink(filepath.Join(root.WWW(), got))
			fmt.Printf("Linked %s\n  -> served at %s.%s (www\\%s points to it; your files stay where they are)\n", dest, got, cfg.TLD, got)
			fmt.Printf("Remove it with `ebur unlink %s`, which never deletes the project. Do not delete www\\%s by hand: some tools would delete the project too.\n", got, got)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "site name (default: the folder name, made URL-safe)")
	return c
}

func unlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <site>",
		Short: "Stop serving a linked project; its files are not touched",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			if err := sites.Unlink(root.WWW(), args[0]); err != nil {
				return &hintErr{err.Error(), "Run `ebur sites` to see which sites are linked."}
			}
			fmt.Printf("Unlinked %s. The project folder was not touched.\n", args[0])
			return nil
		},
	}
}
