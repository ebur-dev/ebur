package host

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/supervisor"
)

// reloadHost has a stack the test can change between builds.
func reloadHost(t *testing.T) (*Host, *[]supervisor.Spec, *sync.Mutex, *atomic.Int32) {
	t.Helper()
	h, _ := fakeHost(t, normal)
	var mu sync.Mutex
	specs := normal(config.Defaults())
	reloads := &atomic.Int32{}
	h.Build = func(paths.Root, config.Settings) (Stack, error) {
		mu.Lock()
		defer mu.Unlock()
		return Stack{
			Specs:  append([]supervisor.Spec(nil), specs...),
			Reload: func(context.Context) error { reloads.Add(1); return nil },
		}, nil
	}
	return h, &specs, &mu, reloads
}

func pid(h *Host, name string) int {
	s, _ := h.sup.Status(name)
	return s.PID
}

func TestReloadOnlyTouchesWhatChanged(t *testing.T) {
	h, specs, mu, reloads := reloadHost(t)
	if _, err := h.Start(Selection{}); err != nil {
		t.Fatal(err)
	}
	nginxPID, php1PID := pid(h, "nginx"), pid(h, "php-8.4.28#1")

	// A site now pins PHP 8.3: a new pool appears; worker #2 changes; #1 is unchanged.
	mu.Lock()
	(*specs)[2].Env = append((*specs)[2].Env, "CHANGED=1")
	*specs = append(*specs, spec("php-8.3.37#1", "sleep"))
	mu.Unlock()
	res, err := h.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Started, "php-8.3.37#1") || !slices.Contains(res.Started, "php-8.4.28#2") || len(res.Stopped) != 0 {
		t.Fatalf("%+v", res)
	}
	if pid(h, "nginx") != nginxPID || pid(h, "php-8.4.28#1") != php1PID {
		t.Fatal("an unchanged service was restarted")
	}
	if reloads.Load() != 1 {
		t.Fatalf("nginx reloaded %d times", reloads.Load())
	}

	// The pin is removed: the 8.3 pool is no longer needed. It keeps
	// running through the grace period (Nginx's old worker may still send
	// it requests), then stops.
	h.RetireGrace = 300 * time.Millisecond
	mu.Lock()
	*specs = (*specs)[:len(*specs)-1]
	mu.Unlock()
	res, _ = h.Reload()
	if !slices.Equal(res.Stopped, []string{"php-8.3.37#1"}) {
		t.Fatalf("%+v", res)
	}
	if s, _ := h.sup.Status("php-8.3.37#1"); s.State != supervisor.Running {
		t.Fatal("retired pool stopped before the grace period")
	}
	time.Sleep(h.RetireGrace + 700*time.Millisecond)
	if _, ok := h.sup.Status("php-8.3.37#1"); ok {
		t.Fatal("retired pool still there after the grace period")
	}
}

// Re-pinning within the grace period keeps the pool instead of killing it.
func TestRetiredPoolNeededAgainIsKept(t *testing.T) {
	h, specs, mu, _ := reloadHost(t)
	h.RetireGrace = 400 * time.Millisecond
	mu.Lock()
	*specs = append(*specs, spec("php-8.3.37#1", "sleep"))
	mu.Unlock()
	h.Start(Selection{})
	mu.Lock()
	*specs = (*specs)[:len(*specs)-1] // unpin
	mu.Unlock()
	h.Reload()
	mu.Lock()
	*specs = append(*specs, spec("php-8.3.37#1", "sleep")) // pin again, quickly
	mu.Unlock()
	h.Reload()
	time.Sleep(h.RetireGrace + 600*time.Millisecond)
	if s, _ := h.sup.Status("php-8.3.37#1"); s.State != supervisor.Running {
		t.Fatalf("pool needed again was stopped: %+v", s)
	}
}

func TestReloadKeepsUserStoppedServicesStopped(t *testing.T) {
	h, specs, mu, _ := reloadHost(t)
	h.Start(Selection{})
	h.Stop(Selection{Services: []string{"php"}})
	mu.Lock()
	(*specs)[1].Env = append((*specs)[1].Env, "CHANGED=1")
	mu.Unlock()
	h.Reload()
	if s, _ := h.sup.Status("php-8.4.28#1"); s.State == supervisor.Running {
		t.Fatal("a service the user stopped was started by a reload")
	}
}

func TestReloadWhileStoppedDoesNothing(t *testing.T) {
	h, _, _, reloads := reloadHost(t)
	if res, err := h.Reload(); err != nil || len(res.Started)+len(res.Stopped) != 0 || reloads.Load() != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

// A pool's ports depend only on its version, not on which others run.
func TestPoolPortsAreStable(t *testing.T) {
	installed := []string{"8.5.13", "8.4.28", "8.3.37"} // newest first
	got := map[string]int{}
	for _, v := range installed {
		got[v] = poolPort(installed, v, 4)
	}
	if got["8.3.37"] != phpBasePort || got["8.4.28"] != phpBasePort+4 || got["8.5.13"] != phpBasePort+8 {
		t.Fatalf("%v", got)
	}
	// Installing a newer version leaves existing blocks where they were.
	more := append([]string{"8.6.0"}, installed...)
	for _, v := range installed {
		if poolPort(more, v, 4) != got[v] {
			t.Fatalf("%s moved after installing 8.6.0", v)
		}
	}
}

func TestSiteSignature(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	base := siteSignature(root, "test")
	shop := filepath.Join(root.WWW(), "shop")
	os.MkdirAll(shop, 0o755)
	s1 := siteSignature(root, "test")
	if s1 == base {
		t.Fatal("new folder not noticed")
	}
	os.MkdirAll(filepath.Join(shop, "public"), 0o755)
	s2 := siteSignature(root, "test")
	if s2 == s1 {
		t.Fatal("new public\\ folder (document root) not noticed")
	}
	os.WriteFile(filepath.Join(shop, ".ebur"), []byte("php: 8.3\n"), 0o644)
	s3 := siteSignature(root, "test")
	if s3 == s2 {
		t.Fatal(".ebur not noticed")
	}
	os.WriteFile(filepath.Join(shop, "public", "index.php"), []byte("<?php"), 0o644)
	if siteSignature(root, "test") != s3 {
		t.Fatal("ordinary file edits should not trigger a reload")
	}
}

// The watcher applies a new site by itself, without Start or Restart.
func TestWatcherReloadsOnNewSite(t *testing.T) {
	h, _, _, reloads := reloadHost(t)
	h.WatchInterval = 50 * time.Millisecond
	h.Start(Selection{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.watch(ctx)
	time.Sleep(120 * time.Millisecond)
	os.MkdirAll(filepath.Join(h.Root.WWW(), "newsite"), 0o755)
	deadline := time.Now().Add(3 * time.Second)
	for reloads.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if reloads.Load() == 0 {
		t.Fatal("watcher did not reload after a new site appeared")
	}
}
