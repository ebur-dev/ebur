package host

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/nginxconf"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/ports"
	"github.com/ebur-dev/ebur/core/share"
	"github.com/ebur-dev/ebur/core/sites"
	"github.com/ebur-dev/ebur/core/supervisor"
	"github.com/ebur-dev/ebur/internal/fsutil"
)

// ShareService is the supervisor name of a site's tunnel.
func ShareService(site string) string { return "share-" + site }

// shareParts turns the active shares into a tunnel service and an Nginx
// server each (TOOL-04). Shares of sites that no longer exist are skipped.
func shareParts(root paths.Root, cfg config.Settings, found []sites.Site) ([]supervisor.Spec, []nginxconf.ShareServer, error) {
	shares, err := share.Load(root)
	if err != nil || len(shares) == 0 {
		return nil, nil, err
	}
	ver, err := optional(root, cfg, "cloudflared")
	if err != nil || ver == "" {
		return nil, nil, err
	}
	exe := filepath.Join(root.RuntimeDir("cloudflared", ver), "cloudflared.exe")
	emptyCfg, err := share.EmptyConfig(root)
	if err != nil {
		return nil, nil, err
	}
	var specs []supervisor.Spec
	var servers []nginxconf.ShareServer
	for _, sh := range shares {
		var site *sites.Site
		for i := range found {
			if found[i].Name == sh.Site {
				site = &found[i]
			}
		}
		if site == nil {
			continue
		}
		srv := nginxconf.ShareServer{Site: *site, Listen: listenAddr + ":" + strconv.Itoa(sh.Port)}
		if sh.Password != "" {
			srv.Htpasswd = share.HtpasswdFile(root, sh.Site)
		}
		servers = append(servers, srv)
		specs = append(specs, supervisor.Spec{
			Name: ShareService(sh.Site),
			Path: exe,
			// Unique per share, so sharing a site again always starts a new
			// tunnel (with a new address) instead of reusing a running one.
			Env: []string{"EBUR_SHARE_STARTED=" + strconv.FormatInt(sh.Started.UnixNano(), 10)},
			// A quick tunnel: no account, random address. Auto-update off:
			// Ebur pins the version (RUN-01).
			Args:    []string{"tunnel", "--no-autoupdate", "--config", emptyCfg, "--url", "http://" + srv.Listen},
			Dir:     filepath.Dir(exe),
			LogFile: share.LogFile(root, sh.Site),
		})
	}
	return specs, servers, nil
}

// ShareRequest is the "share" method's parameter.
type ShareRequest struct {
	Site     string `json:"site"`
	Minutes  int    `json:"minutes"`  // 0 means the default (60)
	Password bool   `json:"password"` // ask visitors for a generated password
}

// connectTimeout bounds how long a new share waits for its address.
var connectTimeout = 45 * time.Second

// Share makes site public through a quick tunnel until it expires, is
// stopped, or the web server stops. Sharing an already shared site
// returns the existing share with a new end time.
func (h *Host) Share(req ShareRequest) (share.Share, error) {
	h.shareMu.Lock()
	defer h.shareMu.Unlock()
	d := time.Duration(req.Minutes) * time.Minute
	if req.Minutes == 0 {
		d = share.DefaultDuration
	}
	if d < time.Minute || d > share.MaxDuration {
		return share.Share{}, &hinted{fmt.Sprintf("a share can last from 1 minute to %v", share.MaxDuration), "Use e.g. --for 30m or --for 3h."}
	}
	if s, ok := h.sup.Status("nginx"); !ok || s.State != supervisor.Running {
		return share.Share{}, &hinted{"the web server is not running", "Run `ebur start` first."}
	}
	cfg, err := config.Load(h.Root.Config())
	if err != nil {
		return share.Share{}, err
	}
	if v, _ := optional(h.Root, cfg, "cloudflared"); v == "" {
		return share.Share{}, &hinted{"cloudflared is not installed", "Run `ebur install cloudflared` (about 53 MB), or share from the window, which installs it."}
	}
	found, _, _ := sites.Scan(h.Root.WWW(), cfg.TLD)
	var site *sites.Site
	for i := range found {
		if found[i].Name == strings.ToLower(req.Site) {
			site = &found[i]
		}
	}
	if site == nil {
		return share.Share{}, &hinted{fmt.Sprintf("no site named %q", req.Site), "Run `ebur sites` to see site names."}
	}
	shares, err := share.Load(h.Root)
	if err != nil {
		return share.Share{}, err
	}
	if cur, i := share.Find(shares, site.Name); i >= 0 {
		cur.Expires = time.Now().Add(d)
		shares[i] = cur
		if err := share.Save(h.Root, shares); err != nil {
			return share.Share{}, err
		}
		return h.withURL(cur), nil
	}
	port, err := share.PickPort(shares, func(p int) bool { return ports.Check(p) == nil })
	if err != nil {
		return share.Share{}, err
	}
	sh := share.Share{Site: site.Name, Port: port, Started: time.Now(), Expires: time.Now().Add(d)}
	if req.Password {
		pw, err := share.NewPassword()
		if err != nil {
			return share.Share{}, err
		}
		line, err := share.Htpasswd("guest", pw)
		if err != nil {
			return share.Share{}, err
		}
		if err := fsutil.WriteFileAtomic(share.HtpasswdFile(h.Root, site.Name), []byte(line), 0o600); err != nil {
			return share.Share{}, err
		}
		sh.User, sh.Password = "guest", pw
	}
	if _, err := share.EnsureWordPressPlugin(site.Dir); err != nil {
		return share.Share{}, fmt.Errorf("add the WordPress share plugin: %w", err)
	}
	os.Remove(share.LogFile(h.Root, site.Name)) // a fresh log holds only this tunnel's address
	if err := share.Save(h.Root, append(shares, sh)); err != nil {
		return share.Share{}, err
	}
	fail := func(err error) (share.Share, error) {
		h.dropShares([]string{site.Name})
		return share.Share{}, err
	}
	if _, err := h.Reload(); err != nil {
		return fail(err)
	}
	// Wait for cloudflared to announce the address and connect to Cloudflare.
	deadline := time.Now().Add(connectTimeout)
	for {
		b, _ := os.ReadFile(share.LogFile(h.Root, site.Name))
		if share.Connected(string(b)) {
			sh.URL = share.URLFromLog(string(b))
			break
		}
		if st, ok := h.sup.Status(ShareService(site.Name)); ok && st.State == supervisor.Failed {
			return fail(&hinted{"the tunnel failed to start:\n" + share.LastLines(string(b), 5), "Check your internet connection; see " + share.LogFile(h.Root, site.Name) + "."})
		}
		if time.Now().After(deadline) {
			return fail(&hinted{"Cloudflare did not answer within " + connectTimeout.String() + ":\n" + share.LastLines(string(b), 5),
				"Check your internet connection, or whether a firewall blocks cloudflared; see " + share.LogFile(h.Root, site.Name) + "."})
		}
		time.Sleep(250 * time.Millisecond)
	}
	shares, _ = share.Load(h.Root)
	if _, i := share.Find(shares, site.Name); i >= 0 {
		shares[i].URL = sh.URL
		share.Save(h.Root, shares)
	}
	log.Printf("shared %s at %s until %s", site.Name, sh.URL, sh.Expires.Format("15:04"))
	return sh, nil
}

