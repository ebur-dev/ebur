package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ebur-dev/ebur/core/certs"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/dbclient"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/dbsnap"
	"github.com/ebur-dev/ebur/core/install"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/sites"
)

// DBServerInfo is one database server as the window shows it. MariaDB and
// MySQL can both be installed and run side by side (SVC-01).
type DBServerInfo struct {
	Kind    string `json:"kind"` // mariadb or mysql
	Name    string `json:"name"` // e.g. "MySQL 8.4.13.1"
	Version string `json:"version"`
	Port    int    `json:"port"`
	Default bool   `json:"default"` // used by new projects
	Running bool   `json:"running"`
	EOL     string `json:"eol"` // set when past end of life
}

// MySQLOption is a MySQL version the Settings card can install.
type MySQLOption struct {
	Version   string `json:"version"`
	Series    string `json:"series"`
	Legacy    bool   `json:"legacy"`
	EOL       string `json:"eol"`
	Installed bool   `json:"installed"`
}

// DBInfo is the Settings tab's Database card.
type DBInfo struct {
	Servers  []DBServerInfo `json:"servers"`
	User     string         `json:"user"`
	Password string         `json:"password"`
	Adminer  string         `json:"adminer"`
	MySQL    []MySQLOption  `json:"mysql"` // versions Ebur offers
}

func (a *App) servers(ping bool) []DBServerInfo {
	cfg, _ := config.Load(a.root.Config())
	def, _ := dbserver.Default(a.root, cfg)
	out := []DBServerInfo{}
	for _, s := range dbserver.Installed(a.root, cfg) {
		info := DBServerInfo{Kind: string(s.Kind), Name: s.Label(), Version: s.Version, Port: s.Port, Default: s.Kind == def.Kind}
		if sup := install.SupportOf(string(s.Kind), s.Version); sup.Ended {
			info.EOL = sup.EOL
		}
		if ping {
			if c, err := dbclient.OpenServer(a.root, cfg, s); err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				info.Running = c.Ping(ctx) == nil
				cancel()
			}
		}
		out = append(out, info)
	}
	return out
}

func (a *App) Database() DBInfo {
	cfg, _ := config.Load(a.root.Config())
	info := DBInfo{Servers: a.servers(false), User: "root", Password: cfg.DBRootPassword, MySQL: []MySQLOption{}}
	if ad, _ := runtimes.Installed(a.root, "adminer"); len(ad) > 0 {
		scheme := "http"
		if certs.Dir(a.root.SSL()).Exists() {
			scheme = "https"
		}
		info.Adminer = scheme + "://db." + cfg.TLD
	}
	installed, _ := runtimes.Installed(a.root, "mysql")
	if m, err := install.Manifest(); err == nil {
		for _, r := range m.Releases("mysql", "x64") {
			o := MySQLOption{Version: r.Version, Series: dbserver.Series(r.Version), EOL: r.EOL, Legacy: r.EndOfLife(time.Now())}
			for _, v := range installed {
				o.Installed = o.Installed || v == r.Version
			}
			info.MySQL = append(info.MySQL, o)
		}
	}
	return info
}

// InstallMySQL installs a MySQL series (e.g. "8.4") next to MariaDB and
// makes it the MySQL that runs. Running services restart so it starts.
func (a *App) InstallMySQL(series string) (DBInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := install.Runtime(ctx, a.root, "mysql", series, nil, nil)
	if err != nil {
		return a.Database(), withHint(err)
	}
	if cfg, err := config.Load(a.root.Config()); err == nil && cfg.Active["mysql"] != res.Version {
		cfg.Active["mysql"] = res.Version // the one just installed is the one wanted
		config.Save(a.root.Config(), cfg)
	}
	a.restartIfRunning()
	return a.Database(), a.after(nil)
}

// SetMySQL switches to another installed MySQL version.
func (a *App) SetMySQL(version string) (DBInfo, error) {
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return a.Database(), err
	}
	installed, _ := runtimes.Installed(a.root, "mysql")
	found := false
	for _, v := range installed {
		found = found || v == version
	}
	if !found {
		return a.Database(), fmt.Errorf("MySQL %s is not installed", version)
	}
	cfg.Active["mysql"] = version
	if err := config.Save(a.root.Config(), cfg); err != nil {
		return a.Database(), err
	}
	a.restartIfRunning()
	return a.Database(), a.after(nil)
}

