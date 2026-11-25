package dbsnap

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/paths"
)

func TestCleanLabel(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"Before upgrade!":        "before-upgrade",
		"  v2 -- seed data":      "v2-seed-data",
		strings.Repeat("ab", 30): strings.Repeat("ab", 20),
	} {
		if got, err := CleanLabel(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	if _, err := CleanLabel("!!!"); err == nil {
		t.Error("label without letters accepted")
	}
}

// fake writes a snapshot file as Save would name it.
func fake(t *testing.T, root paths.Root, db, name, sql string) {
	t.Helper()
	dir := filepath.Join(Dir(root, dbserver.MariaDB), db)
	os.MkdirAll(dir, 0o755)
	f, err := os.Create(filepath.Join(dir, name+ext))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	zw.Write([]byte(sql))
	zw.Close()
	f.Close()
}

func TestListFindDelete(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	fake(t, root, "shop", "2026-11-20_090000", "a")
	fake(t, root, "shop", "2026-11-24_101500_before-upgrade", "b")
	fake(t, root, "shop", "2026-11-25_120000_before-restore", "c")
	fake(t, root, "blog", "2026-11-25_130000", "d")
	os.WriteFile(filepath.Join(Dir(root, dbserver.MariaDB), "shop", "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(Dir(root, dbserver.MariaDB), "shop", "garbage.sql.gz"), []byte("x"), 0o644)

	shop, err := List(root, dbserver.MariaDB, "shop")
	if err != nil || len(shop) != 3 || shop[0].Name != "2026-11-25_120000_before-restore" || !shop[0].Auto || shop[2].Label != "" {
		t.Fatalf("list shop: %+v %v", shop, err)
	}
	if want := time.Date(2026, 11, 24, 10, 15, 0, 0, time.Local); !shop[1].Time.Equal(want) || shop[1].Label != "before-upgrade" {
		t.Fatalf("parsed %+v", shop[1])
	}
	if all, _ := List(root, dbserver.MariaDB, ""); len(all) != 4 || all[0].Database != "blog" {
		t.Fatalf("list all: %+v", all)
	}
	for ref, want := range map[string]string{
		"latest":                           "2026-11-25_120000_before-restore",
		"before-upgrade":                   "2026-11-24_101500_before-upgrade",
		"2026-11-20":                       "2026-11-20_090000",
		"2026-11-20_090000.sql.gz":         "2026-11-20_090000",
		"2026-11-24_101500_before-upgrade": "2026-11-24_101500_before-upgrade",
	} {
		if s, err := Find(root, dbserver.MariaDB, "shop", ref); err != nil || s.Name != want {
			t.Errorf("find %q: %v %v", ref, s.Name, err)
		}
	}
	if _, err := Find(root, dbserver.MariaDB, "shop", "2026-11-2"); err == nil || !strings.Contains(err.Error(), "matches 3") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := Find(root, dbserver.MariaDB, "shop", "nope"); err == nil {
		t.Error("missing snapshot found")
	}
	if _, err := Find(root, dbserver.MariaDB, "empty", "latest"); err == nil || !strings.Contains(err.Error(), "no snapshots") {
		t.Errorf("empty: %v", err)
	}
	for _, bad := range []string{"../shop", "mysql", "a b"} {
		if _, err := List(root, dbserver.MariaDB, bad); err == nil {
			t.Errorf("database %q accepted", bad)
		}
	}
	if _, err := Delete(root, dbserver.MariaDB, "blog", "latest"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir(root, dbserver.MariaDB), "blog")); !os.IsNotExist(err) {
		t.Error("empty folder kept")
	}
	if err := checkGzip(filepath.Join(Dir(root, dbserver.MariaDB), "shop", "2026-11-20_090000"+ext)); err != nil {
		t.Error(err)
	}
	os.WriteFile(filepath.Join(Dir(root, dbserver.MariaDB), "shop", "2026-11-26_000000"+ext), []byte("\x1f\x8b broken"), 0o644)
	if err := checkGzip(filepath.Join(Dir(root, dbserver.MariaDB), "shop", "2026-11-26_000000"+ext)); err == nil {
		t.Error("damaged file passed")
	}
}

func TestSiteDatabase(t *testing.T) {
	cases := []struct {
		files map[string]string
		want  SiteDB
	}{
		{nil, SiteDB{"my_shop", 0, "site name"}},
		{map[string]string{".env": "DB_CONNECTION=mysql\nDB_PORT=3306\nDB_DATABASE=\"lara_demo\"\n"}, SiteDB{"lara_demo", 3306, ".env DB_DATABASE"}},
		{map[string]string{".env": "DB_CONNECTION=sqlite\nDB_DATABASE=/x/db.sqlite\n"}, SiteDB{"my_shop", 0, "site name"}},
		{map[string]string{".env": "DATABASE_URL=\"mysql://root:pw@127.0.0.1:3307/app?serverVersion=x\"", ".env.local": "DATABASE_URL=\"mysql://root:pw@127.0.0.1:3308/sf_demo?serverVersion=12-MariaDB\""}, SiteDB{"sf_demo", 3308, ".env.local DATABASE_URL"}},
		{map[string]string{".env": "DATABASE_URL=\"postgresql://app@127.0.0.1/app\""}, SiteDB{"my_shop", 0, "site name"}},
		{map[string]string{"wp-config.php": "<?php\ndefine( 'DB_NAME', 'wp_demo' );\ndefine( 'DB_HOST', '127.0.0.1:3307' );\n"}, SiteDB{"wp_demo", 3307, "wp-config.php"}},
		{map[string]string{"wp-config.php": "<?php\ndefine( 'DB_NAME', 'wp_demo' );\ndefine( 'DB_HOST', 'localhost' );\n"}, SiteDB{"wp_demo", 0, "wp-config.php"}},
	}
	for i, c := range cases {
		dir := t.TempDir()
		for n, body := range c.files {
			os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644)
		}
		if got := SiteDatabase(dir, "my-shop"); got != c.want {
			t.Errorf("case %d: got %+v, want %+v", i, got, c.want)
		}
	}
}

// MariaDB and MySQL databases with the same name keep separate snapshots.
func TestSnapshotsPerServer(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	fake(t, root, "shop", "2026-11-20_090000", "maria")
	dir := filepath.Join(Dir(root, dbserver.MySQL), "shop")
	os.MkdirAll(dir, 0o755)
	os.Rename(filepath.Join(Dir(root, dbserver.MariaDB), "shop", "2026-11-20_090000"+ext), filepath.Join(dir, "2026-11-21_090000"+ext))
	fake(t, root, "shop", "2026-11-20_090000", "maria")
	m, _ := List(root, dbserver.MariaDB, "shop")
	y, _ := List(root, dbserver.MySQL, "shop")
	both, _ := List(root, "", "")
	if len(m) != 1 || len(y) != 1 || m[0].Server != dbserver.MariaDB || y[0].Server != dbserver.MySQL || len(both) != 2 || both[0].Server != dbserver.MySQL {
		t.Fatalf("mariadb %+v\nmysql %+v\nboth %+v", m, y, both)
	}
	// The MySQL folder is never mistaken for a MariaDB database.
	for _, s := range m {
		if s.Database == ".mysql" {
			t.Error(".mysql listed as a database")
		}
	}
	if all, _ := List(root, dbserver.MariaDB, ""); len(all) != 1 {
		t.Errorf("mariadb all: %+v", all)
	}
}
