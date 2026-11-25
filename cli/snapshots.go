package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/dbclient"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/dbsnap"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/sites"
)

// dbServerFlag is `ebur db ... --server mariadb|mysql`.
var dbServerFlag string

// forcedServer returns the --server choice, checking it is installed.
func forcedServer(root paths.Root, cfg config.Settings) (dbserver.Kind, error) {
	if dbServerFlag == "" {
		return "", nil
	}
	k, ok := dbserver.ParseKind(dbServerFlag)
	if !ok {
		return "", &hintErr{fmt.Sprintf("unknown --server %q", dbServerFlag), "Use --server mariadb or --server mysql."}
	}
	if _, ok := dbserver.Get(root, cfg, k); !ok {
		return "", &hintErr{k.Name() + " is not installed", fmt.Sprintf("Run `ebur install %s`.", k)}
	}
	return k, nil
}

// dbFor resolves a site name or database name to a database and the
// server holding it. A site is mapped through its own settings (.env,
// wp-config.php), whose port tells MariaDB and MySQL apart. A plain
// database name is looked up on every running server.
func dbFor(root paths.Root, arg string) (kind dbserver.Kind, db, note string, err error) {
	cfg, _ := config.Load(root.Config())
	servers := dbserver.Installed(root, cfg)
	if len(servers) == 0 {
		return "", "", "", &hintErr{"no database server is installed", "Run `ebur install mariadb` or `ebur install mysql 8.4`."}
	}
	forced, err := forcedServer(root, cfg)
	if err != nil {
		return "", "", "", err
	}
	def, _ := dbserver.Default(root, cfg)
	found, _, _ := sites.Scan(root.WWW(), cfg.TLD)
	for _, s := range found {
		if s.Name != strings.ToLower(arg) {
			continue
		}
		sd := dbsnap.SiteDatabase(s.Dir, s.Name)
		srv := def
		switch {
		case forced != "":
			srv, _ = dbserver.Get(root, cfg, forced)
		case sd.Port != 0:
			var ok bool
			if srv, ok = dbserver.ForPort(servers, sd.Port); !ok {
				return "", "", "", &hintErr{fmt.Sprintf("site %s's settings (%s) use port %d, where no Ebur database server runs", s.Name, sd.Source, sd.Port),
					"Run `ebur db` to see the servers' ports, and update the site's settings."}
			}
		}
		return srv.Kind, sd.Name, fmt.Sprintf("site %s uses database %s on %s (from %s)", s.Name, sd.Name, srv.Label(), sd.Source), nil
	}
	if !dbclient.ValidName(arg) || dbclient.IsSystem(arg) {
		return "", "", "", &hintErr{fmt.Sprintf("%q is not a site or a database Ebur can snapshot", arg), "Run `ebur db list` to see databases."}
	}
	if forced != "" {
		return forced, arg, "", nil
	}
	if len(servers) == 1 {
		return servers[0].Kind, arg, "", nil
	}
	// Both servers installed: find which one has the database.
	var on []dbserver.Kind
	for _, s := range servers {
		c, err := dbclient.OpenServer(root, cfg, s)
		if err != nil {
			continue
		}
		if ok, err := c.Exists(timeoutCtx(10*time.Second), arg); err == nil && ok {
			on = append(on, s.Kind)
		}
	}
	switch len(on) {
	case 1:
		return on[0], arg, "", nil
	case 2:
		return "", "", "", &hintErr{fmt.Sprintf("a database named %s exists on both MariaDB and MySQL", arg), "Add --server mariadb or --server mysql."}
	}
	return def.Kind, arg, "", nil
}

