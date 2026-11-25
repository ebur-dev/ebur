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
	"github.com/ebur-dev/ebur/core/dbsnap"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/sites"
)

// dbFor resolves a site name or database name to a database name. A site
// is mapped through its own settings (.env, wp-config.php).
func dbFor(root paths.Root, arg string) (db, note string, err error) {
	cfg, _ := config.Load(root.Config())
	found, _, _ := sites.Scan(root.WWW(), cfg.TLD)
	for _, s := range found {
		if s.Name == strings.ToLower(arg) {
			db, from := dbsnap.SiteDatabase(s.Dir, s.Name)
			return db, fmt.Sprintf("site %s uses database %s (from %s)", s.Name, db, from), nil
		}
	}
	if !dbclient.ValidName(arg) || dbclient.IsSystem(arg) {
		return "", "", &hintErr{fmt.Sprintf("%q is not a site or a database Ebur can snapshot", arg), "Run `ebur db list` to see databases."}
	}
	return arg, "", nil
}

// dbReady starts MariaDB if needed and opens a client.
func dbReady(root paths.Root) (*dbclient.Client, error) {
	c, err := dbclient.Open(root)
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
	if err := call(root, "start", selection([]string{"mariadb"}), &st); err != nil {
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
		Short: "List databases with their size, the site using each, and snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			c, err := dbReady(root)
			if err != nil {
				return err
			}
			dbs, err := c.Databases(timeoutCtx(30 * time.Second))
			if err != nil {
				return err
			}
			cfg, _ := config.Load(root.Config())
			found, _, _ := sites.Scan(root.WWW(), cfg.TLD)
			siteOf := map[string][]string{}
			for _, s := range found {
				db, _ := dbsnap.SiteDatabase(s.Dir, s.Name)
				siteOf[db] = append(siteOf[db], s.Name)
			}
			snaps, _ := dbsnap.List(root, "")
			count := map[string]int{}
			for _, s := range snaps {
				count[s.Database]++
			}
			if len(dbs) == 0 {
				fmt.Println("No databases yet. `ebur new <name> --laravel` creates one, or use Adminer.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DATABASE\tTABLES\tSIZE\tSITE\tSNAPSHOTS")
			for _, d := range dbs {
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\n", d.Name, d.Tables, humanBytes(d.Bytes), joinOr(siteOf[d.Name], "-"), count[d.Name])
			}
			return w.Flush()
		},
	}

	snapshot := &cobra.Command{
		Use:   "snapshot <site|database> [label]",
		Short: "Save a snapshot of a database (TOOL-05)",
		Example: "  ebur db snapshot shop                    shop's database, named by date and time\n" +
			"  ebur db snapshot shop before upgrade     with a label\n" +
			"  ebur db restore shop before-upgrade      put it back later",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}
			db, note, err := dbFor(root, args[0])
			if err != nil {
				return err
			}
			c, err := dbReady(root)
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
			fmt.Printf("Saved snapshot %s of %s (%s, %v).\n", s.Name, db, humanBytes(s.Bytes), time.Since(t0).Round(10*time.Millisecond))
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
			db := ""
			if len(args) == 1 {
				if db, _, err = dbFor(root, args[0]); err != nil {
					return err
				}
			}
			all, err := dbsnap.List(root, db)
			if err != nil {
				return err
			}
			if len(all) == 0 {
				fmt.Println("No snapshots yet. Save one with `ebur db snapshot <site|database> [label]`.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DATABASE\tSNAPSHOT\tSAVED\tSIZE\tNOTE")
			for _, s := range all {
				note := ""
				if s.Auto {
					note = "saved automatically before a restore"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Database, s.Name, s.Time.Format("2006-01-02 15:04"), humanBytes(s.Bytes), note)
			}
			w.Flush()
			fmt.Printf("\nStored in %s\n", dbsnap.Dir(root))
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
			db, note, err := dbFor(root, args[0])
			if err != nil {
				return err
			}
			c, err := dbReady(root)
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
			fmt.Printf("Restored %s from %s in %v.\n", db, res.Restored.Name, time.Since(t0).Round(10*time.Millisecond))
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
			db, _, err := dbFor(root, args[0])
			if err != nil {
				return err
			}
			if args[1] == "latest" {
				return errors.New("name the snapshot to delete exactly; see `ebur db snapshots`")
			}
			s, err := dbsnap.Delete(root, db, args[1])
			if err != nil {
				return &hintErr{err.Error(), fmt.Sprintf("Run `ebur db snapshots %s` to see snapshots.", args[0])}
			}
			fmt.Printf("Deleted snapshot %s of %s.\n", s.Name, db)
			return nil
		},
	}
	return []*cobra.Command{list, snapshot, snapshots, restore, del}
}
