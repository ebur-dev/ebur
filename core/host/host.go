// Package host is the long-running owner of Ebur's services. The tray app
// embeds it; `ebur daemon` runs it headless. The CLI talks to it over
// core/ipc, so every CLI command acts on the same processes as the GUI
// (CLI-01) and services stop when Ebur quits (SRS §4.4).
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/ipc"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/ports"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/sites"
	"github.com/ebur-dev/ebur/core/supervisor"
)

// Version is set by the build.
var Version = "dev"

// Stack is everything needed to run one configuration.
type Stack struct {
	Specs []supervisor.Spec
	Ports []int // must be free before the stack starts
	Info  Info
	// Reload tells the running web server to re-read its config without
	// dropping connections (SITE-05). Nil if not supported.
	Reload func(ctx context.Context) error
}

// Info describes the running configuration for status displays.
type Info struct {
	PHP string `json:"php"` // the default version
	// PHPVersions are all versions with a running pool, default first
	// (PHP-04: sites can pin other versions).
	PHPVersions []string        `json:"php_versions,omitempty"`
	Nginx       string          `json:"nginx"`
	Listen      string          `json:"listen"`
	MariaDB     string          `json:"mariadb,omitempty"`
	HTTPS       bool            `json:"https,omitempty"`
	Adminer     string          `json:"adminer,omitempty"` // URL
	Sites       []sites.Site    `json:"sites"`
	Skipped     []sites.Skipped `json:"skipped,omitempty"`
}

// BuildFunc turns settings into a Stack. Tests replace it.
type BuildFunc func(root paths.Root, cfg config.Settings) (Stack, error)

type Host struct {
	Root        paths.Root
	Build       BuildFunc
	StopTimeout time.Duration
	Settle      time.Duration // how long Start waits to catch instant crashes
	// ReadyTimeout bounds how long a full start waits for every port to
	// accept connections. The first run after an install can take many
	// seconds while antivirus scans the new binaries.
	ReadyTimeout time.Duration
	// WatchInterval is how often www\ is checked for site changes
	// (SITE-05). Zero disables the watcher.
	WatchInterval time.Duration
	// RetireGrace is how long a pool no site uses keeps running after a
	// reload, so Nginx's old worker can finish requests sent to it.
	RetireGrace time.Duration

	sup    *supervisor.Supervisor
	wanted map[string]bool // services the latest stack needs (see Reload)
	mu     sync.Mutex      // one start/stop/restart at a time
	info   Info
	ports  []int
	quit   chan struct{}
	once   sync.Once
}

func New(root paths.Root) *Host {
	return &Host{
		Root:          root,
		Build:         BuildStack,
		StopTimeout:   10 * time.Second,
		Settle:        300 * time.Millisecond,
		ReadyTimeout:  45 * time.Second,
		WatchInterval: 300 * time.Millisecond, // change seen within ~0.6 s (NFR-05: site live in 2 s)
		RetireGrace:   5 * time.Second,
		sup:           supervisor.New(),
		quit:          make(chan struct{}),
	}
}

// Ping is returned by the "ping" method.
type Ping struct {
	PID     int    `json:"pid"`
	Root    string `json:"root"`
	Version string `json:"version"`
}

// Status is returned by status, start, stop and restart.
type Status struct {
	Summary  string              `json:"summary"` // all, some, none (GUI-01)
	Services []supervisor.Status `json:"services"`
	Info     Info                `json:"info"`
}

// Selection names services: "nginx", "php", or exact names such as
// "php-8.4.28#2". Empty means all.
type Selection struct {
	Services []string `json:"services,omitempty"`
}

// hinted is an error with a next step (NFR-14).
type hinted struct{ msg, hint string }

func (e *hinted) Error() string { return e.msg }
func (e *hinted) Hint() string  { return e.hint }

