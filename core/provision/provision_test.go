package provision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
)

func TestPassword(t *testing.T) {
	a, _ := password(20)
	b, _ := password(20)
	if len(a) != 20 || a == b {
		t.Fatalf("%q %q", a, b)
	}
	for _, c := range a {
		if !strings.ContainsRune(pwChars, c) {
			t.Fatalf("unexpected char %q", c)
		}
	}
}

func TestSeriesAndPaths(t *testing.T) {
	if Series("12.3.4") != "12.3" || Series("12") != "12" {
		t.Fatal(Series("12.3.4"))
	}
	root, _ := paths.New(t.TempDir())
	d, ini := MariaDBPaths(root, "12.3.4")
	if filepath.Base(d) != "12.3" || filepath.Base(ini) != "my.ini" {
		t.Fatal(d, ini)
	}
}

func TestMyIni(t *testing.T) {
	p := filepath.Join(t.TempDir(), "etc", "my.ini")
	if err := writeMyIni(p, `C:\Users\Jane Doe\ebur\data\mariadb\12.3`, 3307); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{`datadir="C:/Users/Jane Doe/ebur/data/mariadb/12.3"`, "bind-address=127.0.0.1", "port=3307", "innodb_log_file_size=48M"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestTrimMariaDB(t *testing.T) {
	dir := t.TempDir()
	files := map[string]int{
		`bin\mariadbd.exe`: 100, `bin\mariadbd.pdb`: 1000, `bin\mysql.exe`: 50, `bin\server.dll`: 40,
		`lib\libmariadb.lib`: 300, `lib\libmariadb.dll`: 30, `lib\plugin\auth_ed25519.dll`: 20, `lib\plugin\x.pdb`: 7,
		`include\mysql\mysql.h`: 70, `include\mysql.h`: 5, `share\english\errmsg.sys`: 60,
	}
	for name, n := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, make([]byte, n), 0o644)
	}
	outside := filepath.Join(t.TempDir(), "keep.pdb")
	os.WriteFile(outside, []byte("x"), 0o644)

	freed, err := TrimMariaDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 1000+300+7+70+5 {
		t.Errorf("freed %d", freed)
	}
	for name := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		gone := strings.HasSuffix(name, ".pdb") || strings.HasSuffix(name, ".lib") || strings.HasPrefix(name, "include")
		if gone != os.IsNotExist(err) {
			t.Errorf("%s: gone=%v, want %v", name, os.IsNotExist(err), gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "include")); !os.IsNotExist(err) {
		t.Error("include folder kept")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("a file outside the runtime folder was touched")
	}
	if again, err := TrimMariaDB(dir); err != nil || again != 0 {
		t.Errorf("second run: %d, %v", again, err)
	}
}

// With an existing data folder, MariaDB setup only ensures a password and
// my.ini; it must not touch the databases.
func TestMariaDBReusesData(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	data, ini := MariaDBPaths(root, "12.3.4")
	os.MkdirAll(filepath.Join(data, "mysql"), 0o755)
	rep, err := Prepare(context.Background(), root, "mariadb", "12.3.4", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(root.Config())
	if len(cfg.DBRootPassword) != 20 {
		t.Fatal("no password generated")
	}
	if _, err := os.Stat(ini); err != nil {
		t.Fatal("my.ini not written")
	}
	if !strings.Contains(strings.Join(rep.Steps, ";"), "reusing") {
		t.Fatalf("%+v", rep)
	}
	// A second run keeps the same password.
	Prepare(context.Background(), root, "mariadb", "12.3.4", nil)
	cfg2, _ := config.Load(root.Config())
	if cfg2.DBRootPassword != cfg.DBRootPassword {
		t.Fatal("password changed")
	}
}

func TestUnknownRuntimeIsNoop(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	if rep, err := Prepare(context.Background(), root, "adminer", "6.3.1", nil); err != nil || len(rep.Steps) != 0 {
		t.Fatal(rep, err)
	}
}

func TestPHPMissingBinary(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	dir := root.RuntimeDir("php", "8.4.28")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "php.ini-development"), []byte(""), 0o644)
	if _, err := Prepare(context.Background(), root, "php", "8.4.28", nil); err == nil || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("got %v", err)
	}
}