// withURL fills in the share's current address from its tunnel's log
// (it changes if the tunnel had to restart).
func (h *Host) withURL(sh share.Share) share.Share {
	if b, err := os.ReadFile(share.LogFile(h.Root, sh.Site)); err == nil {
		if u := share.URLFromLog(string(b)); u != "" {
			sh.URL = u
		}
	}
	return sh
}

// Shares lists active shares with their current addresses.
func (h *Host) Shares() []share.Share {
	shares, _ := share.Load(h.Root)
	for i := range shares {
		shares[i] = h.withURL(shares[i])
	}
	return shares
}

// Unshare ends the share of site ("" or "all" ends every share).
func (h *Host) Unshare(site string) ([]share.Share, error) {
	h.shareMu.Lock()
	defer h.shareMu.Unlock()
	shares, err := share.Load(h.Root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range shares {
		if site == "" || site == "all" || s.Site == strings.ToLower(site) {
			names = append(names, s.Site)
		}
	}
	if len(names) == 0 {
		return h.Shares(), &hinted{fmt.Sprintf("%s is not shared", site), "Run `ebur share` to see active shares."}
	}
	return h.Shares(), h.dropShares(names)
}

// dropShares removes shares and reloads so their tunnels stop and their
// Nginx servers go. Caller holds shareMu.
func (h *Host) dropShares(names []string) error {
	shares, err := share.Load(h.Root)
	if err != nil {
		return err
	}
	// End the tunnels first and at once: the public address must stop
	// working now, not after the grace period Reload gives PHP pools.
	ctx, cancel := context.WithTimeout(context.Background(), h.StopTimeout)
	defer cancel()
	for _, n := range names {
		h.sup.Stop(ctx, ShareService(n))
		h.sup.Remove(ShareService(n))
	}
	keep := shares[:0]
	for _, s := range shares {
		drop := false
		for _, n := range names {
			drop = drop || s.Site == n
		}
		if drop {
			os.Remove(share.HtpasswdFile(h.Root, s.Site))
			log.Printf("share of %s ended", s.Site)
		} else {
			keep = append(keep, s)
		}
	}
	if err := share.Save(h.Root, keep); err != nil {
		return err
	}
	_, err = h.Reload()
	return err
}

// expireShares ends shares whose time is up. Called by the watcher.
func (h *Host) expireShares() {
	shares, err := share.Load(h.Root)
	if err != nil || len(shares) == 0 {
		return
	}
	var names []string
	for _, s := range share.Expired(shares, time.Now()) {
		names = append(names, s.Site)
	}
	if len(names) == 0 {
		return
	}
	h.shareMu.Lock()
	defer h.shareMu.Unlock()
	if err := h.dropShares(names); err != nil {
		log.Printf("ending expired shares: %v", err)
	}
}

// endAllShares forgets every share, e.g. when the web server stops: a
// restarted tunnel would get a different address anyway.
func (h *Host) endAllShares() {
	h.shareMu.Lock()
	defer h.shareMu.Unlock()
	if shares, _ := share.Load(h.Root); len(shares) > 0 {
		log.Printf("web server stopped; ended %d share(s)", len(shares))
	}
	if err := share.Clear(h.Root); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("clearing shares: %v", err)
	}
}
