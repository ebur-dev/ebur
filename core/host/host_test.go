package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/ipc"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/supervisor"
)

func TestMain(m *testing.M) {
	switch os.Getenv("EBUR_TEST_HELPER") {
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "crash":
		fmt.Println("php-cgi: unable to bind")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func spec(name, mode string) supervisor.Spec {
	return supervisor.Spec{Name: name, Path: os.Args[0], Env: []string{"EBUR_TEST_HELPER=" + mode}, MaxRestarts: -1}
}

// fakeHost builds a stack of helper processes and counts rebuilds.
func fakeHost(t *testing.T, specs func(cfg config.Settings) []supervisor.Spec, busyPorts ...int) (*Host, *atomic.Int32) {
	t.Helper()
	root, _ := paths.New(t.TempDir())
	h := New(root)
	h.Settle = 300 * time.Millisecond
	builds := &atomic.Int32{}
	h.Build = func(_ paths.Root, cfg config.Settings) (Stack, error) {
		builds.Add(1)
		return Stack{Specs: specs(cfg), Ports: busyPorts, Info: Info{PHP: cfg.Active["php"]}}, nil
	}
	t.Cleanup(func() { h.Stop(Selection{}) })
	return h, builds
}

func normal(cfg config.Settings) []supervisor.Spec {
	return []supervisor.Spec{spec("nginx", "sleep"), spec("php-8.4.28#1", "sleep"), spec("php-8.4.28#2", "sleep")}
}

func TestStartStopRestart(t *testing.T) {
	h, builds := fakeHost(t, normal)
	st, err := h.Start(Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Summary != "all" || len(st.Services) != 3 {
		t.Fatalf("%+v", st)
	}
	// Stopping "php" stops both workers and leaves nginx.
	st, _ = h.Stop(Selection{Services: []string{"php"}})
	if st.Summary != "some" {
		t.Fatalf("summary %s", st.Summary)
	}
	// Starting while something runs must not rebuild.
	if _, err := h.Start(Selection{Services: []string{"php"}}); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 1 {
		t.Fatalf("rebuilt while running: %d builds", builds.Load())
	}
	// A full restart reloads settings (PHP-02).
	config.Save(h.Root.Config(), func() config.Settings { c := config.Defaults(); c.Active["php"] = "8.3.37"; return c }())
	st, err = h.Restart(Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 2 || st.Info.PHP != "8.3.37" || st.Summary != "all" {
		t.Fatalf("restart did not reload: builds=%d info=%+v", builds.Load(), st.Info)
	}
	if _, err := h.Stop(Selection{Services: []string{"mysql"}}); err == nil {
		t.Fatal("unknown service accepted")
	}
}

func TestSetActive(t *testing.T) {
	h, builds := fakeHost(t, normal)
	for _, v := range []string{"8.3.37", "8.4.9", "8.4.28"} {
		os.MkdirAll(h.Root.RuntimeDir("php", v), 0o755)
	}
	if _, err := h.SetActive(SetActive{Runtime: "php", Version: "8.4"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(h.Root.Config())
	if cfg.Active["php"] != "8.4.28" {
		t.Fatalf("prefix resolved to %q", cfg.Active["php"])
	}
	if builds.Load() != 0 {
		t.Fatal("restarted while nothing was running")
	}
	h.Start(Selection{})
	st, err := h.SetActive(SetActive{Runtime: "php", Version: "8.3"})
	if err != nil || st.Info.PHP != "8.3.37" || builds.Load() != 2 {
		t.Fatalf("running switch: err=%v php=%s builds=%d", err, st.Info.PHP, builds.Load())
	}
	var he interface{ Hint() string }
	if _, err := h.SetActive(SetActive{Runtime: "php", Version: "7.4"}); !errors.As(err, &he) {
		t.Fatalf("missing version: %v", err)
	}
}

func TestInstantCrashIsReported(t *testing.T) {
	h, _ := fakeHost(t, func(config.Settings) []supervisor.Spec {
		return []supervisor.Spec{spec("nginx", "sleep"), spec("php-8.4.28#1", "crash")}
	})
	_, err := h.Start(Selection{})
	var he interface{ Hint() string }
	if !errors.As(err, &he) || !strings.Contains(err.Error(), "php-8.4.28#1") || !strings.Contains(he.Hint(), "unable to bind") {
		t.Fatalf("got %v", err)
	}
}

func TestPortConflictBlocksStart(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	h, _ := fakeHost(t, normal, port)
	st, err := h.Start(Selection{})
	var he interface{ Hint() string }
	if !errors.As(err, &he) || !strings.Contains(err.Error(), fmt.Sprint(port)) || he.Hint() == "" {
		t.Fatalf("got %v", err)
	}
	if st.Summary != "none" {
		t.Fatal("something started despite the conflict")
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func TestStartWaitsForPorts(t *testing.T) {
	port := freePort(t)
	h, _ := fakeHost(t, normal)
	h.Build = func(paths.Root, config.Settings) (Stack, error) {
		return Stack{Specs: normal(config.Defaults()), Ports: []int{port}}, nil
	}
	// The port opens 700ms after start, like a slow first php-cgi launch.
	go func() {
		time.Sleep(700 * time.Millisecond)
		ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			time.Sleep(3 * time.Second)
			ln.Close()
		}
	}()
	t0 := time.Now()
	if _, err := h.Start(Selection{}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d < 600*time.Millisecond {
		t.Fatalf("returned after %v, before the port opened", d)
	}
}

func TestStartReportsPortsThatNeverOpen(t *testing.T) {
	port := freePort(t)
	h, _ := fakeHost(t, normal)
	h.ReadyTimeout = 500 * time.Millisecond
	h.Build = func(paths.Root, config.Settings) (Stack, error) {
		return Stack{Specs: normal(config.Defaults()), Ports: []int{port}}, nil
	}
	_, err := h.Start(Selection{})
	var he interface{ Hint() string }
	if !errors.As(err, &he) || !strings.Contains(err.Error(), "not accepting connections") || !strings.Contains(he.Hint(), "antivirus") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildErrorAndBadConfig(t *testing.T) {
	h, _ := fakeHost(t, normal)
	h.Build = func(paths.Root, config.Settings) (Stack, error) {
		return Stack{}, &hinted{"php is not installed", "Run `ebur php install 8.4`."}
	}
	if _, err := h.Start(Selection{}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("got %v", err)
	}
	os.WriteFile(h.Root.Config(), []byte("{broken"), 0o644)
	_, err := h.Start(Selection{})
	var he interface{ Hint() string }
	if !errors.As(err, &he) || !strings.Contains(he.Hint(), "ebur.json") {
		t.Fatalf("got %v", err)
	}
}

func TestRunOverIPC(t *testing.T) {
	h, _ := fakeHost(t, normal)
	config.Save(h.Root.Config(), func() config.Settings { c := config.Defaults(); c.AutoStart = []string{"nginx"}; return c }())
	done := make(chan error, 1)
	go func() { done <- h.Run(context.Background()) }()

	call := func(method string, params, out any) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ipc.Call(ctx, h.Root, method, params, out)
	}
	var ping Ping
	deadline := time.Now().Add(5 * time.Second)
	for call("ping", nil, &ping) != nil {
		if time.Now().After(deadline) {
			t.Fatal("host never answered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ping.PID != os.Getpid() || ping.Root != h.Root.Dir() {
		t.Fatalf("%+v", ping)
	}

	// Auto-start (SVC-06) started nginx only.
	var st Status
	deadline = time.Now().Add(5 * time.Second)
	for {
		call("status", nil, &st)
		if st.Summary == "some" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Summary != "some" {
		t.Fatalf("auto-start: %+v", st)
	}
	if err := call("start", Selection{}, &st); err != nil || st.Summary != "all" {
		t.Fatalf("start: %v %+v", err, st)
	}
	if err := call("stop", Selection{Services: []string{"nginx"}}, &st); err != nil || st.Summary != "some" {
		t.Fatalf("stop: %v %+v", err, st)
	}
	if err := call("restart", Selection{}, &st); err != nil || st.Summary != "all" {
		t.Fatalf("restart: %v %+v", err, st)
	}
	var e *ipc.Error
	if err := call("bogus", nil, nil); !errors.As(err, &e) {
		t.Fatalf("bogus: %v", err)
	}
	if err := call("start", "not-an-object", nil); !errors.As(err, &e) {
		t.Fatalf("bad params: %v", err)
	}
	if err := call("shutdown", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("host did not shut down")
	}
	if h.Status().Summary != "none" {
		t.Fatal("services still running after shutdown")
	}
}
