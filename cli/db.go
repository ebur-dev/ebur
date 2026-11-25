package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/install"
	"github.com/ebur-dev/ebur/core/ports"
	"github.com/ebur-dev/ebur/core/runtimes"
)

// dbServerJSON is one server in `ebur db --json`.
type dbServerJSON struct {
	Server   string `json:"server"` // mariadb or mysql
	Name     string `json:"name"`   // e.g. "MySQL 8.4.13.1"
	Version  string `json:"version"`
	State    string `json:"state"`
	Default  bool   `json:"default"` // used by new projects
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DataDir  string `json:"data_dir"`
	EOL      string `json:"end_of_life,omitempty"`
}

func dbCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Show database servers (MariaDB, MySQL), their logins and the Adminer link",
		Example: "  ebur db                       servers, ports and the root login\n" +
			"  ebur install mysql 8.4        add MySQL next to MariaDB\n" +
			"  ebur db default mysql         new projects use MySQL\n" +
			"  ebur db list                  databases on every server",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			servers := dbserver.Installed(root, cfg)
			if len(servers) == 0 {
				return &hintErr{"no database server is installed", "Run `ebur install mariadb` or `ebur install mysql 8.4`."}
			}
			def, _ := dbserver.Default(root, cfg)
			state := map[string]string{}
			var st host.Status
			if call(root, "status", nil, &st) == nil {
				for _, s := range st.Services {
					state[s.Name] = string(s.State)
				}
			}
			adminer := ""
			if a, _ := runtimes.Installed(root, "adminer"); len(a) > 0 {
				adminer = "http://db." + cfg.TLD
			}
			var out []dbServerJSON
			for _, s := range servers {
				j := dbServerJSON{Server: string(s.Kind), Name: s.Label(), Version: s.Version, State: orText(state[string(s.Kind)], "stopped"),
					Default: s.Kind == def.Kind, Host: "127.0.0.1", Port: s.Port, User: "root", Password: cfg.DBRootPassword, DataDir: s.DataDir}
				if sup := supportOf(string(s.Kind), s.Version); sup != "" {
					j.EOL = sup
				}
				out = append(out, j)
			}
			if jsonOut {
				return emit(map[string]any{"servers": out, "adminer": adminer})
			}
			for _, s := range out {
				tag := ""
				if s.Default && len(out) > 1 {
					tag = "  (default for new projects)"
				}
				fmt.Printf("%s, %s%s\n", s.Name, s.State, tag)
				if s.EOL != "" {
					fmt.Printf("  warning: end of life since %s; for old projects only\n", s.EOL)
				}
				fmt.Printf("  Host 127.0.0.1  Port %d  User root  Password %s\n", s.Port, s.Password)
				fmt.Printf("  .env: DB_CONNECTION=mysql DB_HOST=127.0.0.1 DB_PORT=%d DB_USERNAME=root DB_PASSWORD=%s\n", s.Port, s.Password)
				fmt.Printf("  Data: %s\n\n", s.DataDir)
			}
			if adminer != "" {
				fmt.Printf("Adminer: %s  (server 127.0.0.1:<port>, user root)\n", adminer)
			} else {
				fmt.Println("Adminer: not installed (run `ebur install adminer`)")
			}
			if len(out) == 1 && out[0].Server == "mariadb" {
				fmt.Println("Need MySQL 8 to match a production server? `ebur install mysql 8.4` adds it next to MariaDB.")
			}
			return nil
		},
	}
	c.PersistentFlags().StringVar(&dbServerFlag, "server", "", "mariadb or mysql (default: the site's server, else the default one)")
	c.AddCommand(&cobra.Command{
		Use:     "port <number>",
		Short:   "Set a database server's port, e.g. 3307 when another MySQL uses 3306",
		Example: "  ebur db port 3307                 MariaDB's port\n  ebur db port 3308 --server mysql  MySQL's port",
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
			kind := dbserver.MariaDB
			if dbServerFlag != "" {
				k, ok := dbserver.ParseKind(dbServerFlag)
				if !ok {
					return &hintErr{fmt.Sprintf("unknown --server %q", dbServerFlag), "Use --server mariadb or --server mysql."}
				}
				kind = k
			}
			old := dbserver.PortOf(cfg, kind)
			if kind == dbserver.MySQL {
				cfg.MySQLPort = port
			} else {
				cfg.DBPort = port
			}
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			fmt.Printf("%s port set to %d (was %d).\n", kind.Name(), port, old)
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
	c.AddCommand(&cobra.Command{
		Use:     "default <mariadb|mysql>",
		Short:   "Choose the server new projects use when MariaDB and MySQL are both installed",
		Example: "  ebur db default mysql",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			k, ok := dbserver.ParseKind(args[0])
			if !ok {
				return &hintErr{fmt.Sprintf("unknown server %q", args[0]), "Use mariadb or mysql."}
			}
			cfg, err := config.Load(root.Config())
			if err != nil {
				return err
			}
			if _, ok := dbserver.Get(root, cfg, k); !ok {
				return &hintErr{k.Name() + " is not installed", fmt.Sprintf("Run `ebur install %s`.", k)}
			}
			cfg.DefaultDB = string(k)
			if err := config.Save(root.Config(), cfg); err != nil {
				return err
			}
			fmt.Printf("New projects now use %s. Existing projects keep the server in their .env.\n", k.Name())
			return nil
		},
	})
	c.AddCommand(snapshotCmds()...)
	return supportsJSON(c)
}

// supportOf returns the end-of-life date of a runtime version that is
// past it, or "".
func supportOf(rt, version string) string {
	if s := install.SupportOf(rt, version); s.Ended {
		return s.EOL
	}
	return ""
}

func orText(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}
