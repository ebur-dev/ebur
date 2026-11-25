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
	for _, want := range []string{`datadir="C:/Users/Jane Doe/ebur/data/mariadb/12.3"`, "bind-address=127.0.0.1", "port=3307"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q", want)
		}
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
