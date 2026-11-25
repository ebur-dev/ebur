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
	"github.com/ebur-dev/ebur/core/dbclient"
	"github.com/ebur-dev/ebur/core/dbsnap"
	"github.com/ebur-dev/ebur/core/doctor"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/hostsfile"
	"github.com/ebur-dev/ebur/core/hostsync"
	"github.com/ebur-dev/ebur/core/install"
	"github.com/ebur-dev/ebur/core/newproject"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/setup"
	"github.com/ebur-dev/ebur/core/share"
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

	projectMu      sync.Mutex
	projectRunning bool
	projectCancel  context.CancelFunc

	hostsMu   sync.Mutex
	lastSites string // site names last seen, to notice new ones (SITE-05)
	hostsBusy bool
}

// noticeSites runs on every tray refresh. When the host's watcher has
// brought in new sites while services run, it maps their names with one
// administrator prompt (SITE-02, SITE-05). The first call only records
// the current names, so opening Ebur never prompts by itself.
func (a *App) noticeSites(ov Overview) {
	var names []string
	for _, s := range ov.Sites {
		names = append(names, s.Name)
	}
	key := strings.Join(names, ",")
	a.hostsMu.Lock()
	first, changed := a.lastSites == "", key != a.lastSites
	a.lastSites = key
	if first || !changed || a.hostsBusy || ov.Status.Summary == "none" {
		a.hostsMu.Unlock()
		return
	}
	if cfg, _ := config.Load(a.root.Config()); cfg.ManualHosts {
		a.hostsMu.Unlock()
		return
	}
	a.hostsBusy = true
	a.hostsMu.Unlock()
	go func() {
		defer func() { a.hostsMu.Lock(); a.hostsBusy = false; a.hostsMu.Unlock() }()
		a.syncHosts(true) // AutoSync: no repeat prompt for names already declined
	}()
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
	PHPLegacy    []string        `json:"phpLegacy"` // installed versions past end of life
	HTTPS        bool            `json:"https"`
	Adminer      bool            `json:"adminer"`
	Mail         bool            `json:"mail"` // Mailpit installed (TOOL-03)
	DBInstalled  bool            `json:"dbInstalled"`
	ShareReady   bool            `json:"shareReady"` // cloudflared installed (TOOL-04)
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
	PHPEOL     string `json:"phpEol"` // set when the version is past end of life
	Target     string `json:"target"` // linked project's real folder (`ebur link`)
	Running    bool   `json:"running"`
	// PROJ-03: Node for npm in this site ("" when Node is not installed).
	Node        string `json:"node"`
	NodeSource  string `json:"nodeSource"`
	NodeProblem string `json:"nodeProblem"`
}

// LinkProject asks for a folder and serves it as a site (like `ebur link`).
// It returns the new site's name, or "" if the user cancelled.
func (a *App) LinkProject() (string, error) {
	dir, err := a.app.Dialog.OpenFile().
		SetTitle("Choose a project folder to serve with Ebur").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetButtonText("Serve this folder").
		PromptForSingleSelection()
	if err != nil || dir == "" {
		return "", nil // cancelled
	}
	name, err := sites.Link(a.root.Dir(), a.root.WWW(), dir, "")
	return name, a.after(err)
}

// ProjectProgress is one "project:progress" event: the current step and
// the latest line of tool output (Composer).
type ProjectProgress struct {
	Step   string `json:"step"`
	Detail string `json:"detail"`
}

// ProjectFinished is the "project:finished" event.
type ProjectFinished struct {
	OK     bool              `json:"ok"`
	Error  string            `json:"error"`
	Result newproject.Result `json:"result"`
}

