package shellenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/sites"
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

	env := For(root, "")
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

// PHP-04: inside a site, the site's PHP version is the one on PATH.
func TestForUsesTheSitesPHP(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	os.MkdirAll(root.RuntimeDir("php", "8.5.13"), 0o755)
	os.MkdirAll(root.RuntimeDir("php", "8.3.37"), 0o755)
	legacy := filepath.Join(root.WWW(), "legacy")
	os.MkdirAll(filepath.Join(legacy, "app", "Http"), 0o755)
	os.WriteFile(filepath.Join(legacy, ".ebur"), []byte("php: 8.3\n"), 0o644)
	os.MkdirAll(filepath.Join(root.WWW(), "modern"), 0o755)

	deep := For(root, filepath.Join(legacy, "app", "Http"))
	if deep.PHP != "8.3.37" || deep.Path[0] != root.RuntimeDir("php", "8.3.37") || !strings.Contains(deep.PHPSource, "legacy") {
		t.Fatalf("inside legacy: %+v", deep)
	}
	if !strings.HasSuffix(deep.Vars["PHPRC"], "8.3.37") {
		t.Fatalf("PHPRC %s", deep.Vars["PHPRC"])
	}
	for _, d := range []string{filepath.Join(root.WWW(), "modern"), root.WWW(), t.TempDir()} {
		if e := For(root, d); e.PHP != "8.5.13" {
			t.Errorf("%s: PHP %s, want the default", d, e.PHP)
		}
	}
}

// A linked project's real folder gets the site's PHP too (`ebur link`).
func TestForLinkedProject(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	os.MkdirAll(root.RuntimeDir("php", "8.5.13"), 0o755)
	os.MkdirAll(root.RuntimeDir("php", "8.3.37"), 0o755)
	project := filepath.Join(t.TempDir(), "client-site")
	os.MkdirAll(filepath.Join(project, "src"), 0o755)
	os.WriteFile(filepath.Join(project, ".ebur"), []byte("php: 8.3\n"), 0o644)
	if _, err := sites.Link(root.Dir(), root.WWW(), project, ""); err != nil {
		t.Fatal(err)
	}
	if e := For(root, filepath.Join(project, "src")); e.PHP != "8.3.37" || !strings.Contains(e.PHPSource, "client-site") {
		t.Fatalf("in the real project folder: %+v", e)
	}
}

// PROJ-03: Node is on PATH, and inside a site it is the site's version.
func TestForNode(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	os.MkdirAll(root.RuntimeDir("php", "8.5.13"), 0o755)
	os.MkdirAll(root.RuntimeDir("node", "24.22.3"), 0o755)
	os.MkdirAll(root.RuntimeDir("node", "22.23.5"), 0o755)
	cfg := config.Defaults()
	cfg.Active["node"] = "24.22.3"
	config.Save(root.Config(), cfg)
	old := filepath.Join(root.WWW(), "old")
	os.MkdirAll(filepath.Join(old, "resources"), 0o755)
	os.WriteFile(filepath.Join(old, ".nvmrc"), []byte("22\n"), 0o644)

	if e := For(root, ""); e.Node != "24.22.3" || !strings.Contains(strings.Join(e.Path, ";"), root.RuntimeDir("node", "24.22.3")) || e.Summary() != "php, node" {
		t.Fatalf("default: %+v (%s)", e, e.Summary())
	}
	e := For(root, filepath.Join(old, "resources"))
	if e.Node != "22.23.5" || !strings.Contains(e.NodeSource, "old (.nvmrc)") || !strings.Contains(strings.Join(e.Path, ";"), root.RuntimeDir("node", "22.23.5")) {
		t.Fatalf("inside old: %+v", e)
	}
	if e.PHP != "8.5.13" {
		t.Fatalf("PHP changed: %s", e.PHP)
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
	env := For(root, "")
	if len(env.Path) != 0 || env.Vars["PHPRC"] != "" {
		t.Fatalf("%+v", env)
	}
}
