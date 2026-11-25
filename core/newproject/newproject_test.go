package newproject

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/paths"
)

func testRoot(t *testing.T, runtimes ...string) paths.Root {
	t.Helper()
	root, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, rv := range runtimes {
		rt, v, _ := strings.Cut(rv, "@")
		if err := os.MkdirAll(root.RuntimeDir(rt, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBlank(t *testing.T) {
	root := testRoot(t, "php@8.4.1", "php@8.3.2")
	var steps []string
	res, err := Create(context.Background(), root, Options{Name: "Demo", Kind: Blank, PHP: "8.3"}, func(s string) { steps = append(steps, s) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "demo" || res.Database != "" || !strings.HasSuffix(res.URL, "://demo.test") {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root.WWW(), "demo", "index.php")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root.WWW(), "demo", ".ebur")); string(b) != "php: 8.3\n" {
		t.Fatalf(".ebur = %q", b)
	}
	if _, err := os.Stat(filepath.Join(root.WWW(), ".ebur-new-demo")); !os.IsNotExist(err) {
		t.Fatal("work folder left behind")
	}
	// Same name again is refused and the existing site is untouched.
	if _, err := Create(context.Background(), root, Options{Name: "demo", Kind: Blank}, nil, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second create: %v", err)
	}
}

// The Node hint appears only when Node is missing.
func TestNextStepsNodeHint(t *testing.T) {
	res := Result{URL: "https://shop.test", Database: "shop", Dir: `C:\ebur\www\shop`}
	with := strings.Join(nextSteps(Laravel, res, true), "\n")
	without := strings.Join(nextSteps(Laravel, res, false), "\n")
	if strings.Contains(with, "ebur node install") || !strings.Contains(with, "npm run dev") {
		t.Errorf("with Node:\n%s", with)
	}
	if !strings.Contains(without, "ebur node install 24") {
		t.Errorf("without Node:\n%s", without)
	}
}

func TestRefusals(t *testing.T) {
	root := testRoot(t, "php@8.4.1")
	cases := []struct {
		o    Options
		want string
	}{
		{Options{Name: "my_shop", Kind: Blank}, "cannot be a site name"},
		{Options{Name: "-x", Kind: Blank}, "cannot be a site name"},
		{Options{Name: "db", Kind: Blank}, "reserved for Adminer"},
		{Options{Name: "mail", Kind: Laravel}, "reserved for Mailpit"},
		{Options{Name: "ok", Kind: "drupal"}, "unknown project type"},
		{Options{Name: "ok", Kind: Blank, PHP: "7.4"}, "PHP 7.4 is not installed"},
		{Options{Name: "ok", Kind: Laravel}, "Composer is not installed"},
		{Options{Name: "ok", Kind: WordPress}, "no database server is installed"},
		{Options{Name: "ok", Kind: WordPress, DB: "postgres"}, "unknown database server"},
	}
	for _, c := range cases {
		_, err := Create(context.Background(), root, c.o, nil, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.o, err, c.want)
		}
	}
	empty := testRoot(t)
	if _, err := Create(context.Background(), empty, Options{Name: "x", Kind: Blank}, nil, nil); err == nil || !strings.Contains(err.Error(), "PHP is not installed") {
		t.Errorf("no PHP: %v", err)
	}
	entries, _ := os.ReadDir(root.WWW())
	if len(entries) != 0 {
		t.Errorf("refusals left files in www: %v", entries)
	}
}

func TestSetEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	laravel := "APP_NAME=Laravel\r\nDB_CONNECTION=sqlite\r\n# DB_HOST=127.0.0.1\r\n# DB_PORT=3306\r\n# DB_PASSWORD=\r\nMAIL_MAILER=log\r\n"
	os.WriteFile(path, []byte(laravel), 0o644)
	err := SetEnv(path, map[string]string{
		"DB_CONNECTION": "mysql", "DB_HOST": "127.0.0.1", "DB_PORT": "3307",
		"DB_PASSWORD": `pa ss"$x`, "APP_URL": "https://shop.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := "APP_NAME=Laravel\r\nDB_CONNECTION=mysql\r\nDB_HOST=127.0.0.1\r\nDB_PORT=3307\r\nDB_PASSWORD=\"pa ss\\\"\\$x\"\r\nMAIL_MAILER=log\r\nAPP_URL=https://shop.test\r\n"
	if string(b) != want {
		t.Fatalf("got\n%q\nwant\n%q", b, want)
	}

	fresh := filepath.Join(t.TempDir(), ".env.local")
	if err := SetEnv(fresh, map[string]string{"B": "2", "A": "1"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(fresh); string(b) != "A=1\nB=2\n" {
		t.Fatalf("new file = %q", b)
	}
}

const wpSample = `<?php
define( 'DB_NAME', 'database_name_here' );
define( 'DB_USER', 'username_here' );
define( 'DB_PASSWORD', 'password_here' );
define( 'DB_HOST', 'localhost' );
define( 'AUTH_KEY',         'put your unique phrase here' );
define( 'SECURE_AUTH_KEY',  'put your unique phrase here' );
define( 'LOGGED_IN_KEY',    'put your unique phrase here' );
define( 'NONCE_KEY',        'put your unique phrase here' );
define( 'AUTH_SALT',        'put your unique phrase here' );
define( 'SECURE_AUTH_SALT', 'put your unique phrase here' );
define( 'LOGGED_IN_SALT',   'put your unique phrase here' );
define( 'NONCE_SALT',       'put your unique phrase here' );
/* That's all, stop editing! Happy publishing. */
`

func TestWPConfig(t *testing.T) {
	out, err := wpConfig(wpSample, "blog", "pw'1", "127.0.0.1:3307")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"'DB_NAME', 'blog'", "'DB_USER', 'root'", `'DB_PASSWORD', 'pw\'1'`, "'DB_HOST', '127.0.0.1:3307'", "define( 'WP_ENVIRONMENT_TYPE', 'local' );\n\n/* That's all"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "put your unique phrase here") {
		t.Error("a secret key was not filled")
	}
	keys := map[string]bool{}
	keyLine := regexp.MustCompile(`^define\( '\w+_(KEY|SALT)',\s+'([^'\\]*)' \);$`)
	for _, line := range strings.Split(out, "\n") {
		if m := keyLine.FindStringSubmatch(line); m != nil {
			if v := m[2]; len(v) != 64 || keys[v] {
				t.Errorf("bad or repeated key %q", v)
			} else {
				keys[v] = true
			}
		}
	}
	if len(keys) != 8 {
		t.Errorf("%d keys", len(keys))
	}
	if _, err := wpConfig("<?php // changed layout", "blog", "pw", "h"); err == nil {
		t.Error("an unknown layout should be refused")
	}
}
