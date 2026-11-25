package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/ebur-dev/ebur/core/certs"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/doctor"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/hostsfile"
	"github.com/ebur-dev/ebur/core/hostsync"
	"github.com/ebur-dev/ebur/core/install"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/setup"
	"github.com/ebur-dev/ebur/core/shellenv"
	"github.com/ebur-dev/ebur/core/sites"
	"github.com/ebur-dev/ebur/core/supervisor"
)

// App is bound to the frontend: every exported method can be called from
// JavaScript as "main.App.<Method>". Errors carry a next step (NFR-14).
type App struct {
	root    paths.Root
	b       *backend
	app     *application.App
	changed func() // refreshes the tray after an action
	// applyTheme recolours the open window's title bar.
	applyTheme func(mode string)
	hostsNote  string // why the last hosts sync failed, for the Hosts card

	setupMu      sync.Mutex
	setupRunning bool
}

func withHint(err error) error {
	if err == nil {
		return nil
	}
	var he interface{ Hint() string }
	if errors.As(err, &he) && he.Hint() != "" {
		return fmt.Errorf("%s\nNext step: %s", err.Error(), he.Hint())
	}
	return err
}

func (a *App) after(err error) error {
	if a.changed != nil {
		go a.changed()
	}
	return withHint(err)
}

// Overview is everything the main window shows at a glance.
type Overview struct {
	Status       host.Status     `json:"status"`
	Sites        []SiteRow       `json:"sites"`
	Skipped      []sites.Skipped `json:"skipped"`
	PHPActive    string          `json:"phpActive"`
	PHPInstalled []string        `json:"phpInstalled"`
	HTTPS        bool            `json:"https"`
	Adminer      bool            `json:"adminer"`
	TLD          string          `json:"tld"`
	Root         string          `json:"root"`
	Version      string          `json:"version"`
	NeedsSetup   bool            `json:"needsSetup"` // show the first-run wizard
}

// SiteRow is a site as the Sites tab and tray menu show it (GUI-03).
type SiteRow struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	DocRoot string `json:"docRoot"`
	PHP     string `json:"php"`
	// PHP-04: where the version came from (default, .ebur, composer.json),
	// what the project asked for, and any problem meeting that.
	PHPSource  string `json:"phpSource"`
	PHPWant    string `json:"phpWant"`
	PHPProblem string `json:"phpProblem"`
	Running    bool   `json:"running"`
}

func (a *App) Overview() (Overview, error) {
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return Overview{}, withHint(err)
	}
	var st host.Status
	if err := a.b.call("status", nil, &st); err != nil {
		st = host.Status{Summary: "none"}
	}
	ov := Overview{Status: st, TLD: cfg.TLD, Root: a.root.Dir(), Version: version, NeedsSetup: setup.NeedsSetup(a.root)}
	ov.PHPInstalled, _ = runtimes.Installed(a.root, "php")
	ov.PHPActive = cfg.Active["php"]
	if ov.PHPActive == "" && len(ov.PHPInstalled) > 0 {
		ov.PHPActive = ov.PHPInstalled[0]
	}
	if st.Info.PHP != "" {
		ov.PHPActive = st.Info.PHP
	}
	ov.HTTPS = certs.Dir(a.root.SSL()).Exists()
	adminer, _ := runtimes.Installed(a.root, "adminer")
	ov.Adminer = len(adminer) > 0
	scheme := "http"
	if ov.HTTPS {
		scheme = "https"
	}
	found, skipped, _ := sites.Scan(a.root.WWW(), cfg.TLD)
	ov.Skipped = skipped
	nginxUp := false
	for _, s := range st.Services {
		if s.Name == "nginx" && s.State == "running" {
			nginxUp = true
		}
	}
	for _, s := range found {
		if s.Name == "db" {
			continue
		}
		c := project.Resolve(s.Dir, ov.PHPInstalled, ov.PHPActive)
		ov.Sites = append(ov.Sites, SiteRow{
			Name: s.Name, URL: scheme + "://" + s.Host, DocRoot: s.DocRoot,
			PHP: c.Version, PHPSource: c.Source, PHPWant: c.Want, PHPProblem: c.Problem,
			Running: nginxUp,
		})
	}
	// Empty lists, not null, so the page can iterate without checks.
	if ov.Skipped == nil {
		ov.Skipped = []sites.Skipped{}
	}
	if ov.Sites == nil {
		ov.Sites = []SiteRow{}
	}
	if ov.PHPInstalled == nil {
		ov.PHPInstalled = []string{}
	}
	if ov.Status.Services == nil {
		ov.Status.Services = []supervisor.Status{}
	}
	return ov, nil
}

