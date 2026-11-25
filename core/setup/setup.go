// Package setup is the first-run setup (SRS §4.1): pick PHP, a database
// and HTTPS, then install everything with progress. The window's wizard
// and `ebur setup` both drive it, so they behave the same (CLI-01).
package setup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ebur-dev/ebur/core/certs"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/install"
	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/ports"
	"github.com/ebur-dev/ebur/core/runtimes"
)

// DBOption is one database choice.
type DBOption struct {
	Version string `json:"version"`
	Label   string `json:"label"`
}

// Options is what the wizard offers, with sensible defaults.
type Options struct {
	NeedsSetup   bool       `json:"needsSetup"`
	Root         string     `json:"root"`
	TLD          string     `json:"tld"`
	PHP          []string   `json:"php"`
	PHPDefault   string     `json:"phpDefault"`
	DB           []DBOption `json:"db"`
	DBDefault    string     `json:"dbDefault"`
	DBPort       int        `json:"dbPort"`
	DBPortNote   string     `json:"dbPortNote"`
	Warnings     []string   `json:"warnings"`
	WebServer    string     `json:"webServer"`
	HTTPSTrusted bool       `json:"httpsTrusted"`
	HavePHP      bool       `json:"havePHP"`
}

// Choices is what the user picked. DB "" means no database.
type Choices struct {
	PHP     string `json:"php"`
	DB      string `json:"db"`
	DBPort  int    `json:"dbPort"`
	Adminer bool   `json:"adminer"`
	Mail    bool   `json:"mail"`  // Mailpit catches outgoing mail (TOOL-03)
	Node    bool   `json:"node"`  // newest Node.js LTS for front-end tools (PROJ-03)
	Redis   bool   `json:"redis"` // Redis plus the phpredis extension (SVC-05)
	HTTPS   bool   `json:"https"`
}

// NeedsSetup is true until setup has run, unless PHP was already
// installed some other way (e.g. with the CLI).
func NeedsSetup(root paths.Root) bool {
	cfg, err := config.Load(root.Config())
	if err != nil || cfg.SetupDone {
		return false
	}
	installed, _ := runtimes.Installed(root, "php")
	return len(installed) == 0
}

var ltsRe = regexp.MustCompile(`LTS until (\d{4})`)

// GetOptions reads the manifest and checks ports to propose defaults.
func GetOptions(root paths.Root) (Options, error) {
	m, err := install.Manifest()
	if err != nil {
		return Options{}, err
	}
	cfg, err := config.Load(root.Config())
	if err != nil {
		return Options{}, err
	}
	o := Options{NeedsSetup: NeedsSetup(root), Root: root.Dir(), TLD: cfg.TLD, WebServer: "Nginx", Warnings: []string{}}
	if inst, _ := runtimes.Installed(root, "php"); len(inst) > 0 {
		o.HavePHP = true
	}
	// Legacy (end-of-life) PHP versions stay installable for old projects
	// with `ebur php install`, but a new setup only offers supported ones.
	now := time.Now()
	for _, r := range m.Releases("php", "x64") {
		if !r.EndOfLife(now) {
			o.PHP = append(o.PHP, r.Version)
		}
	}
	o.PHPDefault = RecommendedPHP(o.PHP)
	for _, r := range m.Releases("mariadb", "x64") {
		label := "MariaDB " + r.Version
		if mm := ltsRe.FindStringSubmatch(r.Notes); mm != nil {
			label += ", supported until " + mm[1]
		}
		o.DB = append(o.DB, DBOption{Version: r.Version, Label: label})
	}
	if len(o.DB) > 0 {
		o.DBDefault = o.DB[0].Version
	}
	o.DBPort, o.DBPortNote = pickDBPort(cfg.DBPort)
	for _, p := range []int{80, 443} {
		var c *ports.Conflict
		if errors.As(ports.Check(p), &c) && !strings.HasPrefix(strings.ToLower(c.Owner.Path), strings.ToLower(root.Dir())) {
			o.Warnings = append(o.Warnings, c.Error()+". "+c.Hint())
		}
	}
	if ca, err := certs.Dir(root.SSL()).LoadCA(); err == nil {
		o.HTTPSTrusted, _ = ca.IsTrusted()
	}
	return o, nil
}

