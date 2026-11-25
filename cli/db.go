package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/ports"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/runtimes"
)

func dbCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Show database connection details and the Adminer link",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			installed, _ := runtimes.Installed(root, "mariadb")
			if len(installed) == 0 {
				return &hintErr{"no database is installed", "Run `ebur install mariadb`."}
			}
			version := cfg.Active["mariadb"]
			if version == "" {
				version = installed[0]
			}
			state := "stopped (run `ebur start`)"
			var st host.Status
			if call(root, "status", nil, &st) == nil {
				for _, s := range st.Services {
					if s.Name == "mariadb" {
						state = string(s.State)
					}
				}
			}
			data, _ := provision.MariaDBPaths(root, version)
			fmt.Printf("MariaDB %s, %s\n\n", version, state)
			fmt.Printf("  Host:      127.0.0.1\n  Port:      %d\n  User:      root\n  Password:  %s\n\n", cfg.DBPort, cfg.DBRootPassword)
			fmt.Printf("  .env:      DB_HOST=127.0.0.1 DB_PORT=%d DB_USERNAME=root DB_PASSWORD=%s\n", cfg.DBPort, cfg.DBRootPassword)
			fmt.Printf("  Data:      %s\n", data)
			if a, _ := runtimes.Installed(root, "adminer"); len(a) > 0 {
				fmt.Printf("  Adminer:   http://db.%s  (server 127.0.0.1:%d, user root)\n", cfg.TLD, cfg.DBPort)
			} else {
				fmt.Println("  Adminer:   not installed (run `ebur install adminer`)")
			}
			return nil
		},
	}
	c.AddCommand(&cobra.Command{
		Use:     "port <number>",
		Short:   "Set the database port, e.g. 3307 when another MySQL uses 3306",
		Example: "  ebur db port 3307",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("%q is not a port number", args[0])
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			old := cfg.DBPort
			cfg.DBPort = port
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			fmt.Printf("Database port set to %d (was %d).\n", port, old)
			var conflict *ports.Conflict
			if port != old && errors.As(ports.Check(port), &conflict) {
				fmt.Printf("warning: %s. %s\n", conflict.Error(), conflict.Hint())
			}
			var st host.Status
			if call(root, "status", nil, &st) == nil && st.Summary != "none" {
				fmt.Println("Run `ebur restart` to apply it, and update DB_PORT in your projects' .env files.")
			} else {
				fmt.Println("Update DB_PORT in your projects' .env files.")
			}
			return nil
		},
	})
	c.AddCommand(snapshotCmds()...)
	return c
}
