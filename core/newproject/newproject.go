// Package newproject creates a new site in www\ from a starter: Laravel,
// Symfony, WordPress or a blank folder (PROJ-01), with its own database
// filled into .env / wp-config.php (PROJ-02).
//
// The project is built in www\.ebur-new-<name>, which the site scan
// ignores, and moved into place only when it is complete, so a half-made
// site is never served and a failure leaves nothing behind.
package newproject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ebur-dev/ebur/core/certs"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/shellenv"
	"github.com/ebur-dev/ebur/core/sites"
	"github.com/ebur-dev/ebur/internal/fsutil"
	"github.com/ebur-dev/ebur/internal/proc"
)

// Kind is a project starter.
type Kind string

const (
	Laravel   Kind = "laravel"
	WordPress Kind = "wordpress"
	Symfony   Kind = "symfony"
	Blank     Kind = "blank"
)

// Kinds lists the starters in the order menus show them.
var Kinds = []Kind{Laravel, WordPress, Symfony, Blank}

// Label is the starter's display name.
func (k Kind) Label() string {
	switch k {
	case Laravel:
		return "Laravel"
	case WordPress:
		return "WordPress"
	case Symfony:
		return "Symfony"
	case Blank:
		return "Blank"
	}
	return string(k)
}

func (k Kind) usesComposer() bool { return k == Laravel || k == Symfony }
func (k Kind) usesDatabase() bool { return k != Blank }

// Options describe the project to create.
type Options struct {
	Name string
	Kind Kind
	PHP  string // optional: pin this PHP version in the site's .ebur file
}

// Result describes the created project.
type Result struct {
	Name     string   `json:"name"`
	Dir      string   `json:"dir"`
	URL      string   `json:"url"`
	Database string   `json:"database"` // "" when none was created
	Next     []string `json:"next"`     // what to do next, one step per line
}

// Create makes the project. say reports each step; out receives the
// output of the tools it runs (Composer), and may be nil.
func Create(ctx context.Context, root paths.Root, o Options, say func(string), out io.Writer) (Result, error) {
	if say == nil {
		say = func(string) {}
	}
	if out == nil {
		out = io.Discard
	}
	cfg, err := config.Load(root.Config())
	if err != nil {
		return Result{}, err
	}
	name := strings.ToLower(strings.TrimSpace(o.Name))
	if err := checkName(root, name); err != nil {
		return Result{}, err
	}
	if !known(o.Kind) {
		return Result{}, fmt.Errorf("unknown project type %q; choose laravel, wordpress, symfony or blank", o.Kind)
	}

	installed, _ := runtimes.Installed(root, "php")
	if len(installed) == 0 {
		return Result{}, errors.New("PHP is not installed; run `ebur php install 8.4` first")
	}
	php := ""
	if o.PHP != "" {
		for _, v := range installed {
			if v == o.PHP || strings.HasPrefix(v, o.PHP+".") {
				php = v
				break
			}
		}
		if php == "" {
			return Result{}, fmt.Errorf("PHP %s is not installed; run `ebur php install %s` or leave out --php", o.PHP, o.PHP)
		}
	}
	env := shellenv.ForPHP(root, php)
	composer := ""
	if o.Kind.usesComposer() {
		v := activeOf(root, cfg, "composer")
		if v == "" {
			return Result{}, errors.New("Composer is not installed; run `ebur install composer` first")
		}
		composer = filepath.Join(root.RuntimeDir("composer", v), "composer.phar")
	}
	var db *database
	if o.Kind.usesDatabase() {
		if db, err = openDatabase(root, cfg); err != nil {
			return Result{}, err
		}
		if err := db.ping(ctx); err != nil {
			return Result{}, err
		}
	}

	res := Result{Name: name, Dir: filepath.Join(root.WWW(), name), URL: siteURL(root, cfg, name)}
	tmp := filepath.Join(root.WWW(), ".ebur-new-"+name)
	if err := os.RemoveAll(tmp); err != nil { // left over from an interrupted run
		return Result{}, fmt.Errorf("remove the unfinished www\\.ebur-new-%s: %w", name, err)
	}
	createdDB := false
	fail := func(err error) (Result, error) {
		// Antivirus or a still-exiting PHP can hold a file for a moment.
		if rmErr := fsutil.Retry(func() error { return os.RemoveAll(tmp) }); rmErr != nil {
			err = fmt.Errorf("%w (some files in www\\.ebur-new-%s could not be removed yet; it is not served, and the next `ebur new %s` clears it)", err, name, name)
		}
		if createdDB {
			db.drop(context.Background(), res.Database) // only the one made just now, still empty of user data
		}
		return Result{}, err
	}

	if db != nil {
		res.Database = strings.ReplaceAll(name, "-", "_")
		say("Creating database " + res.Database)
		if err := db.create(ctx, res.Database); err != nil {
			return fail(err)
		}
		createdDB = true
	}

	r := runner{root: root, cfg: cfg, env: env, composer: composer, db: db, out: out, say: say}
	switch o.Kind {
	case Laravel:
		err = r.laravel(ctx, tmp, res)
	case Symfony:
		err = r.symfony(ctx, tmp, res)
	case WordPress:
		err = r.wordpress(ctx, tmp, res)
	case Blank:
		err = blank(tmp)
	}
	if err != nil {
		if ctx.Err() != nil {
			err = errors.New("cancelled; nothing was kept")
		}
		return fail(err)
	}
	if o.PHP != "" {
		if err := os.WriteFile(filepath.Join(tmp, ".ebur"), []byte("php: "+o.PHP+"\n"), 0o644); err != nil {
			return fail(err)
		}
	}
	if _, err := os.Lstat(res.Dir); err == nil { // made while we worked
		return fail(fmt.Errorf("www\\%s appeared while the project was being created; nothing was changed", name))
	}
	if err := fsutil.Rename(tmp, res.Dir); err != nil {
		return fail(fmt.Errorf("move the finished project into www\\%s: %w", name, err))
	}
	res.Next = nextSteps(o.Kind, res)
	return res, nil
}