// RecommendedPHP prefers the newest release that has had at least one
// patch (x.y.1 or later) over a brand-new x.y.0.
func RecommendedPHP(newestFirst []string) string {
	for _, v := range newestFirst {
		if p, err := manifest.ParseVersion(v); err == nil && len(p) >= 3 && p[2] > 0 {
			return v
		}
	}
	if len(newestFirst) > 0 {
		return newestFirst[0]
	}
	return ""
}

// pickDBPort keeps the configured port if free, otherwise suggests the
// next free one, e.g. 3307 beside Laragon's MySQL on 3306.
func pickDBPort(want int) (int, string) {
	var c *ports.Conflict
	if !errors.As(ports.Check(want), &c) {
		return want, ""
	}
	for p := want + 1; p < want+20; p++ {
		if ports.Check(p) == nil {
			return p, fmt.Sprintf("Port %d is in use by %s, so Ebur's database will use %d and both can run side by side.", want, ownerName(c), p)
		}
	}
	return want, c.Error() + ". " + c.Hint()
}

func ownerName(c *ports.Conflict) string {
	if c.Owner.PID == 0 {
		return "another program"
	}
	return c.Owner.Name
}

// Step is one line of the progress list.
type Step struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  string `json:"state"` // pending, running, done, failed, skipped
	Detail string `json:"detail"`
	Done   int64  `json:"done"`  // bytes downloaded
	Total  int64  `json:"total"` // bytes to download, -1 if unknown
}

// Hooks are the steps setup cannot do itself: starting services goes
// through the host, mapping site names through the elevated helper.
type Hooks struct {
	Start func() error
	Hosts func() error
}

