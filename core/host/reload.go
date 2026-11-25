package host

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/sites"
	"github.com/ebur-dev/ebur/core/supervisor"
)

// ReloadResult says what a reload changed.
type ReloadResult struct {
	Started []string `json:"started"` // services added or replaced
	Stopped []string `json:"stopped"` // services no longer needed
}

func sameSpec(a, b supervisor.Spec) bool {
	return a.Path == b.Path && a.Dir == b.Dir && a.LogFile == b.LogFile &&
		slices.Equal(a.Args, b.Args) && slices.Equal(a.Env, b.Env)
}

// Reload applies site changes to running services without a full restart
// (SITE-05): only services whose definition changed are stopped or
// started, then Nginx re-reads its config gracefully. MariaDB and
// unaffected PHP pools keep running. With nothing running it does
// nothing; the next start picks the changes up.
func (h *Host) Reload() (ReloadResult, error) {
	var res ReloadResult
	h.mu.Lock()
	if !h.anyRunning() {
		h.mu.Unlock()
		return res, nil
	}
	cfg, err := config.Load(h.Root.Config())
	if err != nil {
		h.mu.Unlock()
		return res, err
	}
	st, err := h.Build(h.Root, cfg) // writes and tests the new nginx.conf (NFR-10)
	if err != nil {
		h.mu.Unlock()
		return res, err
	}
	want := map[string]supervisor.Spec{}
	for _, s := range st.Specs {
		want[s.Name] = s
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.StopTimeout)
	defer cancel()

	// Order matters (measured: stopping first left Nginx routing a site to
	// a pool that was shutting down, so unpinning took 3.3 s):
	//  1. replace changed services and start new ones,
	//  2. reload Nginx so every site points at a running pool,
	//  3. only then stop services nothing needs any more.
	wasRunning := map[string]bool{}
	var gone []string
	for _, cur := range h.sup.All() {
		running := cur.State == supervisor.Running || cur.State == supervisor.Restarting
		wasRunning[cur.Name] = running
		next, keep := want[cur.Name]
		if !keep {
			gone = append(gone, cur.Name)
			continue
		}
		if old, _ := h.sup.Spec(cur.Name); sameSpec(old, next) {
			continue
		}
		if running {
			h.sup.Stop(ctx, cur.Name)
		}
		h.sup.Remove(cur.Name)
	}
	// Add and start what is new or changed. A service the user had
	// stopped stays stopped; new ones (e.g. a pool for a newly pinned PHP
	// version) start.
	for _, s := range st.Specs {
		if _, ok := h.sup.Spec(s.Name); ok {
			continue
		}
		if err := h.sup.Add(s); err != nil {
			h.mu.Unlock()
			return res, err
		}
		if running, known := wasRunning[s.Name]; known && !running {
			continue // the user had stopped it: keep it stopped
		}
		if err := h.sup.Start(s.Name); err != nil {
			log.Printf("reload: start %s: %v", s.Name, err)
			continue
		}
		res.Started = append(res.Started, s.Name)
	}
	h.info, h.ports = st.Info, st.Ports
	h.wanted = map[string]bool{}
	for name := range want {
		h.wanted[name] = true
	}
	h.mu.Unlock()

	// New PHP workers must accept connections before Nginx routes to them.
	if len(res.Started) > 0 {
		if err := h.waitReady(res.Started, st.Ports); err != nil {
			return res, err
		}
	}
	if s, ok := h.sup.Status("nginx"); ok && s.State == supervisor.Running && st.Reload != nil {
		if err := st.Reload(ctx); err != nil {
			return res, fmt.Errorf("nginx reload: %w", err)
		}
	}
	// New requests no longer reach these, but Nginx's old worker may still
	// be finishing requests with the old config: stopping at once made
	// such a request hang (measured). Stop them after a grace period, in
	// the background, unless a later reload needs them again.
	if len(gone) > 0 {
		res.Stopped = gone
		go func(names []string) {
			time.Sleep(h.RetireGrace)
			h.mu.Lock()
			defer h.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), h.StopTimeout)
			defer cancel()
			for _, name := range names {
				if h.wanted[name] {
					continue // needed again since
				}
				h.sup.Stop(ctx, name)
				h.sup.Remove(name)
			}
		}(gone)
	}
	return res, nil
}

// siteSignature changes whenever something that affects how sites are
// served changes: folders in www\, their document root, and their
// .ebur and composer.json files (PHP-04).
func siteSignature(root paths.Root, tld string) string {
	found, skipped, _ := sites.Scan(root.WWW(), tld)
	var b strings.Builder
	for _, s := range found {
		fmt.Fprintf(&b, "%s|%s", s.Name, s.DocRoot)
		for _, f := range []string{project.FileName, "composer.json"} {
			if fi, err := os.Stat(filepath.Join(s.Dir, f)); err == nil {
				fmt.Fprintf(&b, "|%s:%d:%d", f, fi.Size(), fi.ModTime().UnixNano())
			}
		}
		b.WriteByte('\n')
	}
	for _, s := range skipped {
		fmt.Fprintf(&b, "skipped|%s\n", s.Folder)
	}
	return b.String()
}

// watch polls www\ and reloads when sites change (SITE-05). A change is
// applied once it has been stable for one interval, so copying a project
// in triggers one reload, not dozens. Target: a new site answers within
// 2 seconds (NFR-05).
func (h *Host) watch(ctx context.Context) {
	if h.WatchInterval <= 0 {
		return
	}
	tld := func() string {
		cfg, err := config.Load(h.Root.Config())
		if err != nil {
			return "test"
		}
		return cfg.TLD
	}
	last, pending := siteSignature(h.Root, tld()), ""
	t := time.NewTicker(h.WatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.expireShares() // TOOL-04: shares end on time
		cur := siteSignature(h.Root, tld())
		if !h.anyRunning() {
			last, pending = cur, "" // a start rebuilds from scratch anyway
			continue
		}
		if cur == last {
			pending = ""
			continue
		}
		if cur != pending {
			pending = cur // wait for it to settle
			continue
		}
		last, pending = cur, ""
		t0 := time.Now()
		res, err := h.Reload()
		if err != nil {
			log.Printf("sites changed; reload failed: %v", err)
			continue
		}
		log.Printf("sites changed; reloaded in %v (started %v, stopped %v)", time.Since(t0).Round(time.Millisecond), res.Started, res.Stopped)
	}
}