func known(k Kind) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

func checkName(root paths.Root, name string) error {
	if !sites.ValidName(name) {
		return fmt.Errorf("%q cannot be a site name: use lowercase letters, digits and hyphens, e.g. shop or my-shop", name)
	}
	if tool, ok := sites.Reserved[name]; ok {
		return fmt.Errorf("%s is reserved for %s; choose another name", name, tool)
	}
	if _, err := os.Lstat(filepath.Join(root.WWW(), name)); err == nil {
		return fmt.Errorf("www\\%s already exists; choose another name", name)
	}
	return nil
}

func activeOf(root paths.Root, cfg config.Settings, rt string) string {
	if v := cfg.Active[rt]; v != "" {
		return v
	}
	if inst, _ := runtimes.Installed(root, rt); len(inst) > 0 {
		return inst[0]
	}
	return ""
}

func siteURL(root paths.Root, cfg config.Settings, name string) string {
	scheme := "http"
	if certs.Dir(root.SSL()).Exists() {
		scheme = "https"
	}
	return scheme + "://" + name + "." + cfg.TLD
}

func mailInstalled(root paths.Root) bool {
	v, _ := runtimes.Installed(root, "mailpit")
	return len(v) > 0
}

type runner struct {
	root     paths.Root
	cfg      config.Settings
	env      shellenv.Env
	composer string
	db       *database
	out      io.Writer
	say      func(string)
}