// CreateProject starts creating a site from a starter (PROJ-01/02) in the
// background, like `ebur new`. kind is laravel, wordpress, symfony or blank.
func (a *App) CreateProject(name, kind, php string) error {
	a.projectMu.Lock()
	if a.projectRunning {
		a.projectMu.Unlock()
		return errors.New("a project is already being created; wait for it to finish")
	}
	a.projectRunning = true
	ctx, cancel := context.WithCancel(context.Background())
	a.projectCancel = cancel
	a.projectMu.Unlock()

	go func() {
		defer func() {
			cancel()
			a.projectMu.Lock()
			a.projectRunning, a.projectCancel = false, nil
			a.projectMu.Unlock()
		}()
		var mu sync.Mutex
		p := ProjectProgress{}
		emit := func() { a.app.Event.Emit("project:progress", p) }
		say := func(step string) { mu.Lock(); p = ProjectProgress{Step: step}; emit(); mu.Unlock() }
		out := lineWriter(func(line string) { mu.Lock(); p.Detail = line; emit(); mu.Unlock() })

		k := newproject.Kind(kind)
		if k != newproject.Blank {
			say("Starting services")
			var st host.Status
			a.b.call("start", host.Selection{}, &st) // the database must run; Create explains if it does not
		}
		res, err := newproject.Create(ctx, a.root, newproject.Options{Name: name, Kind: k, PHP: php}, say, out)
		fin := ProjectFinished{OK: err == nil, Result: res}
		if err != nil {
			fin.Error = err.Error()
		}
		a.app.Event.Emit("project:finished", fin)
		if a.changed != nil {
			a.changed()
		}
	}()
	return nil
}

// CancelProject stops a project being created; nothing is kept.
func (a *App) CancelProject() {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	if a.projectCancel != nil {
		a.projectCancel()
	}
}

// lineWriter calls fn with each non-empty output line.
type lineWriter func(string)

func (w lineWriter) Write(b []byte) (int, error) {
	for _, l := range strings.FieldsFunc(string(b), func(r rune) bool { return r == '\n' || r == '\r' }) {
		if l = strings.TrimSpace(l); l != "" {
			w(l)
		}
	}
	return len(b), nil
}

// ShareSite gives a site a public trycloudflare.com address for minutes
// (TOOL-04), installing cloudflared first if needed (about 53 MB).
func (a *App) ShareSite(site string, minutes int, password bool) (share.Share, error) {
	if inst, _ := runtimes.Installed(a.root, "cloudflared"); len(inst) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if _, err := install.Runtime(ctx, a.root, "cloudflared", "", nil, nil); err != nil {
			return share.Share{}, withHint(err)
		}
	}
	var st host.Status
	if a.b.call("status", nil, &st) == nil && st.Summary == "none" {
		if err := a.b.call("start", host.Selection{}, &st); err != nil {
			return share.Share{}, withHint(err)
		}
	}
	var sh share.Share
	err := a.b.call("share", host.ShareRequest{Site: site, Minutes: minutes, Password: password}, &sh)
	return sh, a.after(err)
}

// UnshareSite ends a share now.
func (a *App) UnshareSite(site string) error {
	var left []share.Share
	return a.after(a.b.call("unshare", host.ShareRequest{Site: site}, &left))
}