func (a *App) Start() (host.Status, error) {
	var st host.Status
	err := a.b.call("start", host.Selection{}, &st)
	if err == nil {
		// Map new sites; a declined or blocked prompt is shown in the
		// Hosts card, not as a failed start.
		if cfg, _ := config.Load(a.root.Config()); !cfg.ManualHosts {
			a.syncHosts(true)
		}
	}
	return st, a.after(err)
}

// HostsInfo is what the Hosts card shows (SITE-02).
type HostsInfo struct {
	Path     string   `json:"path"`
	Wanted   []string `json:"wanted"`
	InSync   bool     `json:"inSync"`
	Manual   bool     `json:"manual"`
	LastNote string   `json:"lastNote"`
}

func (a *App) Hosts() HostsInfo {
	cfg, _ := config.Load(a.root.Config())
	want, _ := hostsync.Wanted(a.root)
	ok, _ := hostsync.InSync(want)
	if want == nil {
		want = []string{}
	}
	return HostsInfo{Path: hostsfile.Path(), Wanted: want, InSync: ok, Manual: cfg.ManualHosts, LastNote: a.hostsNote}
}

// SyncHosts is the Update button: it always tries (one UAC prompt).
func (a *App) SyncHosts() (HostsInfo, error) { return a.syncHosts(false) }

// syncHosts maps every site to 127.0.0.1. After a start (auto) it does
// not prompt again for names whose last automatic attempt failed.
func (a *App) syncHosts(auto bool) (HostsInfo, error) {
	want, err := hostsync.Wanted(a.root)
	if err != nil {
		return a.Hosts(), err
	}
	if auto {
		_, err = hostsync.AutoSync(a.root, want)
	} else {
		hostsync.Forget(a.root)
		_, err = hostsync.Sync(want)
	}
	var skipped *hostsync.ErrSkipped
	switch {
	case errors.As(err, &skipped):
		a.hostsNote = "Not asked again after Start because the last attempt failed (" + skipped.Reason + "). Click Update hosts file to try again."
	case errors.Is(err, hostsync.ErrDeclined):
		a.hostsNote = "The administrator prompt was declined, so the hosts file was not updated."
	case errors.Is(err, hostsync.ErrLocked):
		a.hostsNote = "Security software is protecting the hosts file. Allow changes to it in that program's settings (Webroot: \"Prevent any program from modifying the HOSTS file\"), then try again."
	case err != nil:
		a.hostsNote = err.Error()
	default:
		a.hostsNote = ""
	}
	if a.hostsNote != "" {
		return a.Hosts(), errors.New(a.hostsNote)
	}
	return a.Hosts(), nil
}

func (a *App) Stop() (host.Status, error) {
	var st host.Status
	return st, a.after(a.b.call("stop", host.Selection{}, &st))
}

func (a *App) Restart() (host.Status, error) {
	var st host.Status
	return st, a.after(a.b.call("restart", host.Selection{}, &st))
}

// StartService and StopService take a group name such as "nginx" or "php".
func (a *App) StartService(name string) (host.Status, error) {
	var st host.Status
	return st, a.after(a.b.call("start", host.Selection{Services: []string{name}}, &st))
}

func (a *App) StopService(name string) (host.Status, error) {
	var st host.Status
	return st, a.after(a.b.call("stop", host.Selection{Services: []string{name}}, &st))
}

// SetSitePHP pins one site's PHP version in its .ebur file, or with
// version "" returns it to the default (PHP-04). Running services restart
// so the change applies at once.
func (a *App) SetSitePHP(site, version string) error {
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return withHint(err)
	}
	found, _, err := sites.Scan(a.root.WWW(), cfg.TLD)
	if err != nil {
		return err
	}
	for _, s := range found {
		if s.Name != site {
			continue
		}
		if err := project.SetPHP(s.Dir, version); err != nil {
			return err
		}
		a.restartIfRunning()
		return a.after(nil)
	}
	return fmt.Errorf("no site named %q", site)
}