// php runs Ebur's PHP with args (e.g. composer.phar create-project …),
// streaming its output, with extra environment variables.
func (r runner) php(ctx context.Context, dir string, extra map[string]string, args ...string) error {
	exe := filepath.Join(r.root.RuntimeDir("php", r.env.PHP), proc.Exe("php"))
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	environ := r.env.Environ(os.Environ())
	environ = append(environ, "COMPOSER_NO_INTERACTION=1")
	for k, v := range extra {
		environ = append(environ, k+"="+v)
	}
	cmd.Env = environ
	cmd.Stdout, cmd.Stderr = r.out, r.out
	proc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func (r runner) createProject(ctx context.Context, pkg, tmp string, extra map[string]string) error {
	r.say(fmt.Sprintf("Downloading %s with Composer (PHP %s); this takes a minute or two", pkg, r.env.PHP))
	err := r.php(ctx, filepath.Dir(tmp), extra, r.composer, "create-project", pkg, filepath.Base(tmp), "--no-interaction", "--prefer-dist")
	if err != nil {
		return fmt.Errorf("composer create-project %s failed (%v); its output above says why", pkg, err)
	}
	return nil
}

func (r runner) laravel(ctx context.Context, tmp string, res Result) error {
	vars := map[string]string{
		"DB_CONNECTION": "mysql",
		"DB_HOST":       "127.0.0.1",
		"DB_PORT":       fmt.Sprint(r.cfg.DBPort),
		"DB_DATABASE":   res.Database,
		"DB_USERNAME":   "root",
		"DB_PASSWORD":   r.cfg.DBRootPassword,
	}
	// Laravel's own setup scripts run `artisan migrate`; real environment
	// variables beat .env, so they migrate into the new MariaDB database
	// instead of creating a SQLite one.
	if err := r.createProject(ctx, "laravel/laravel", tmp, vars); err != nil {
		return err
	}
	r.say("Writing database and mail settings to .env")
	vars["APP_URL"] = res.URL
	if strings.HasPrefix(res.URL, "https://") {
		// laravel-vite-plugin serves `npm run dev` over HTTPS with these,
		// so an https:// page can load Vite's scripts (PROJ-03). Ebur
		// writes the site's certificate when it picks up the new folder.
		host := res.Name + "." + r.cfg.TLD
		d := certs.Dir(r.root.SSL())
		vars["VITE_DEV_SERVER_KEY"] = filepath.ToSlash(d.SiteKey(host))
		vars["VITE_DEV_SERVER_CERT"] = filepath.ToSlash(d.SiteCert(host))
	}
	if mailInstalled(r.root) {
		vars["MAIL_MAILER"] = "smtp"
		vars["MAIL_HOST"] = "127.0.0.1"
		vars["MAIL_PORT"] = fmt.Sprint(provision.MailSMTPPort)
	}
	if err := SetEnv(filepath.Join(tmp, ".env"), vars); err != nil {
		return err
	}
	// The setup script touches an empty SQLite file we do not use.
	sqlite := filepath.Join(tmp, "database", "database.sqlite")
	if fi, err := os.Stat(sqlite); err == nil && fi.Size() == 0 {
		os.Remove(sqlite)
	}
	return nil
}

func (r runner) symfony(ctx context.Context, tmp string, res Result) error {
	if err := r.createProject(ctx, "symfony/skeleton", tmp, nil); err != nil {
		return err
	}
	r.say("Writing database and mail settings to .env.local")
	mariadb := activeOf(r.root, r.cfg, "mariadb")
	vars := map[string]string{
		"DATABASE_URL": fmt.Sprintf("mysql://root:%s@127.0.0.1:%d/%s?serverVersion=%s-MariaDB&charset=utf8mb4",
			urlEscape(r.cfg.DBRootPassword), r.cfg.DBPort, res.Database, mariadb),
		"DEFAULT_URI": res.URL,
	}
	if mailInstalled(r.root) {
		vars["MAILER_DSN"] = fmt.Sprintf("smtp://127.0.0.1:%d", provision.MailSMTPPort)
	}
	// .env.local holds this computer's settings; Symfony's .gitignore keeps it out of git.
	return SetEnv(filepath.Join(tmp, ".env.local"), vars)
}

func blank(tmp string) error {
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	page := "<?php\n// A new Ebur site. Replace this file with your project.\n" +
		"echo '<h1>', htmlspecialchars(basename(__DIR__)), '</h1><p>PHP ', PHP_VERSION, ' is working.</p>';\n"
	return os.WriteFile(filepath.Join(tmp, "index.php"), []byte(page), 0o644)
}

func nextSteps(k Kind, res Result) []string {
	var next []string
	switch k {
	case WordPress:
		next = append(next, "Open "+res.URL+" to finish the WordPress install (site title and admin user). The database is already set in wp-config.php.")
	case Laravel:
		next = append(next, "Open "+res.URL+". The database "+res.Database+" is set in .env and already migrated.",
			"Run artisan inside the folder with `ebur exec php artisan …`, or open Ebur's terminal there.",
			"Front end: `npm install` then `npm run dev` in Ebur's terminal (needs Node: `ebur node install 24`).")
	case Symfony:
		next = append(next, "Open "+res.URL+". The database "+res.Database+" is set in .env.local.",
			"Add packages with `ebur exec composer require …` inside the folder (e.g. webapp for a full web app).")
	case Blank:
		next = append(next, "Open "+res.URL+", then replace index.php with your project.")
	}
	return append(next, "Files: "+res.Dir)
}

// envLine matches KEY=… in a .env file, possibly commented out ("# KEY=").
func envLine(key string) *regexp.Regexp {
	return regexp.MustCompile(`^(\s*#\s*)?` + regexp.QuoteMeta(key) + `\s*=`)
}

// SetEnv sets keys in a .env-style file, creating it if needed. An
// existing KEY= line is replaced; otherwise a commented "# KEY=" line is
// (Laravel ships DB_HOST and friends commented out); otherwise the key
// is appended. Other lines and comments are kept as they are.
func SetEnv(path string, vars map[string]string) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	nl := "\n"
	if bytes.Contains(b, []byte("\r\n")) {
		nl = "\r\n"
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		line := k + "=" + envQuote(vars[k])
		re := envLine(k)
		set, commented := -1, -1
		for i, l := range lines {
			if m := re.FindStringSubmatch(l); m != nil {
				if m[1] == "" {
					set = i
					break
				}
				if commented < 0 {
					commented = i
				}
			}
		}
		switch {
		case set >= 0:
			lines[set] = line
		case commented >= 0:
			lines[commented] = line
		default:
			lines = append(lines, line)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, nl)+nl), 0o644)
}

var plainValue = regexp.MustCompile(`^[A-Za-z0-9_.:/@?&=%+-]*$`)

func envQuote(v string) string {
	if plainValue.MatchString(v) {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`).Replace(v) + `"`
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
