package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/runtimes"
)

// mysqlCmd shows or switches the MySQL version that runs next to MariaDB
// (SVC-01). Each version series keeps its own data folder: MySQL cannot
// safely open another major version's data.
func mysqlCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mysql [version]",
		Short: "Show the MySQL version running next to MariaDB, or switch to another installed one",
		Example: "  ebur mysql                 active and installed versions\n" +
			"  ebur install mysql 8.4     MySQL 8.4 LTS (also 8.0 and 5.7, legacy)\n" +
			"  ebur mysql 5.7             switch to the installed 5.7",
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
			installed, _ := runtimes.Installed(root, "mysql")
			if len(args) == 0 {
				active := dbserver.ActiveVersion(root, cfg, dbserver.MySQL)
				fmt.Printf("Active MySQL: %s\nInstalled:    %s\n", orNone(active), joinOr(installed, "none (run `ebur install mysql 8.4`)"))
				if active != "" {
					fmt.Printf("Port:         %d (MariaDB uses %d)\n", cfg.MySQLPort, cfg.DBPort)
				}
				return nil
			}
			v, err := matchInstalled(root, "mysql", args[0])
			if err != nil {
				return err
			}
			if dbserver.ActiveVersion(root, cfg, dbserver.MySQL) == v {
				fmt.Printf("MySQL %s is already active.\n", v)
				return nil
			}
			cfg.Active["mysql"] = v
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			data, _ := dbserver.Paths(root, dbserver.MySQL, v)
			fmt.Printf("Active MySQL set to %s. Its databases live in %s,\n", v, data)
			fmt.Println("separate from other MySQL versions; move a database across with `ebur db snapshot` and `ebur db restore`.")
			var st host.Status
			if err := call(root, "status", nil, &st); err != nil || st.Summary == "none" {
				return nil // takes effect on the next start
			}
			return restartIfRunning(root, "to switch MySQL")
		},
	}
	return c
}
