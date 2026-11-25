package sites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNameFor(t *testing.T) {
	cases := map[string]string{"My_Shop 2": "my-shop-2", "shop": "shop", "--Odd..Name--": "odd-name", "Ünïcode": "n-code"}
	for in, want := range cases {
		if got := NameFor(in); got != want {
			t.Errorf("NameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinkScanUnlink(t *testing.T) {
	root := t.TempDir()
	www := filepath.Join(root, "www")
	project := filepath.Join(t.TempDir(), "My_Shop")
	os.MkdirAll(filepath.Join(project, "public"), 0o755)
	os.WriteFile(filepath.Join(project, "public", "index.php"), []byte("<?php"), 0o644)

	name, err := Link(root, www, project, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "my-shop" {
		t.Fatalf("name %q", name)
	}
	found, skipped, err := Scan(www, "test")
	if err != nil || len(found) != 1 || len(skipped) != 0 {
		t.Fatalf("scan: %+v %+v %v", found, skipped, err)
	}
	s := found[0]
	if s.Host != "my-shop.test" || s.Target == "" || filepath.Base(s.DocRoot) != "public" {
		t.Fatalf("%+v", s)
	}
	// Tools run in the project's real folder find the site.
	if got, ok := ForPath(found, filepath.Join(project, "app")); !ok || got.Name != "my-shop" {
		t.Fatal("ForPath did not map the real project folder to its site")
	}
	if _, ok := ForPath(found, t.TempDir()); ok {
		t.Fatal("ForPath matched an unrelated folder")
	}
	if !IsLink(www, "my-shop") {
		t.Fatal("IsLink false for a link")
	}

	if err := Unlink(www, "my-shop"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "public", "index.php")); err != nil {
		t.Fatal("unlink deleted the project's files")
	}
	if found, _, _ := Scan(www, "test"); len(found) != 0 {
		t.Fatal("site still listed after unlink")
	}
}

func TestUnlinkRefusesRealFolders(t *testing.T) {
	www := t.TempDir()
	os.MkdirAll(filepath.Join(www, "real", "src"), 0o755)
	if err := Unlink(www, "real"); err == nil || !strings.Contains(err.Error(), "real folder") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(www, "real", "src")); err != nil {
		t.Fatal("real folder was touched")
	}
}

func TestLinkRefusals(t *testing.T) {
	root := t.TempDir()
	www := filepath.Join(root, "www")
	os.MkdirAll(filepath.Join(www, "inside"), 0o755)
	project := filepath.Join(t.TempDir(), "shop")
	os.MkdirAll(project, 0o755)
	cases := map[string]func() error{
		"missing folder": func() error { _, err := Link(root, www, filepath.Join(project, "nope"), ""); return err },
		"inside www":     func() error { _, err := Link(root, www, filepath.Join(www, "inside"), ""); return err },
		"parent of ebur": func() error { _, err := Link(root, www, filepath.Dir(root), "x"); return err },
		"bad name":       func() error { _, err := Link(root, www, project, "no_good"); return err },
		"network share":  func() error { _, err := Link(root, www, `\\server\share\shop`, ""); return err },
	}
	for name, f := range cases {
		if err := f(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Link(root, www, project, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(root, www, project, ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate name: %v", err)
	}
}

func TestBrokenLinkIsReported(t *testing.T) {
	root := t.TempDir()
	www := filepath.Join(root, "www")
	project := filepath.Join(t.TempDir(), "gone")
	os.MkdirAll(project, 0o755)
	Link(root, www, project, "")
	os.Remove(project) // the project folder is moved or deleted
	found, skipped, _ := Scan(www, "test")
	if len(found) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "ebur unlink gone") {
		t.Fatalf("found %+v skipped %+v", found, skipped)
	}
}