// Run installs the choices, calling emit with the full step list on every
// change. It stops at the first install failure; a declined HTTPS or
// hosts prompt is reported but does not fail setup.
func Run(ctx context.Context, root paths.Root, c Choices, hooks Hooks, emit func([]Step)) error {
	var mu sync.Mutex
	steps := []Step{
		{ID: "nginx", Label: "Web server (Nginx)"},
		{ID: "php", Label: "PHP " + c.PHP},
		{ID: "composer", Label: "Composer"},
	}
	if c.DB != "" {
		steps = append(steps, Step{ID: "mariadb", Label: "MariaDB " + c.DB})
		if c.Adminer {
			steps = append(steps, Step{ID: "adminer", Label: "Adminer (db." + tldOf(root) + ")"})
		}
	}
	if c.Mail {
		steps = append(steps, Step{ID: "mailpit", Label: "Mail capture (mail." + tldOf(root) + ")"})
	}
	if c.Node {
		steps = append(steps, Step{ID: "node", Label: "Node.js (npm, Vite)"})
	}
	if c.Redis {
		steps = append(steps, Step{ID: "redis", Label: "Redis (cache and queues)"})
	}
	if c.HTTPS {
		steps = append(steps, Step{ID: "https", Label: "Local HTTPS"})
	}
	if hooks.Start != nil {
		steps = append(steps, Step{ID: "start", Label: "Start services"})
	}
	if hooks.Hosts != nil {
		steps = append(steps, Step{ID: "hosts", Label: "Map site names to this computer"})
	}
	for i := range steps {
		steps[i].State, steps[i].Total = "pending", -1
	}
	update := func(id string, f func(*Step)) {
		mu.Lock()
		for i := range steps {
			if steps[i].ID == id {
				f(&steps[i])
			}
		}
		cp := append([]Step(nil), steps...)
		mu.Unlock()
		if emit != nil {
			emit(cp)
		}
	}
	setState := func(id, state, detail string) {
		update(id, func(s *Step) { s.State, s.Detail = state, detail })
	}
	update("", func(*Step) {}) // first emit: the full pending list

	cfg, err := config.Load(root.Config())
	if err != nil {
		return err
	}
	if c.DB != "" && c.DBPort > 0 {
		cfg.DBPort = c.DBPort // before MariaDB is prepared, so my.ini uses it
	}
	delete(cfg.Active, "php") // the chosen version becomes active below
	if err := config.Save(root.Config(), cfg); err != nil {
		return err
	}

	installStep := func(id, runtime, want string) error {
		setState(id, "running", "")
		res, err := install.Runtime(ctx, root, runtime, want,
			func(done, total int64) {
				update(id, func(s *Step) { s.Done, s.Total, s.Detail = done, total, "Downloading" })
			},
			func(msg string) { setState(id, "running", msg) })
		if err != nil {
			setState(id, "failed", err.Error())
			return fmt.Errorf("%s: %w", runtime, err)
		}
		detail := res.Version + " ready"
		if !res.Downloaded {
			detail = res.Version + " already installed"
		}
		if len(res.Report.Warnings) > 0 {
			detail += "; " + strings.Join(res.Report.Warnings, "; ")
		}
		setState(id, "done", detail)
		return nil
	}

	if err := installStep("nginx", "nginx", ""); err != nil {
		return err
	}
	if err := installStep("php", "php", c.PHP); err != nil {
		return err
	}
	if err := installStep("composer", "composer", ""); err != nil {
		return err
	}
	// Make the chosen PHP the active one even if others were installed.
	if cfg, err = config.Load(root.Config()); err == nil {
		if inst, _ := runtimes.Installed(root, "php"); len(inst) > 0 {
			for _, v := range inst {
				if v == c.PHP || strings.HasPrefix(v, c.PHP+".") {
					cfg.Active["php"] = v
					break
				}
			}
		}
		config.Save(root.Config(), cfg)
	}
	if c.DB != "" {
		if err := installStep("mariadb", "mariadb", c.DB); err != nil {
			return err
		}
		if c.Adminer {
			if err := installStep("adminer", "adminer", ""); err != nil {
				return err
			}
		}
	}
	if c.Mail {
		if err := installStep("mailpit", "mailpit", ""); err != nil {
			return err
		}
	}
	if c.Node {
		if err := installStep("node", "node", ""); err != nil { // "" = newest LTS
			return err
		}
	}
	if c.Redis {
		if err := installStep("redis", "redis", ""); err != nil { // also brings phpredis
			return err
		}
	}
	var notes []string
	if c.HTTPS {
		setState("https", "running", "Windows will ask you to confirm trusting Ebur's certificate")
		if err := enableHTTPS(ctx, root); err != nil {
			setState("https", "failed", err.Error())
			notes = append(notes, "HTTPS: "+err.Error())
		} else {
			setState("https", "done", "trusted by Windows")
		}
	}
	cfg, _ = config.Load(root.Config())
	cfg.SetupDone = true
	if err := config.Save(root.Config(), cfg); err != nil {
		return err
	}
	if hooks.Start != nil {
		setState("start", "running", "")
		if err := hooks.Start(); err != nil {
			setState("start", "failed", err.Error())
			return fmt.Errorf("start: %w", err)
		}
		setState("start", "done", "")
	}
	if hooks.Hosts != nil {
		setState("hosts", "running", "Windows will ask for administrator permission")
		if err := hooks.Hosts(); err != nil {
			setState("hosts", "failed", err.Error())
			notes = append(notes, "Site names: "+err.Error())
		} else {
			setState("hosts", "done", "")
		}
	}
	if len(notes) > 0 {
		return &Partial{Notes: notes}
	}
	return nil
}

// Partial means everything was installed and started, but an optional
// step (HTTPS trust, hosts file) did not complete.
type Partial struct{ Notes []string }

func (p *Partial) Error() string { return strings.Join(p.Notes, "\n") }

func tldOf(root paths.Root) string {
	cfg, _ := config.Load(root.Config())
	return cfg.TLD
}

// enableHTTPS installs the public roots for PHP, then creates and trusts
// the local CA (SSL-01, SSL-04).
func enableHTTPS(ctx context.Context, root paths.Root) error {
	if _, err := install.Runtime(ctx, root, "cacert", "", nil, nil); err != nil {
		return err
	}
	cfg, err := config.Load(root.Config())
	if err != nil {
		return err
	}
	dir := certs.Dir(root.SSL())
	ca, err := dir.LoadCA()
	if errors.Is(err, certs.ErrNoCA) {
		ca, err = dir.CreateCA(cfg.TLD)
	}
	if err != nil {
		return err
	}
	if err := ca.Trust(); err != nil {
		if errors.Is(err, certs.ErrDeclined) {
			return errors.New("not enabled: the Windows dialog was declined (enable it later in Settings or with `ebur ssl install`)")
		}
		return err
	}
	return nil
}