// SetPHP switches the active PHP version (PHP-01, PHP-02).
func (a *App) SetPHP(version string) (host.Status, error) {
	var st host.Status
	return st, a.after(a.b.call("set-active", host.SetActive{Runtime: "php", Version: version}, &st))
}

// OpenURL opens a local site in the default browser. Only http(s) URLs for
// local names are allowed, so the page cannot open arbitrary programs.
func (a *App) OpenURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("not a web address: %q", raw)
	}
	cfg, _ := config.Load(a.root.Config())
	h := u.Hostname()
	if !strings.HasSuffix(h, "."+cfg.TLD) && h != "localhost" && h != "127.0.0.1" {
		return fmt.Errorf("only local sites can be opened from Ebur: %q", h)
	}
	return a.app.Browser.OpenURL(u.String())
}

func (a *App) OpenWWW() error  { return openFolder(a.root.WWW()) }
func (a *App) OpenLogs() error { return openFolder(a.root.Logs()) }

func openFolder(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return exec.Command("explorer.exe", dir).Start()
}

// OpenTerminal opens a console with Ebur's runtimes and Composer on PATH,
// without touching the system PATH (GUI-04, PHP-07).
func (a *App) OpenTerminal() error { return shellenv.Open(a.root, a.root.WWW()) }

// LogFile describes one file in logs\ (TOOL-02).
type LogFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

var logNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.log$`)

func (a *App) Logs() ([]LogFile, error) {
	entries, err := os.ReadDir(a.root.Logs())
	if err != nil {
		return []LogFile{}, nil
	}
	out := []LogFile{}
	for _, e := range entries {
		if e.IsDir() || !logNameRe.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, LogFile{Name: e.Name(), Size: fi.Size(), Modified: fi.ModTime().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, nil
}

// ReadLog returns up to the last n lines of a file in logs\. The name is
// checked so the page can only read Ebur's own logs.
func (a *App) ReadLog(name string, n int) ([]string, error) {
	if !logNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid log name %q", name)
	}
	if n <= 0 || n > 2000 {
		n = 300
	}
	f, err := os.Open(filepath.Join(a.root.Logs(), name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxRead = 2 << 20 // last 2 MB is plenty for a tail
	if fi, err := f.Stat(); err == nil && fi.Size() > maxRead {
		f.Seek(fi.Size()-maxRead, 0)
	}
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring, nil
}

func (a *App) Doctor() []doctor.Check {
	running := a.b.call("ping", nil, nil) == nil
	return doctor.Run(a.root, doctor.Options{HostRunning: running})
}

// DBInfo is what the Database panel shows.
type DBInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Adminer   string `json:"adminer"`
}

func (a *App) Database() DBInfo {
	cfg, _ := config.Load(a.root.Config())
	inst, _ := runtimes.Installed(a.root, "mariadb")
	info := DBInfo{Installed: len(inst) > 0, Port: cfg.DBPort, User: "root", Password: cfg.DBRootPassword}
	if info.Installed {
		info.Version = cfg.Active["mariadb"]
		if info.Version == "" {
			info.Version = inst[0]
		}
	}
	if ad, _ := runtimes.Installed(a.root, "adminer"); len(ad) > 0 {
		scheme := "http"
		if certs.Dir(a.root.SSL()).Exists() {
			scheme = "https"
		}
		info.Adminer = scheme + "://db." + cfg.TLD
	}
	return info
}

// SSLInfo is what the HTTPS panel shows.
type SSLInfo struct {
	Exists      bool   `json:"exists"`
	Trusted     bool   `json:"trusted"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	ValidTo     string `json:"validTo"`
}

func (a *App) SSL() SSLInfo {
	ca, err := certs.Dir(a.root.SSL()).LoadCA()
	if err != nil {
		return SSLInfo{}
	}
	trusted, _ := ca.IsTrusted()
	return SSLInfo{Exists: true, Trusted: trusted, Name: ca.Cert.Subject.CommonName,
		Fingerprint: ca.Fingerprint(), ValidTo: ca.Cert.NotAfter.Format("2006-01-02")}
}

