package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/paths"
)

func TestMySQLIni(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	ini84, err := WriteMySQLIni(root, "8.4.13.1", 3306)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ini84)
	for _, want := range []string{"port=3306", "bind-address=127.0.0.1", "skip-log-bin", "mysqlx=OFF", "performance_schema=OFF", `datadir="` + filepath.ToSlash(filepath.Join(root.Data(), "mysql", "8.4")) + `"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("8.4 my.ini missing %q", want)
		}
	}
	ini57, _ := WriteMySQLIni(root, "5.7.44", 3308)
	b, _ = os.ReadFile(ini57)
	// 5.7 refuses unknown options: no MySQL 8 settings there.
	if strings.Contains(string(b), "mysqlx") || strings.Contains(string(b), "skip-log-bin") || !strings.Contains(string(b), "explicit_defaults_for_timestamp=ON") {
		t.Errorf("5.7 my.ini:\n%s", b)
	}
	if filepath.Dir(ini84) == filepath.Dir(ini57) {
		t.Error("versions share a config folder")
	}
}

func TestClientCnf(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	p, err := WriteClientCnf(root, dbserver.MySQL, "8.4.13.1", 3306, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "[client]") || !strings.Contains(string(b), `password="s3cret"`) || !strings.Contains(string(b), "port=3306") {
		t.Errorf("client.cnf:\n%s", b)
	}
}

func TestTrimMySQL(t *testing.T) {
	dir := t.TempDir()
	files := map[string]int{
		`bin\mysqld.exe`: 100, `bin\mysqld.pdb`: 5000, `bin\mysqldump.exe`: 30, `bin\libssl.dll`: 20,
		`lib\mysqlclient.lib`: 700, `lib\plugin\auth.dll`: 10, `lib\plugin\debug\auth.dll`: 11, `lib\plugin\debug\auth.pdb`: 12,
		`include\mysql.h`: 9, `share\english\errmsg.sys`: 40,
	}
	for name, n := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, make([]byte, n), 0o644)
	}
	freed, err := TrimMySQL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 5000+700+11+12+9 {
		t.Errorf("freed %d", freed)
	}
	for _, keep := range []string{`bin\mysqld.exe`, `bin\mysqldump.exe`, `bin\libssl.dll`, `lib\plugin\auth.dll`, `share\english\errmsg.sys`} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s removed", keep)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "lib", "plugin", "debug")); !os.IsNotExist(err) {
		t.Error("debug plugins kept")
	}
	if again, _ := TrimMySQL(dir); again != 0 {
		t.Errorf("second run freed %d", again)
	}
}
