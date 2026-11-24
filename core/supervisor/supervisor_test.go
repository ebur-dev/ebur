package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the service under test.
func TestMain(m *testing.M) {
	switch os.Getenv("EBUR_TEST_HELPER") {
	case "crash":
		fmt.Println("starting")
		fmt.Fprintln(os.Stderr, "fatal: port 3306 already in use")
		os.Exit(3)
	case "sleep":
		fmt.Println("ready")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(name, mode string) Spec {
	return Spec{Name: name, Path: os.Args[0], Env: []string{"EBUR_TEST_HELPER=" + mode}}
}

func fast() *Supervisor {
	s := New()
	s.Backoff = func(int) time.Duration { return 10 * time.Millisecond }
	return s
}

func waitState(t *testing.T, s *Supervisor, name string, want State) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.Status(name); st.State == want {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, _ := s.Status(name)
	t.Fatalf("%s: state %s, want %s", name, st.State, want)
	return st
}

func TestJobObject(t *testing.T) {
	if err := New().JobError(); err != nil {
		t.Fatalf("job object: %v", err)
	}
}

func TestCrashRestartsThenFails(t *testing.T) {
	s := fast()
	log := filepath.Join(t.TempDir(), "logs", "db.log")
	spec := helper("db", "crash")
	spec.LogFile = log
	if err := s.Add(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.Start("db"); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, s, "db", Failed)
	if st.Restarts != DefaultMaxRestarts {
		t.Errorf("restarts = %d, want %d", st.Restarts, DefaultMaxRestarts)
	}
	if !strings.Contains(st.Error, "exit code 3") || !strings.Contains(st.Error, "failed after 3 restarts") {
		t.Errorf("error = %q", st.Error)
	}
	if got := strings.Join(st.LastLines, "\n"); !strings.Contains(got, "port 3306 already in use") {
		t.Errorf("last lines missing cause: %q", got)
	}
	b, _ := os.ReadFile(log)
	if n := strings.Count(string(b), "starting"); n != 4 {
		t.Errorf("log shows %d launches, want 4 (1 + 3 restarts)", n)
	}
	// A failed service can be started again, with a fresh count.
	if err := s.Start("db"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "db", Failed)
}

func TestNeverRestart(t *testing.T) {
	s := fast()
	spec := helper("once", "crash")
	spec.MaxRestarts = -1
	s.Add(spec)
	s.Start("once")
	st := waitState(t, s, "once", Failed)
	if st.Restarts != 0 || strings.Contains(st.Error, "restarts") {
		t.Errorf("%+v", st)
	}
}

func TestStartStop(t *testing.T) {
	s := fast()
	s.Add(helper("web", "sleep"))
	s.Add(helper("php", "sleep"))
	if s.Summary() != "none" {
		t.Fatal(s.Summary())
	}
	if err := s.Start("web"); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, s, "web", Running)
	if st.PID == 0 {
		t.Fatal("no pid")
	}
	if err := s.Start("web"); err != nil { // idempotent
		t.Fatal(err)
	}
	if s.Summary() != "some" {
		t.Fatal(s.Summary())
	}
	if err := s.StartAll(); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "php", Running)
	if s.Summary() != "all" {
		t.Fatal(s.Summary())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status("web"); st.State != Stopped || st.PID != 0 {
		t.Fatalf("%+v", st)
	}
	if err := s.Stop(ctx, "web"); err != nil { // idempotent
		t.Fatal(err)
	}
	s.StopAll(ctx)
	if s.Summary() != "none" {
		t.Fatal(s.Summary())
	}
	if len(s.All()) != 2 {
		t.Fatal("All() lost services")
	}
	s.Start("web")
	waitState(t, s, "web", Running)
	s.Clear()
	if all := s.All(); len(all) != 1 || all[0].Name != "web" {
		t.Fatalf("Clear removed a running service or kept a stopped one: %+v", all)
	}
	s.StopAll(ctx)
}

func TestGracefulStopFallsBackToKill(t *testing.T) {
	s := fast()
	spec := helper("nginx", "sleep")
	called := false
	spec.Stop = func(ctx context.Context, pid int) error { called = true; return errors.New("nginx -s quit failed") }
	s.Add(spec)
	s.Start("nginx")
	waitState(t, s, "nginx", Running)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Stop(ctx, "nginx")
	if !called {
		t.Fatal("Stop func not called")
	}
	if st, _ := s.Status("nginx"); st.State != Stopped {
		t.Fatalf("%+v", st)
	}
}

func TestStopTimeoutKills(t *testing.T) {
	s := fast()
	spec := helper("stubborn", "sleep")
	spec.Stop = func(ctx context.Context, pid int) error { return nil } // claims success, does nothing
	s.Add(spec)
	s.Start("stubborn")
	waitState(t, s, "stubborn", Running)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	s.Stop(ctx, "stubborn")
	if d := time.Since(t0); d > 5*time.Second {
		t.Fatalf("stop took %v", d)
	}
	if st, _ := s.Status("stubborn"); st.State != Stopped {
		t.Fatalf("%+v", st)
	}
}

func TestStopDuringRestartBackoff(t *testing.T) {
	s := New()
	s.Backoff = func(int) time.Duration { return time.Minute }
	s.Add(helper("flaky", "crash"))
	s.Start("flaky")
	waitState(t, s, "flaky", Restarting)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t0 := time.Now()
	s.Stop(ctx, "flaky")
	if time.Since(t0) > 2*time.Second {
		t.Fatal("stop waited for the backoff")
	}
	if st, _ := s.Status("flaky"); st.State != Stopped {
		t.Fatalf("%+v", st)
	}
}

func TestErrors(t *testing.T) {
	s := fast()
	if err := s.Add(Spec{Name: "x"}); err == nil {
		t.Error("spec without path accepted")
	}
	s.Add(helper("a", "sleep"))
	if err := s.Add(helper("a", "sleep")); err == nil {
		t.Error("duplicate accepted")
	}
	if err := s.Start("nope"); err == nil {
		t.Error("unknown start accepted")
	}
	if err := s.Stop(context.Background(), "nope"); err == nil {
		t.Error("unknown stop accepted")
	}
	if _, ok := s.Status("nope"); ok {
		t.Error("unknown status found")
	}
	s.Add(Spec{Name: "missing", Path: filepath.Join(t.TempDir(), "no-such.exe")})
	if err := s.Start("missing"); err == nil {
		t.Error("missing binary started")
	}
	if st, _ := s.Status("missing"); st.State != Failed || st.Error == "" {
		t.Errorf("%+v", st)
	}
	if err := s.StartAll(); err == nil {
		t.Error("StartAll hid the missing binary")
	}
	s.StopAll(context.Background())
}

func TestRing(t *testing.T) {
	r := newRing(3)
	r.Write([]byte("a\r\nb\nc"))
	r.Write([]byte("d\ne\n"))
	if got := strings.Join(r.lines(), ","); got != "b,cd,e" {
		t.Fatalf("got %q", got)
	}
	r.Write([]byte("partial"))
	if got := strings.Join(r.lines(), ","); got != "cd,e,partial" {
		t.Fatalf("got %q", got)
	}
	r.reset()
	if len(r.lines()) != 0 {
		t.Fatal("reset failed")
	}
}
