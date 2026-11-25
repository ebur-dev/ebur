package dbsnap

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	dir := filepath.Join(Dir(root), db)
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
	os.WriteFile(filepath.Join(Dir(root), "shop", "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(Dir(root), "shop", "garbage.sql.gz"), []byte("x"), 0o644)

	shop, err := List(root, "shop")
	if err != nil || len(shop) != 3 || shop[0].Name != "2026-11-25_120000_before-restore" || !shop[0].Auto || shop[2].Label != "" {
		t.Fatalf("list shop: %+v %v", shop, err)
	}
	if want := time.Date(2026, 11, 24, 10, 15, 0, 0, time.Local); !shop[1].Time.Equal(want) || shop[1].Label != "before-upgrade" {
		t.Fatalf("parsed %+v", shop[1])
	}
	if all, _ := List(root, ""); len(all) != 4 || all[0].Database != "blog" {
		t.Fatalf("list all: %+v", all)
	}
	for ref, want := range map[string]string{
		"latest":                           "2026-11-25_120000_before-restore",
		"before-upgrade":                   "2026-11-24_101500_before-upgrade",
		"2026-11-20":                       "2026-11-20_090000",
		"2026-11-20_090000.sql.gz":         "2026-11-20_090000",
		"2026-11-24_101500_before-upgrade": "2026-11-24_101500_before-upgrade",
	} {
		if s, err := Find(root, "shop", ref); err != nil || s.Name != want {
			t.Errorf("find %q: %v %v", ref, s.Name, err)
		}
	}
	if _, err := Find(root, "shop", "2026-11-2"); err == nil || !strings.Contains(err.Error(), "matches 3") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := Find(root, "shop", "nope"); err == nil {
		t.Error("missing snapshot found")
	}
	if _, err := Find(root, "empty", "latest"); err == nil || !strings.Contains(err.Error(), "no snapshots") {
		t.Errorf("empty: %v", err)
	}
	for _, bad := range []string{"../shop", "mysql", "a b"} {
		if _, err := List(root, bad); err == nil {
			t.Errorf("database %q accepted", bad)
		}
	}
	if _, err := Delete(root, "blog", "latest"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir(root), "blog")); !os.IsNotExist(err) {
		t.Error("empty folder kept")
	}
	if err := checkGzip(filepath.Join(Dir(root), "shop", "2026-11-20_090000"+ext)); err != nil {
		t.Error(err)
	}
	os.WriteFile(filepath.Join(Dir(root), "shop", "2026-11-26_000000"+ext), []byte("\x1f\x8b broken"), 0o644)
	if err := checkGzip(filepath.Join(Dir(root), "shop", "2026-11-26_000000"+ext)); err == nil {
		t.Error("damaged file passed")
	}
}

func TestSiteDatabase(t *testing.T) {
	cases := []struct {
		files      map[string]string
		name, from string
	}{
		{nil, "my_shop", "site name"},
		{map[string]string{".env": "DB_CONNECTION=mysql\nDB_DATABASE=\"lara_demo\"\n"}, "lara_demo", ".env DB_DATABASE"},
		{map[string]string{".env": "DB_CONNECTION=sqlite\nDB_DATABASE=/x/db.sqlite\n"}, "my_shop", "site name"},
		{map[string]string{".env": "DATABASE_URL=\"mysql://root:pw@127.0.0.1:3307/app?serverVersion=x\"", ".env.local": "DATABASE_URL=\"mysql://root:pw@127.0.0.1:3307/sf_demo?serverVersion=12-MariaDB\""}, "sf_demo", ".env.local DATABASE_URL"},
		{map[string]string{".env": "DATABASE_URL=\"postgresql://app@127.0.0.1/app\""}, "my_shop", "site name"},
		{map[string]string{"wp-config.php": "<?php\ndefine( 'DB_NAME', 'wp_demo' );\n"}, "wp_demo", "wp-config.php"},
	}
	for i, c := range cases {
		dir := t.TempDir()
		for n, body := range c.files {
			os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644)
		}
		if name, from := SiteDatabase(dir, "my-shop"); name != c.name || from != c.from {
			t.Errorf("case %d: got %s from %s", i, name, from)
		}
	}
}