// dbReady starts a server if needed and opens a client for it.
func dbReady(root paths.Root, kind dbserver.Kind) (*dbclient.Client, error) {
	c, err := dbclient.OpenKind(root, kind)
	if err != nil {
		return nil, err
	}
	if c.Ping(timeoutCtx(10*time.Second)) == nil {
		return c, nil
	}
	if err := ensureHost(root); err != nil {
		return nil, err
	}
	var st host.Status
	if err := call(root, "start", selection([]string{string(kind)}), &st); err != nil {
		return nil, err
	}
	return c, c.Ping(timeoutCtx(10 * time.Second))
}

// timeoutCtx is a context that ends after d (short CLI queries).
func timeoutCtx(d time.Duration) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(d, cancel)
	return ctx
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func snapshotCmds() []*cobra.Command {
	list := &cobra.Command{
		Use:   "list",
		Short: "List databases on every server with their size, the site using each, and snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, _ := config.Load(root.Config())
			servers := dbserver.Installed(root, cfg)
			if len(servers) == 0 {
				return &hintErr{"no database server is installed", "Run `ebur install mariadb` or `ebur install mysql 8.4`."}
			}
			found, _, _ := sites.Scan(root.WWW(), cfg.TLD)
			def, _ := dbserver.Default(root, cfg)
			siteOf := map[string][]string{} // "kind/db" -> sites
			for _, s := range found {
				sd := dbsnap.SiteDatabase(s.Dir, s.Name)
				k := def.Kind
				if srv, ok := dbserver.ForPort(servers, sd.Port); ok {
					k = srv.Kind
				}
				siteOf[string(k)+"/"+sd.Name] = append(siteOf[string(k)+"/"+sd.Name], s.Name)
			}
			snaps, _ := dbsnap.List(root, "", "")
			count := map[string]int{}
			for _, s := range snaps {
				count[string(s.Server)+"/"+s.Database]++
			}
			type row struct {
				Server    string   `json:"server"`
				Name      string   `json:"name"`
				Tables    int      `json:"tables"`
				Bytes     int64    `json:"bytes"`
				Sites     []string `json:"sites"`
				Snapshots int      `json:"snapshots"`
			}
			out := []row{}
			for _, srv := range servers {
				c, err := dbReady(root, srv.Kind)
				if err != nil {
					return fmt.Errorf("%s: %w", srv.Label(), err)
				}
				dbs, err := c.Databases(timeoutCtx(30 * time.Second))
				if err != nil {
					return fmt.Errorf("%s: %w", srv.Label(), err)
				}
				for _, d := range dbs {
					key := string(srv.Kind) + "/" + d.Name
					s := siteOf[key]
					if s == nil {
						s = []string{}
					}
					out = append(out, row{string(srv.Kind), d.Name, d.Tables, d.Bytes, s, count[key]})
				}
			}
			if jsonOut {
				return emit(out)
			}
			if len(out) == 0 {
				fmt.Println("No databases yet. `ebur new <name> --laravel` creates one, or use Adminer.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVER\tDATABASE\tTABLES\tSIZE\tSITE\tSNAPSHOTS")
			for _, r := range out {
				k, _ := dbserver.ParseKind(r.Server)
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%d\n", k.Name(), r.Name, r.Tables, humanBytes(r.Bytes), joinOr(r.Sites, "-"), r.Snapshots)
			}
			return w.Flush()
		},
	}

	snapshot := &cobra.Command{
		Use:   "snapshot <site|database> [label]",
		Short: "Save a snapshot of a database (TOOL-05)",
		Example: "  ebur db snapshot shop                    shop's database, named by date and time\n" +
			"  ebur db snapshot shop before upgrade     with a label\n" +
			"  ebur db snapshot legacy --server mysql   a database on MySQL\n" +
			"  ebur db restore shop before-upgrade      put it back later",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			kind, db, note, err := dbFor(root, args[0])
			if err != nil {
				return err
			}
			c, err := dbReady(root, kind)
			if err != nil {
				return err
			}
			if note != "" {
				fmt.Println(note)
			}
			ctx, cancel := signalContext()
			defer cancel()
			t0 := time.Now()
			s, err := dbsnap.Save(ctx, c, root, db, strings.Join(args[1:], " "))
			if err != nil {
				return &hintErr{err.Error(), "Run `ebur db list` to see databases."}
			}
			fmt.Printf("Saved snapshot %s of %s on %s (%s, %v).\n", s.Name, db, kind.Name(), humanBytes(s.Bytes), time.Since(t0).Round(10*time.Millisecond))
			fmt.Printf("Restore it with: ebur db restore %s %s\n", args[0], s.Name)
			return nil
		},
	}

	snapshots := &cobra.Command{
		Use:   "snapshots [site|database]",
		Short: "List saved snapshots",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			cfg, _ := config.Load(root.Config())
			kind, db := dbserver.Kind(""), ""
			if len(args) == 1 {
				if kind, db, _, err = dbFor(root, args[0]); err != nil {
					return err
				}
			} else if kind, err = forcedServer(root, cfg); err != nil {
				return err
			}
			all, err := dbsnap.List(root, kind, db)
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(all)
			}
			if len(all) == 0 {
				fmt.Println("No snapshots yet. Save one with `ebur db snapshot <site|database> [label]`.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVER\tDATABASE\tSNAPSHOT\tSAVED\tSIZE\tNOTE")
			for _, s := range all {
				note := ""
				if s.Auto {
					note = "saved automatically before a restore"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", s.Server.Name(), s.Database, s.Name, s.Time.Format("2006-01-02 15:04"), humanBytes(s.Bytes), note)
			}
			w.Flush()
			fmt.Printf("\nStored in %s\n", dbsnap.Dir(root, dbserver.MariaDB))
			return nil
		},
	}

	restore := &cobra.Command{
		Use:   "restore <site|database> <snapshot|latest>",
		Short: "Replace a database with a snapshot; the current data is saved first",
		Example: "  ebur db restore shop before-upgrade      by label (the newest with it)\n" +
			"  ebur db restore shop 2026-11-25_143012   by name\n" +
			"  ebur db restore shop latest",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			kind, db, note, err := dbFor(root, args[0])
			if err != nil {
				return err
			}
			c, err := dbReady(root, kind)
			if err != nil {
				return err
			}
			if note != "" {
				fmt.Println(note)
			}
			ctx, cancel := signalContext()
			defer cancel()
			t0 := time.Now()
			res, err := dbsnap.Restore(ctx, c, root, db, args[1])
			if res.Backup != nil {
				fmt.Printf("Saved the current data first as snapshot %s.\n", res.Backup.Name)
			}
			if err != nil {
				return &hintErr{err.Error(), fmt.Sprintf("Run `ebur db snapshots %s` to see snapshots.", args[0])}
			}
			fmt.Printf("Restored %s on %s from %s in %v.\n", db, kind.Name(), res.Restored.Name, time.Since(t0).Round(10*time.Millisecond))
			if res.Backup != nil {
				fmt.Printf("To undo: ebur db restore %s %s\n", args[0], res.Backup.Name)
			}
			return nil
		},
	}

	del := &cobra.Command{
		Use:   "delete-snapshot <site|database> <snapshot>",
		Short: "Delete one saved snapshot (the database is not touched)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			kind, db, _, err := dbFor(root, args[0])
			if err != nil {
				return err
			}
			if args[1] == "latest" {
				return errors.New("name the snapshot to delete exactly; see `ebur db snapshots`")
			}
			s, err := dbsnap.Delete(root, kind, db, args[1])
			if err != nil {
				return &hintErr{err.Error(), fmt.Sprintf("Run `ebur db snapshots %s` to see snapshots.", args[0])}
			}
			fmt.Printf("Deleted snapshot %s of %s on %s.\n", s.Name, db, kind.Name())
			return nil
		},
	}
	return []*cobra.Command{supportsJSON(list), snapshot, supportsJSON(snapshots), restore, del}
}
