package runtimes

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func setup(t *testing.T, body []byte) (*Installer, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	root, _ := paths.New(t.TempDir())
	return &Installer{Root: root, Client: srv.Client()}, srv.URL
}

func TestInstallVerifiesAndExtracts(t *testing.T) {
	z := makeZip(t, map[string]string{"php-cgi.exe": "bin", "ext/php_curl.dll": "dll"})
	in, url := setup(t, z)
	var last int64
	dir, err := in.Install(context.Background(), "php", manifest.Release{Version: "8.4.28", URL: url + "/php.zip", SHA256: sum(z)},
		func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ext", "php_curl.dll")); string(b) != "dll" {
		t.Fatal("file not extracted")
	}
	if last != int64(len(z)) {
		t.Fatalf("progress ended at %d of %d", last, len(z))
	}
	if v, _ := Installed(in.Root, "php"); len(v) != 1 || v[0] != "8.4.28" {
		t.Fatalf("Installed = %v", v)
	}
	if left, _ := os.ReadDir(in.Root.Downloads()); len(left) != 0 {
		t.Fatalf("download not cleaned up: %v", left)
	}
	// Second install is a no-op even with a bad URL.
	if _, err := in.Install(context.Background(), "php", manifest.Release{Version: "8.4.28", URL: url + "/missing.zip"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestChecksumMismatchInstallsNothing(t *testing.T) {
	z := makeZip(t, map[string]string{"a.txt": "a"})
	in, url := setup(t, z)
	_, err := in.Install(context.Background(), "php", manifest.Release{Version: "8.4.1", URL: url + "/php.zip", SHA256: strings.Repeat("0", 64)}, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
	if v, _ := Installed(in.Root, "php"); len(v) != 0 {
		t.Fatalf("something was installed: %v", v)
	}
	if _, err := os.Stat(in.Root.RuntimeDir("php", "8.4.1")); err == nil {
		t.Fatal("install folder exists")
	}
}

func TestHTTPError(t *testing.T) {
	in, url := setup(t, nil)
	_, err := in.Install(context.Background(), "php", manifest.Release{Version: "8.4.1", URL: url + "/missing.zip", SHA256: strings.Repeat("0", 64)}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want 404 error, got %v", err)
	}
}

func TestRootStripped(t *testing.T) {
	z := makeZip(t, map[string]string{"nginx-1.30.8/nginx.exe": "x", "nginx-1.30.8/conf/nginx.conf": "c", "other/skip.txt": "s"})
	in, url := setup(t, z)
	dir, err := in.Install(context.Background(), "nginx", manifest.Release{Version: "1.30.8", URL: url + "/n.zip", SHA256: sum(z), Root: "nginx-1.30.8"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "conf", "nginx.conf")); err != nil {
		t.Fatal("root not stripped")
	}
	if _, err := os.Stat(filepath.Join(dir, "other")); err == nil {
		t.Fatal("entry outside root extracted")
	}
}

func TestSingleFile(t *testing.T) {
	body := []byte("<?php // adminer")
	in, url := setup(t, body)
	dir, err := in.Install(context.Background(), "adminer", manifest.Release{Version: "6.3.1", URL: url + "/a.php", SHA256: sum(body), File: "adminer.php"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "adminer.php")); string(b) != string(body) {
		t.Fatal("file not placed")
	}
}

func TestRootMissing(t *testing.T) {
	z := makeZip(t, map[string]string{"x/a": "a"})
	in, url := setup(t, z)
	if _, err := in.Install(context.Background(), "nginx", manifest.Release{Version: "1.0", URL: url + "/n.zip", SHA256: sum(z), Root: "nginx-9"}, nil); err == nil {
		t.Fatal("missing root accepted")
	}
}

func TestZipSlipRejected(t *testing.T) {
	for _, evil := range []string{"../evil.txt", "a/../../evil.txt", "C:/evil.txt"} {
		z := makeZip(t, map[string]string{"ok.txt": "ok", evil: "pwned"})
		in, url := setup(t, z)
		_, err := in.Install(context.Background(), "php", manifest.Release{Version: "8.4.1", URL: url + "/p.zip", SHA256: sum(z)}, nil)
		if err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Fatalf("%s: want unsafe path error, got %v", evil, err)
		}
		if _, err := os.Stat(filepath.Join(in.Root.Bin(), "evil.txt")); err == nil {
			t.Fatal("file escaped")
		}
		if v, _ := Installed(in.Root, "php"); len(v) != 0 {
			t.Fatal("partial install left behind")
		}
	}
}

func TestRemove(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	os.MkdirAll(filepath.Join(root.RuntimeDir("php", "8.3.37"), "ext"), 0o755)
	os.MkdirAll(root.RuntimeDir("php", "8.4.28"), 0o755)

	busy := func(rt, v string) bool { return v == "8.4.28" }
	if err := Remove(root, "php", "8.4.28", busy); !errors.Is(err, ErrInUse) {
		t.Fatalf("want ErrInUse, got %v", err)
	}
	if err := Remove(root, "php", "8.3.37", busy); err != nil {
		t.Fatal(err)
	}
	if v, _ := Installed(root, "php"); len(v) != 1 || v[0] != "8.4.28" {
		t.Fatalf("Installed = %v", v)
	}
	if err := Remove(root, "php", "7.4.0", nil); err == nil {
		t.Fatal("removing missing version succeeded")
	}
	if err := Remove(root, "php", "../../x", nil); err == nil {
		t.Fatal("path traversal in version accepted")
	}
	if err := Remove(root, "../php", "8.4.28", nil); err == nil {
		t.Fatal("path traversal in runtime name accepted")
	}
	in := &Installer{Root: root}
	if _, err := in.Install(context.Background(), `..\x`, manifest.Release{Version: "1.0"}, nil); err == nil {
		t.Fatal("bad runtime name accepted by Install")
	}
}

func TestInstalledOrderAndHidden(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	for _, v := range []string{"8.4.9", "8.4.28", ".8.5.0.partial", "8.3.37"} {
		os.MkdirAll(root.RuntimeDir("php", v), 0o755)
	}
	v, _ := Installed(root, "php")
	if strings.Join(v, ",") != "8.4.28,8.4.9,8.3.37" {
		t.Fatalf("got %v", v)
	}
	if v, err := Installed(root, "none"); err != nil || v != nil {
		t.Fatalf("missing runtime: %v %v", v, err)
	}
}
