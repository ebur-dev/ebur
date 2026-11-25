package dbserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
)

func TestServers(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	cfg := config.Defaults()
	cfg.DBPort, cfg.MySQLPort = 3307, 3306
	if _, ok := Default(root, cfg); ok {
		t.Fatal("no server installed, but a default was found")
	}
	os.MkdirAll(root.RuntimeDir("mariadb", "12.3.4"), 0o755)
	os.MkdirAll(root.RuntimeDir("mysql", "8.4.13.1"), 0o755)
	os.MkdirAll(root.RuntimeDir("mysql", "5.7.44"), 0o755)

	all := Installed(root, cfg)
	if len(all) != 2 || all[0].Kind != MariaDB || all[1].Kind != MySQL {
		t.Fatalf("installed %+v", all)
	}
	my := all[1]
	if my.Version != "8.4.13.1" || my.Port != 3306 || my.Label() != "MySQL 8.4.13.1" {
		t.Fatalf("newest MySQL should be active: %+v", my)
	}
	if !strings.HasSuffix(my.DataDir, filepath.Join("data", "mysql", "8.4")) {
		t.Errorf("data dir %s", my.DataDir)
	}
	if !strings.HasSuffix(my.ToolPath(Dump), filepath.Join("bin", "mysqldump.exe")) && !strings.HasSuffix(my.ToolPath(Dump), filepath.Join("bin", "mysqldump")) {
		t.Errorf("dump tool %s", my.ToolPath(Dump))
	}
	if !strings.Contains(all[0].ToolPath(Client), "mariadb") {
		t.Errorf("mariadb client %s", all[0].ToolPath(Client))
	}
	// The configured version wins over the newest.
	cfg.Active["mysql"] = "5.7.44"
	if s, _ := Get(root, cfg, MySQL); s.Version != "5.7.44" || !strings.HasSuffix(s.DataDir, filepath.Join("mysql", "5.7")) {
		t.Errorf("active 5.7: %+v", s)
	}
	// Default: MariaDB unless configured otherwise.
	if d, _ := Default(root, cfg); d.Kind != MariaDB {
		t.Errorf("default %s", d.Kind)
	}
	cfg.DefaultDB = "mysql"
	if d, _ := Default(root, cfg); d.Kind != MySQL {
		t.Errorf("default mysql: %s", d.Kind)
	}
	if s, ok := ForPort(all, 3307); !ok || s.Kind != MariaDB {
		t.Errorf("port 3307: %+v", s)
	}
	if _, ok := ForPort(all, 9999); ok {
		t.Error("unknown port matched")
	}
	for in, want := range map[string]Kind{"MySQL": MySQL, " mariadb ": MariaDB} {
		if k, ok := ParseKind(in); !ok || k != want {
			t.Errorf("ParseKind(%q) = %q", in, k)
		}
	}
	if _, ok := ParseKind("postgres"); ok {
		t.Error("postgres accepted")
	}
	if Series("8.4.13.1") != "8.4" || Series("5.7.44") != "5.7" {
		t.Error("series")
	}
}
