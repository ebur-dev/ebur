package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/paths"
)

func TestRedisConf(t *testing.T) {
	c := redisConf(`..\..\data\redis`)
	for _, want := range []string{"bind 127.0.0.1\n", "port 6379\n", "protected-mode yes", `dir "../../data/redis"`, "save 3600 1"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in\n%s", want, c)
		}
	}
	if strings.Contains(c, ":/") {
		t.Error("absolute Windows path in redis.conf; the MSYS2 build cannot read it")
	}
}

func TestRedisSpecUsesRelativePaths(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	s, err := redisSpec(root, "8.10.3")
	if err != nil {
		t.Fatal(err)
	}
	if s.Dir != filepath.Join(root.Etc(), "redis") || len(s.Args) != 1 || s.Args[0] != "redis.conf" || s.Stop == nil {
		t.Fatalf("spec %+v", s)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, "redis.conf"))
	if err != nil || !strings.Contains(string(b), `dir "../../data/redis"`) {
		t.Fatalf("conf: %s %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root.Data(), "redis")); err != nil {
		t.Error("data folder not created")
	}
}

func TestWriteRedisIni(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	for _, v := range []string{"8.5.13", "8.6.0"} {
		os.MkdirAll(root.RuntimeDir("php", v), 0o755)
	}
	// phpredis exists for 8.5 only (as in the manifest: none for 8.6 yet).
	dll := filepath.Join(root.RuntimeDir("phpredis", "8.5"), "php_redis.dll")
	os.MkdirAll(filepath.Dir(dll), 0o755)
	os.WriteFile(dll, []byte("x"), 0o644)
	ini := func(v string) string { return filepath.Join(root.Etc(), "php", v, "conf.d", "ebur-redis.ini") }

	if err := writeRedisIni(root, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(ini("8.5.13"))
	if err != nil || !strings.Contains(string(b), `extension = "`+filepath.ToSlash(dll)+`"`) {
		t.Fatalf("8.5 ini: %s %v", b, err)
	}
	if _, err := os.Stat(ini("8.6.0")); !os.IsNotExist(err) {
		t.Error("8.6 got an extension line without a phpredis build")
	}
	// Redis removed: the line goes, so PHP does not load an unused extension.
	if err := writeRedisIni(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ini("8.5.13")); !os.IsNotExist(err) {
		t.Error("ini kept without Redis")
	}
}
