package shellenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
)

func TestFor(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	for _, d := range []string{
		root.RuntimeDir("php", "8.4.28"), root.RuntimeDir("php", "8.5.13"),
		root.RuntimeDir("composer", "2.11.3"), filepath.Join(root.RuntimeDir("mariadb", "12.3.4"), "bin"),
	} {
		os.MkdirAll(d, 0o755)
	}
	cfg := config.Defaults()
	cfg.Active["php"] = "8.4.28" // not the newest: the active one must win
	config.Save(root.Config(), cfg)

	env := For(root)
	if len(env.Path) != 3 || env.Path[0] != root.RuntimeDir("php", "8.4.28") {
		t.Fatalf("path: %v", env.Path)
	}
	if env.Vars["PHPRC"] != filepath.Join(root.Etc(), "php", "8.4.28") || !strings.HasSuffix(env.Vars["PHP_INI_SCAN_DIR"], "conf.d") {
		t.Fatalf("vars: %v", env.Vars)
	}
	if env.Vars[paths.EnvRoot] != root.Dir() {
		t.Fatal("EBUR_ROOT not set")
	}
	if s := env.Summary(); s != "php, composer, mariadb" {
		t.Fatalf("summary %q", s)
	}
}

func TestEnviron(t *testing.T) {
	e := Env{Path: []string{`C:\e\php`}, Vars: map[string]string{"PHPRC": `C:\e\etc`, "NEW": "1"}}
	sep := string(os.PathListSeparator)
	out := e.Environ([]string{"Path=C:\\Windows", "phprc=old", "HOME=x"})
	joined := strings.Join(out, "\n")
	for _, want := range []string{"Path=C:\\e\\php" + sep + "C:\\Windows", "PHPRC=C:\\e\\etc", "HOME=x", "NEW=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "phprc=old") {
		t.Error("old PHPRC kept (Windows names are case-insensitive)")
	}
	if got := (Env{Path: []string{"a"}}).Environ(nil); len(got) != 1 || got[0] != "PATH=a" {
		t.Errorf("no base PATH: %v", got)
	}
}

func TestEmptyRoot(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	env := For(root)
	if len(env.Path) != 0 || env.Vars["PHPRC"] != "" {
		t.Fatalf("%+v", env)
	}
}
