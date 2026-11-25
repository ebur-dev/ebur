package doctor

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
)

func find(cs []Check, name string) Check {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return Check{}
}

func TestEmptyInstall(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	hosts := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(hosts, []byte("# empty\n"), 0o644)
	cs := Run(root, Options{HostsFile: hosts})
	if c := find(cs, "Runtime php"); c.Level != Fail || !strings.Contains(c.Hint, "ebur php install") {
		t.Errorf("php: %+v", c)
	}
	if c := find(cs, "Runtime mariadb"); c.Level != Info {
		t.Errorf("optional runtime should be info: %+v", c)
	}
	if c := find(cs, "Ebur folder"); c.Level != OK {
		t.Errorf("%+v", c)
	}
	if c := find(cs, "Ebur host"); c.Level != Info {
		t.Errorf("%+v", c)
	}
	for _, c := range cs {
		if (c.Level == Fail || c.Level == Warn) && c.Hint == "" {
			t.Errorf("finding without a next step (NFR-14): %+v", c)
		}
	}
}

func TestRuntimeAndIni(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	phpDir := root.RuntimeDir("php", "8.4.28")
	os.MkdirAll(filepath.Join(phpDir, "ext"), 0o755)
	os.WriteFile(filepath.Join(phpDir, "ext", "php_curl.dll"), nil, 0o644)
	ini := filepath.Join(root.Etc(), "php", "8.4.28", "php.ini")
	os.MkdirAll(filepath.Dir(ini), 0o755)
	os.WriteFile(ini, []byte("extension = curl\nextension=\"gd\"\n;extension = intl\n"), 0o644)

	cs := Run(root, Options{HostsFile: filepath.Join(t.TempDir(), "none")})
	if c := find(cs, "Runtime php"); c.Level != OK || !strings.Contains(c.Detail, "8.4.28") {
		t.Errorf("%+v", c)
	}
	if c := find(cs, "php.ini"); c.Level != Warn || !strings.Contains(c.Detail, "gd") || strings.Contains(c.Detail, "intl") || strings.Contains(c.Detail, "curl") {
		t.Errorf("%+v", c)
	}

	cfg := config.Defaults()
	cfg.Active["php"] = "8.3.0"
	config.Save(root.Config(), cfg)
	if c := find(Run(root, Options{}), "Runtime php"); c.Level != Fail || !strings.Contains(c.Hint, "8.4.28") {
		t.Errorf("missing active version: %+v", c)
	}
}

func TestSitePHPProblem(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	os.MkdirAll(filepath.Join(root.RuntimeDir("php", "8.5.13"), "ext"), 0o755)
	legacy := filepath.Join(root.WWW(), "legacy")
	os.MkdirAll(legacy, 0o755)
	os.WriteFile(filepath.Join(legacy, "composer.json"), []byte(`{"require":{"php":"^5.6"}}`), 0o644)
	c := find(Run(root, Options{HostsFile: filepath.Join(t.TempDir(), "none")}), "Site legacy PHP")
	if c.Level != Warn || !strings.Contains(c.Detail, "^5.6") || !strings.Contains(c.Hint, "composer.json") {
		t.Fatalf("%+v", c)
	}
}

// SEC-01: a site on end-of-life PHP is flagged; an unused one is noted.
func TestLegacyPHP(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	for _, v := range []string{"8.5.13", "8.0.30", "7.4.33"} {
		os.MkdirAll(filepath.Join(root.RuntimeDir("php", v), "ext"), 0o755)
	}
	cfg := config.Defaults()
	cfg.Active["php"] = "8.5.13"
	config.Save(root.Config(), cfg)
	old := filepath.Join(root.WWW(), "oldshop")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, ".ebur"), []byte("php: 7.4\n"), 0o644)

	cs := Run(root, Options{HostsFile: filepath.Join(t.TempDir(), "none")})
	if c := find(cs, "PHP 7.4.33"); c.Level != Warn || !strings.Contains(c.Detail, "oldshop") || !strings.Contains(c.Detail, "2022-11-28") {
		t.Errorf("used legacy PHP: %+v", c)
	}
	if c := find(cs, "PHP 8.0.30"); c.Level != Info || !strings.Contains(c.Hint, "ebur php remove 8.0.30") {
		t.Errorf("unused legacy PHP: %+v", c)
	}
	if c := find(cs, "PHP 8.5.13"); c.Name != "" {
		t.Errorf("supported PHP flagged: %+v", c)
	}
}

func TestHosts(t *testing.T) {
	hosts := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(hosts, []byte("127.0.0.1 shop.test # ebur\n10.0.0.1 blog.test\n::1 api.test\n"), 0o644)
	c := checkHosts(hosts, []string{"shop.test", "blog.test", "api.test"})
	if c.Level != Warn || !strings.Contains(c.Detail, "blog.test") || strings.Contains(c.Detail, "shop.test") || c.Hint == "" {
		t.Errorf("%+v", c)
	}
	if c := checkHosts(hosts, []string{"shop.test"}); c.Level != OK {
		t.Errorf("%+v", c)
	}
	if c := checkHosts(hosts, nil); c.Level != OK {
		t.Errorf("%+v", c)
	}
}

func TestPortConflictNamesOwner(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	root, _ := paths.New(t.TempDir())
	c := checkPort(root, ln.Addr().(*net.TCPAddr).Port, false)
	if c.Level != Fail || c.Hint == "" {
		t.Errorf("%+v", c)
	}
	// The test binary lives outside root, so it is not "ours"; a root
	// containing it would be.
	exe, _ := os.Executable()
	ours, _ := paths.New(filepath.Dir(exe))
	if c := checkPort(ours, ln.Addr().(*net.TCPAddr).Port, true); c.Level != OK {
		t.Errorf("own process not recognised: %+v", c)
	}
}
