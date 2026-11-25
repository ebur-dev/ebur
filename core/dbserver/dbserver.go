// Package dbserver describes the database servers Ebur runs: MariaDB and
// MySQL can be installed side by side, each on its own port (SVC-01), so
// one project can match a MySQL 8 production server while another uses
// MariaDB. Everything that talks to a database asks this package which
// servers exist, where their tools are and which port they use.
package dbserver

import (
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/internal/proc"
)

// Kind is a database server product.
type Kind string

const (
	MariaDB Kind = "mariadb"
	MySQL   Kind = "mysql"
)

// Kinds lists the servers in the order menus show them.
var Kinds = []Kind{MariaDB, MySQL}

// Name is the product name, e.g. "MySQL".
func (k Kind) Name() string {
	if k == MySQL {
		return "MySQL"
	}
	return "MariaDB"
}

// Server is one installed database server.
type Server struct {
	Kind    Kind   `json:"kind"`
	Version string `json:"version"`
	Port    int    `json:"port"`
	Dir     string `json:"-"` // runtime folder, e.g. bin\mysql\8.4.13.1
	DataDir string `json:"data_dir"`
	Config  string `json:"-"` // my.ini
}

// Label is e.g. "MySQL 8.4.13.1".
func (s Server) Label() string { return s.Kind.Name() + " " + s.Version }

// Tool is one of the server's programs.
type Tool string

const (
	Client Tool = "client" // mariadb.exe / mysql.exe
	Dump   Tool = "dump"   // mariadb-dump.exe / mysqldump.exe
	Admin  Tool = "admin"  // mariadb-admin.exe / mysqladmin.exe
	Daemon Tool = "daemon" // mariadbd.exe / mysqld.exe
)

var toolNames = map[Kind]map[Tool]string{
	MariaDB: {Client: "mariadb", Dump: "mariadb-dump", Admin: "mariadb-admin", Daemon: "mariadbd"},
	MySQL:   {Client: "mysql", Dump: "mysqldump", Admin: "mysqladmin", Daemon: "mysqld"},
}

// ToolPath returns the path of one of the server's programs.
func (s Server) ToolPath(t Tool) string {
	return filepath.Join(s.Dir, "bin", proc.Exe(toolNames[s.Kind][t]))
}

// Series returns "8.4" for "8.4.13.1": a data folder is shared by the
// patch releases of one series.
func Series(version string) string {
	p := strings.Split(version, ".")
	if len(p) >= 2 {
		return p[0] + "." + p[1]
	}
	return version
}

// Paths returns a server version's data folder and my.ini.
func Paths(root paths.Root, kind Kind, version string) (dataDir, myIni string) {
	s := Series(version)
	return filepath.Join(root.Data(), string(kind), s), filepath.Join(root.Etc(), string(kind), s, "my.ini")
}

// ActiveVersion is the version of kind in use: the configured one, or the
// newest installed. "" when none is installed.
func ActiveVersion(root paths.Root, cfg config.Settings, kind Kind) string {
	installed, _ := runtimes.Installed(root, string(kind))
	if v := cfg.Active[string(kind)]; v != "" {
		for _, i := range installed {
			if i == v {
				return v
			}
		}
	}
	if len(installed) > 0 {
		return installed[0]
	}
	return ""
}

// PortOf returns the port a kind of server uses.
func PortOf(cfg config.Settings, kind Kind) int {
	if kind == MySQL {
		return cfg.MySQLPort
	}
	return cfg.DBPort
}

// Get returns the installed server of kind.
func Get(root paths.Root, cfg config.Settings, kind Kind) (Server, bool) {
	v := ActiveVersion(root, cfg, kind)
	if v == "" {
		return Server{}, false
	}
	data, ini := Paths(root, kind, v)
	return Server{Kind: kind, Version: v, Port: PortOf(cfg, kind), Dir: root.RuntimeDir(string(kind), v), DataDir: data, Config: ini}, true
}

// Installed returns every installed server, MariaDB first.
func Installed(root paths.Root, cfg config.Settings) []Server {
	var out []Server
	for _, k := range Kinds {
		if s, ok := Get(root, cfg, k); ok {
			out = append(out, s)
		}
	}
	return out
}

// Default is the server new projects use: the configured default when it
// is installed, otherwise the first installed.
func Default(root paths.Root, cfg config.Settings) (Server, bool) {
	if cfg.DefaultDB != "" {
		if s, ok := Get(root, cfg, Kind(cfg.DefaultDB)); ok {
			return s, true
		}
	}
	all := Installed(root, cfg)
	if len(all) == 0 {
		return Server{}, false
	}
	return all[0], true
}

// ForPort returns the server listening on port, if any.
func ForPort(servers []Server, port int) (Server, bool) {
	for _, s := range servers {
		if s.Port == port {
			return s, true
		}
	}
	return Server{}, false
}

// ParseKind accepts "mysql" or "mariadb" (any case).
func ParseKind(s string) (Kind, bool) {
	switch Kind(strings.ToLower(strings.TrimSpace(s))) {
	case MariaDB:
		return MariaDB, true
	case MySQL:
		return MySQL, true
	}
	return "", false
}