// SetDefaultDB picks the server new projects use: "mariadb" or "mysql".
func (a *App) SetDefaultDB(kind string) (DBInfo, error) {
	k, ok := dbserver.ParseKind(kind)
	if !ok {
		return a.Database(), fmt.Errorf("unknown server %q", kind)
	}
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return a.Database(), err
	}
	cfg.DefaultDB = string(k)
	return a.Database(), a.after(config.Save(a.root.Config(), cfg))
}

// DBRow is one database on the Databases tab (TOOL-05).
type DBRow struct {
	Server     string            `json:"server"` // mariadb or mysql
	ServerName string            `json:"serverName"`
	Name       string            `json:"name"`
	Tables     int               `json:"tables"`
	Bytes      int64             `json:"bytes"`
	Sites      []string          `json:"sites"`
	Snapshots  []dbsnap.Snapshot `json:"snapshots"`
}

// DBList is the Databases tab: every server's databases.
type DBList struct {
	Installed bool           `json:"installed"`
	Running   bool           `json:"running"` // at least one server answered
	Servers   []DBServerInfo `json:"servers"`
	Databases []DBRow        `json:"databases"`
	Folder    string         `json:"folder"`
}

func (a *App) Databases() DBList {
	cfg, _ := config.Load(a.root.Config())
	out := DBList{Databases: []DBRow{}, Folder: dbsnap.Dir(a.root, dbserver.MariaDB), Servers: a.servers(true)}
	servers := dbserver.Installed(a.root, cfg)
	out.Installed = len(servers) > 0
	def, _ := dbserver.Default(a.root, cfg)
	found, _, _ := sites.Scan(a.root.WWW(), cfg.TLD)
	siteOf := map[string][]string{} // "kind/db" -> sites
	for _, s := range found {
		sd := dbsnap.SiteDatabase(s.Dir, s.Name)
		k := def.Kind
		if srv, ok := dbserver.ForPort(servers, sd.Port); ok {
			k = srv.Kind
		}
		siteOf[string(k)+"/"+sd.Name] = append(siteOf[string(k)+"/"+sd.Name], s.Name)
	}
	snaps, _ := dbsnap.List(a.root, "", "")
	for _, srv := range servers {
		c, err := dbclient.OpenServer(a.root, cfg, srv)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		dbs, err := c.Databases(ctx)
		cancel()
		if err != nil {
			continue
		}
		out.Running = true
		for _, d := range dbs {
			key := string(srv.Kind) + "/" + d.Name
			row := DBRow{Server: string(srv.Kind), ServerName: srv.Label(), Name: d.Name, Tables: d.Tables, Bytes: d.Bytes, Sites: siteOf[key], Snapshots: []dbsnap.Snapshot{}}
			if row.Sites == nil {
				row.Sites = []string{}
			}
			for _, s := range snaps {
				if s.Server == srv.Kind && s.Database == d.Name {
					row.Snapshots = append(row.Snapshots, s)
				}
			}
			out.Databases = append(out.Databases, row)
		}
	}
	return out
}

func (a *App) dbClient(server string) (*dbclient.Client, error) {
	k, ok := dbserver.ParseKind(server)
	if !ok {
		return nil, fmt.Errorf("unknown server %q", server)
	}
	c, err := dbclient.OpenKind(a.root, k)
	if err != nil {
		return nil, withHint(err)
	}
	return c, nil
}

// SnapshotDB saves a snapshot of db on server with an optional label.
func (a *App) SnapshotDB(server, db, label string) (dbsnap.Snapshot, error) {
	c, err := a.dbClient(server)
	if err != nil {
		return dbsnap.Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s, err := dbsnap.Save(ctx, c, a.root, db, label)
	return s, withHint(err)
}

// RestoreDB replaces db with a snapshot; the current data is saved first.
func (a *App) RestoreDB(server, db, name string) (dbsnap.RestoreResult, error) {
	c, err := a.dbClient(server)
	if err != nil {
		return dbsnap.RestoreResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := dbsnap.Restore(ctx, c, a.root, db, name)
	return res, withHint(err)
}

// DeleteSnapshot removes one snapshot file; the database is not touched.
func (a *App) DeleteSnapshot(server, db, name string) error {
	k, ok := dbserver.ParseKind(server)
	if !ok {
		return fmt.Errorf("unknown server %q", server)
	}
	_, err := dbsnap.Delete(a.root, k, db, name)
	return withHint(err)
}

// OpenSnapshots opens the snapshots folder in Explorer.
func (a *App) OpenSnapshots() error {
	dir := dbsnap.Dir(a.root, dbserver.MariaDB)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return openFolder(dir)
}