// Run serves IPC until ctx ends or a client sends "shutdown", then stops
// every service.
func (h *Host) Run(ctx context.Context) error {
	if err := h.sup.JobError(); err != nil {
		log.Printf("warning: services may outlive Ebur (job object: %v)", err)
	}
	ln, err := ipc.Listen(h.Root)
	if err != nil {
		return err
	}
	defer ln.Close()
	log.Printf("ebur host %s listening (pid %d, root %s)", Version, os.Getpid(), h.Root.Dir())
	go ipc.Serve(ctx, ln, h.Handle)
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	go h.watch(watchCtx) // SITE-05

	if cfg, err := config.Load(h.Root.Config()); err == nil && len(cfg.AutoStart) > 0 { // SVC-06
		if _, err := h.Start(Selection{Services: cfg.AutoStart}); err != nil {
			log.Printf("auto-start: %v", err)
		}
	}

	select {
	case <-ctx.Done():
	case <-h.quit:
	}
	log.Printf("stopping all services")
	stopCtx, cancel := context.WithTimeout(context.Background(), h.StopTimeout)
	defer cancel()
	h.sup.StopAll(stopCtx)
	return nil
}

// Handle dispatches one IPC request.
func (h *Host) Handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	var sel Selection
	if len(params) > 0 {
		if err := json.Unmarshal(params, &sel); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
	}
	switch method {
	case "ping":
		return Ping{PID: os.Getpid(), Root: h.Root.Dir(), Version: Version}, nil
	case "status":
		return h.Status(), nil
	case "start":
		return h.Start(sel)
	case "stop":
		return h.Stop(sel)
	case "restart":
		return h.Restart(sel)
	case "set-active":
		var p SetActive
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		return h.SetActive(p)
	case "shutdown":
		h.once.Do(func() {
			// Reply first, then quit.
			go func() { time.Sleep(100 * time.Millisecond); close(h.quit) }()
		})
		return nil, nil
	}
	return nil, fmt.Errorf("unknown method %q", method)
}

func (h *Host) Status() Status {
	h.mu.Lock()
	info := h.info
	h.mu.Unlock()
	return Status{Summary: h.sup.Summary(), Services: h.sup.All(), Info: info}
}

func (h *Host) anyRunning() bool { return h.sup.Summary() != "none" }

// rebuild loads settings, builds a fresh stack and checks its ports.
// Caller holds h.mu and nothing is running.
func (h *Host) rebuild() error {
	cfg, err := config.Load(h.Root.Config())
	if err != nil {
		return &hinted{err.Error(), "Fix or delete ebur.json; Ebur recreates it with defaults."}
	}
	st, err := h.Build(h.Root, cfg)
	if err != nil {
		return err
	}
	if conflicts := ports.CheckAll(st.Ports...); len(conflicts) > 0 { // SVC-03
		var msgs, hints []string
		for _, c := range conflicts {
			msgs = append(msgs, c.Error())
			hints = append(hints, c.Hint())
		}
		return &hinted{strings.Join(msgs, "; "), hints[0]}
	}
	h.sup.Clear()
	for _, s := range st.Specs {
		if err := h.sup.Add(s); err != nil {
			return err
		}
	}
	h.info = st.Info
	h.ports = st.Ports
	// A delayed retire from an earlier reload must not stop what this
	// stack needs.
	h.wanted = map[string]bool{}
	for _, s := range st.Specs {
		h.wanted[s.Name] = true
	}
	return nil
}

// failedAmong returns the first selected service that crashed.
func failedAmong(st Status, names []string) (supervisor.Status, bool) {
	for _, s := range st.Services {
		if (s.State == supervisor.Failed || s.State == supervisor.Restarting) && contains(names, s.Name) {
			return s, true
		}
	}
	return supervisor.Status{}, false
}

func crashError(s supervisor.Status) error {
	hint := "Run `ebur doctor`, or check the logs folder."
	if len(s.LastLines) > 0 {
		hint = "Last output: " + s.LastLines[len(s.LastLines)-1]
	}
	return &hinted{fmt.Sprintf("%s did not stay up: %s", s.Name, firstNonEmpty(s.Error, string(s.State))), hint}
}