// UnlinkSite stops serving a linked project; its files are not touched.
func (a *App) UnlinkSite(name string) error {
	return a.after(sites.Unlink(a.root.WWW(), name))
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
	mail, _ := runtimes.Installed(a.root, "mailpit")
	ov.Mail = len(mail) > 0
	db, _ := runtimes.Installed(a.root, "mariadb")
	ov.DBInstalled = len(db) > 0
	cf, _ := runtimes.Installed(a.root, "cloudflared")
	ov.ShareReady = len(cf) > 0
	if ov.Status.Shares == nil {
		ov.Status.Shares = []share.Share{}
	}
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
	nodes, _ := runtimes.Installed(a.root, "node")
	nodeDef := cfg.Active["node"]
	if nodeDef == "" && len(nodes) > 0 {
		nodeDef = nodes[0]
	}
	for _, s := range found {
		if s.Name == "db" {
			continue
		}
		c := project.Resolve(s.Dir, ov.PHPInstalled, ov.PHPActive)
		row := SiteRow{
			Name: s.Name, URL: scheme + "://" + s.Host, DocRoot: s.DocRoot,
			PHP: c.Version, PHPSource: c.Source, PHPWant: c.Want, PHPProblem: c.Problem,
			Target: s.Target, Running: nginxUp,
		}
		if len(nodes) > 0 {
			n := project.ResolveNode(s.Dir, nodes, nodeDef)
			row.Node, row.NodeSource, row.NodeProblem = n.Version, n.Source, n.Problem
		}
		if sup := install.SupportOf("php", c.Version); sup.Ended {
			row.PHPEOL = sup.EOL
		}
		ov.Sites = append(ov.Sites, row)
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
	ov.PHPLegacy = []string{}
	for _, v := range ov.PHPInstalled {
		if install.SupportOf("php", v).Ended {
			ov.PHPLegacy = append(ov.PHPLegacy, v)
		}
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

// MailInfo is what the Mail capture card shows (TOOL-03).
type MailInfo struct {
	Installed bool   `json:"installed"`
	URL       string `json:"url"`
	SMTPPort  int    `json:"smtpPort"`
}

func (a *App) Mail() MailInfo {
	cfg, _ := config.Load(a.root.Config())
	inst, _ := runtimes.Installed(a.root, "mailpit")
	scheme := "http"
	if certs.Dir(a.root.SSL()).Exists() {
		scheme = "https"
	}
	return MailInfo{Installed: len(inst) > 0, URL: scheme + "://mail." + cfg.TLD, SMTPPort: provision.MailSMTPPort}
}

// InstallMail downloads Mailpit and restarts running services so it
// starts and PHP's mail() is pointed at it.
func (a *App) InstallMail() (MailInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := install.Runtime(ctx, a.root, "mailpit", "", nil, nil); err != nil {
		return a.Mail(), withHint(err)
	}
	a.restartIfRunning()
	return a.Mail(), a.after(nil)
}

// RedisInfo is what the Redis card shows (SVC-05).
type RedisInfo struct {
	Installed  bool     `json:"installed"`
	Version    string   `json:"version"`
	Port       int      `json:"port"`
	PHPWith    []string `json:"phpWith"`    // PHP series with the phpredis extension
	PHPWithout []string `json:"phpWithout"` // PHP series without it (use predis)
}

func (a *App) Redis() RedisInfo {
	info := RedisInfo{Port: provision.RedisPort, PHPWith: []string{}, PHPWithout: []string{}}
	inst, _ := runtimes.Installed(a.root, "redis")
	if len(inst) == 0 {
		return info
	}
	info.Installed, info.Version = true, inst[0]
	phps, _ := runtimes.Installed(a.root, "php")
	seen := map[string]bool{}
	for _, v := range phps {
		s := provision.PHPSeries(v)
		if seen[s] {
			continue
		}
		seen[s] = true
		if _, ok := provision.PHPRedisDLL(a.root, v); ok {
			info.PHPWith = append(info.PHPWith, s)
		} else {
			info.PHPWithout = append(info.PHPWithout, s)
		}
	}
	return info
}

// InstallRedis downloads Redis and the phpredis extension for each
// installed PHP version, then restarts running services so it starts.
func (a *App) InstallRedis() (RedisInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := install.Runtime(ctx, a.root, "redis", "", nil, nil); err != nil {
		return a.Redis(), withHint(err)
	}
	a.restartIfRunning()
	return a.Redis(), a.after(nil)
}

// NodeVersion is one Node.js release on the Node card.
type NodeVersion struct {
	Version string `json:"version"`
	Major   string `json:"major"`
	EOL     string `json:"eol"`
	Legacy  bool   `json:"legacy"` // past end of life
}

// NodeInfo is what the Node.js card shows (PROJ-03).
type NodeInfo struct {
	Default   string        `json:"default"`
	Installed []NodeVersion `json:"installed"`
	Available []NodeVersion `json:"available"` // offered, not installed
}

func (a *App) Node() NodeInfo {
	cfg, _ := config.Load(a.root.Config())
	inst, _ := runtimes.Installed(a.root, "node")
	info := NodeInfo{Default: cfg.Active["node"], Installed: []NodeVersion{}, Available: []NodeVersion{}}
	if info.Default == "" && len(inst) > 0 {
		info.Default = inst[0]
	}
	row := func(v string) NodeVersion {
		s := install.SupportOf("node", v)
		m, _, _ := strings.Cut(v, ".")
		return NodeVersion{Version: v, Major: m, EOL: s.EOL, Legacy: s.Ended}
	}
	have := map[string]bool{}
	for _, v := range inst {
		info.Installed = append(info.Installed, row(v))
		have[v] = true
	}
	if m, err := install.Manifest(); err == nil {
		for _, r := range m.Releases("node", "x64") {
			if !have[r.Version] {
				info.Available = append(info.Available, row(r.Version))
			}
		}
	}
	return info
}

// InstallNode downloads a Node.js version, e.g. "24". The first one
// installed becomes the default.
func (a *App) InstallNode(version string) (NodeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, err := install.Runtime(ctx, a.root, "node", version, nil, nil)
	return a.Node(), a.after(err)
}

// SetNode makes an installed version the default. Nothing restarts:
// new Ebur terminals and `ebur exec` use it.
func (a *App) SetNode(version string) (NodeInfo, error) {
	inst, _ := runtimes.Installed(a.root, "node")
	found := false
	for _, v := range inst {
		found = found || v == version
	}
	if !found {
		return a.Node(), fmt.Errorf("Node %s is not installed", version)
	}
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return a.Node(), err
	}
	cfg.Active["node"] = version
	return a.Node(), a.after(config.Save(a.root.Config(), cfg))
}

// RemoveNode deletes an installed version.
func (a *App) RemoveNode(version string) (NodeInfo, error) {
	if err := runtimes.Remove(a.root, "node", version, nil); err != nil {
		return a.Node(), fmt.Errorf("%w\nNext step: close Ebur terminals and stop `npm run dev`, then try again", err)
	}
	cfg, _ := config.Load(a.root.Config())
	if cfg.Active["node"] == version {
		delete(cfg.Active, "node")
		config.Save(a.root.Config(), cfg)
	}
	return a.Node(), a.after(nil)
}

// DBRow is one database on the Databases tab (TOOL-05).
type DBRow struct {
	Name      string            `json:"name"`
	Tables    int               `json:"tables"`
	Bytes     int64             `json:"bytes"`
	Sites     []string          `json:"sites"`
	Snapshots []dbsnap.Snapshot `json:"snapshots"`
}

// DBList is the Databases tab: databases, or why they cannot be listed.
type DBList struct {
	Installed bool    `json:"installed"`
	Running   bool    `json:"running"`
	Databases []DBRow `json:"databases"`
	Folder    string  `json:"folder"`
}

func (a *App) Databases() DBList {
	out := DBList{Databases: []DBRow{}, Folder: dbsnap.Dir(a.root)}
	c, err := dbclient.Open(a.root)
	if err != nil {
		return out
	}
	out.Installed = true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dbs, err := c.Databases(ctx)
	if err != nil {
		return out
	}
	out.Running = true
	cfg, _ := config.Load(a.root.Config())
	found, _, _ := sites.Scan(a.root.WWW(), cfg.TLD)
	siteOf := map[string][]string{}
	for _, s := range found {
		db, _ := dbsnap.SiteDatabase(s.Dir, s.Name)
		siteOf[db] = append(siteOf[db], s.Name)
	}
	snaps, _ := dbsnap.List(a.root, "")
	for _, d := range dbs {
		row := DBRow{Name: d.Name, Tables: d.Tables, Bytes: d.Bytes, Sites: siteOf[d.Name], Snapshots: []dbsnap.Snapshot{}}
		if row.Sites == nil {
			row.Sites = []string{}
		}
		for _, s := range snaps {
			if s.Database == d.Name {
				row.Snapshots = append(row.Snapshots, s)
			}
		}
		out.Databases = append(out.Databases, row)
	}
	return out
}

func (a *App) dbClient() (*dbclient.Client, error) {
	c, err := dbclient.Open(a.root)
	if err != nil {
		return nil, withHint(err)
	}
	return c, nil
}

// SnapshotDB saves a snapshot of db with an optional label.
func (a *App) SnapshotDB(db, label string) (dbsnap.Snapshot, error) {
	c, err := a.dbClient()
	if err != nil {
		return dbsnap.Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s, err := dbsnap.Save(ctx, c, a.root, db, label)
	return s, withHint(err)
}

// RestoreDB replaces db with a snapshot; the current data is saved first.
func (a *App) RestoreDB(db, name string) (dbsnap.RestoreResult, error) {
	c, err := a.dbClient()
	if err != nil {
		return dbsnap.RestoreResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := dbsnap.Restore(ctx, c, a.root, db, name)
	return res, withHint(err)
}

// DeleteSnapshot removes one snapshot file; the database is not touched.
func (a *App) DeleteSnapshot(db, name string) error {
	_, err := dbsnap.Delete(a.root, db, name)
	return withHint(err)
}

// OpenSnapshots opens the snapshots folder in Explorer.
func (a *App) OpenSnapshots() error {
	dir := dbsnap.Dir(a.root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return openFolder(dir)
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