// EnableHTTPS creates and trusts the local CA (SSL-01). Windows shows its
// own confirmation dialog; that is the user's consent.
func (a *App) EnableHTTPS() (SSLInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	install.Runtime(ctx, a.root, "cacert", "", nil, nil) // public roots for PHP; best effort
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return SSLInfo{}, withHint(err)
	}
	dir := certs.Dir(a.root.SSL())
	ca, err := dir.LoadCA()
	if errors.Is(err, certs.ErrNoCA) {
		ca, err = dir.CreateCA(cfg.TLD)
	}
	if err != nil {
		return SSLInfo{}, withHint(err)
	}
	if err := ca.Trust(); err != nil {
		if errors.Is(err, certs.ErrDeclined) {
			return a.SSL(), errors.New("HTTPS was not enabled because the Windows dialog was declined.\nNext step: click Enable HTTPS again and choose Yes.")
		}
		return a.SSL(), err
	}
	a.restartIfRunning()
	return a.SSL(), a.after(nil)
}

// DisableHTTPS removes the CA from Windows and deletes Ebur's certificates (SSL-03).
func (a *App) DisableHTTPS() (SSLInfo, error) {
	dir := certs.Dir(a.root.SSL())
	if ca, err := dir.LoadCA(); err == nil {
		if err := ca.Untrust(); err != nil {
			if errors.Is(err, certs.ErrDeclined) {
				return a.SSL(), errors.New("HTTPS is still enabled because the Windows dialog was declined.")
			}
			return a.SSL(), err
		}
	}
	if err := dir.Remove(); err != nil {
		return a.SSL(), err
	}
	a.restartIfRunning()
	return a.SSL(), a.after(nil)
}

// SetupOptions is what the first-run wizard offers (SRS §4.1).
func (a *App) SetupOptions() (setup.Options, error) {
	o, err := setup.GetOptions(a.root)
	return o, withHint(err)
}

// SetupResult is sent with the "setup:finished" event.
type SetupResult struct {
	OK    bool     `json:"ok"`
	Notes []string `json:"notes"` // optional steps that did not complete
	Error string   `json:"error"` // why setup stopped, if it did
}

// RunSetup starts the first-run setup in the background. Progress
// arrives as "setup:progress" events (the step list) and the end as one
// "setup:finished" event.
func (a *App) RunSetup(c setup.Choices) error {
	a.setupMu.Lock()
	if a.setupRunning {
		a.setupMu.Unlock()
		return errors.New("setup is already running")
	}
	a.setupRunning = true
	a.setupMu.Unlock()

	go func() {
		defer func() {
			a.setupMu.Lock()
			a.setupRunning = false
			a.setupMu.Unlock()
		}()
		hooks := setup.Hooks{
			Start: func() error {
				var st host.Status
				return withHint(a.b.call("start", host.Selection{}, &st))
			},
			Hosts: func() error {
				_, err := a.syncHosts(true)
				return err
			},
		}
		err := setup.Run(context.Background(), a.root, c, hooks, func(steps []setup.Step) {
			a.app.Event.Emit("setup:progress", steps)
		})
		res := SetupResult{OK: err == nil, Notes: []string{}}
		var partial *setup.Partial
		switch {
		case errors.As(err, &partial):
			res.OK, res.Notes = true, partial.Notes
		case err != nil:
			res.Error = withHint(err).Error()
		}
		a.app.Event.Emit("setup:finished", res)
		if a.changed != nil {
			a.changed()
		}
	}()
	return nil
}

// Appearance returns "system", "light" or "dark" (GUI-05).
func (a *App) Appearance() string {
	cfg, _ := config.Load(a.root.Config())
	if cfg.Theme == "" {
		return "system"
	}
	return cfg.Theme
}

// SetAppearance saves the choice and recolours the window's title bar;
// the page recolours itself.
func (a *App) SetAppearance(mode string) error {
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return withHint(err)
	}
	cfg.Theme = mode
	if err := config.Save(a.root.Config(), cfg); err != nil {
		return err
	}
	if a.applyTheme != nil {
		a.applyTheme(mode)
	}
	return nil
}

func (a *App) restartIfRunning() {
	var st host.Status
	if a.b.call("status", nil, &st) == nil && st.Summary != "none" {
		a.b.call("restart", host.Selection{}, &st)
	}
}