// waitReady polls until every port accepts a TCP connection, a selected
// service crashes, or the timeout passes.
func (h *Host) waitReady(names []string, ports []int) error {
	deadline := time.Now().Add(h.ReadyTimeout)
	pending := append([]int(nil), ports...)
	for len(pending) > 0 {
		if s, bad := failedAmong(h.Status(), names); bad {
			return crashError(s)
		}
		var still []int
		for _, p := range pending {
			c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)), 200*time.Millisecond)
			if err != nil {
				still = append(still, p)
				continue
			}
			c.Close()
		}
		pending = still
		if len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			return &hinted{
				fmt.Sprintf("services started but ports %v are not accepting connections after %v", pending, h.ReadyTimeout),
				"The first start after installing a runtime can be slow while antivirus scans it. Wait a moment and run `ebur status`.",
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// match expands a selection to service names.
func (h *Host) match(sel Selection) ([]string, error) {
	var all []string
	for _, s := range h.sup.All() {
		all = append(all, s.Name)
	}
	if len(sel.Services) == 0 {
		return all, nil
	}
	var out []string
	for _, want := range sel.Services {
		found := false
		for _, n := range all {
			if n == want || strings.HasPrefix(n, want+"-") || strings.HasPrefix(n, want+"#") {
				out = append(out, n)
				found = true
			}
		}
		if !found {
			return nil, &hinted{fmt.Sprintf("no service named %q", want), "Run `ebur status` to see service names."}
		}
	}
	return out, nil
}

func (h *Host) Start(sel Selection) (Status, error) {
	h.mu.Lock()
	if !h.anyRunning() {
		if err := h.rebuild(); err != nil {
			h.mu.Unlock()
			return h.Status(), err
		}
	}
	names, err := h.match(sel)
	if err != nil {
		h.mu.Unlock()
		return h.Status(), err
	}
	var errs []error
	for _, n := range names {
		if err := h.sup.Start(n); err != nil {
			errs = append(errs, err)
		}
	}
	full := len(sel.Services) == 0
	ports := h.ports
	h.mu.Unlock()
	if len(errs) > 0 {
		return h.Status(), errors.Join(errs...)
	}
	// Catch services that die immediately (bad config, port taken).
	time.Sleep(h.Settle)
	if s, bad := failedAmong(h.Status(), names); bad {
		return h.Status(), crashError(s)
	}
	// "Running" is not enough: report success only once sites can be
	// served. Partial starts skip this, as their ports are not tracked.
	if full {
		if err := h.waitReady(names, ports); err != nil {
			return h.Status(), err
		}
	}
	return h.Status(), nil
}

func (h *Host) Stop(sel Selection) (Status, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	names, err := h.match(sel)
	if err != nil {
		return h.statusLocked(), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.StopTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func() { defer wg.Done(); h.sup.Stop(ctx, n) }()
	}
	wg.Wait()
	return h.statusLocked(), nil
}

// Restart stops the selection and starts it again. When everything is
// stopped in between, settings are reloaded, so `ebur php 8.3` followed
// by a restart picks up the new version (PHP-02).
func (h *Host) Restart(sel Selection) (Status, error) {
	if _, err := h.Stop(sel); err != nil {
		return h.Status(), err
	}
	return h.Start(sel)
}

// SetActive picks the active version of a runtime, e.g. PHP from the tray
// menu (PHP-01). Version may be a prefix such as "8.4"; the newest
// installed match wins. Running services restart on it (PHP-02).
type SetActive struct {
	Runtime string `json:"runtime"`
	Version string `json:"version"`
}

func (h *Host) SetActive(p SetActive) (Status, error) {
	installed, err := runtimes.Installed(h.Root, p.Runtime)
	if err != nil {
		return h.Status(), err
	}
	var match string
	for _, v := range installed { // newest first
		if v == p.Version || strings.HasPrefix(v, p.Version+".") {
			match = v
			break
		}
	}
	if match == "" {
		return h.Status(), &hinted{
			fmt.Sprintf("%s %s is not installed (installed: %s)", p.Runtime, p.Version, strings.Join(installed, ", ")),
			fmt.Sprintf("Install it first, e.g. `ebur install %s %s`.", p.Runtime, p.Version),
		}
	}
	cfg, err := config.Load(h.Root.Config())
	if err != nil {
		return h.Status(), err
	}
	if cfg.Active[p.Runtime] != match {
		cfg.Active[p.Runtime] = match
		if err := config.Save(h.Root.Config(), cfg); err != nil {
			return h.Status(), err
		}
	}
	if h.anyRunning() {
		return h.Restart(Selection{})
	}
	return h.Status(), nil
}

func (h *Host) statusLocked() Status {
	return Status{Summary: h.sup.Summary(), Services: h.sup.All(), Info: h.info}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
