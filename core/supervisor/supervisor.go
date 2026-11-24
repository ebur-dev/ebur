// Package supervisor starts, stops and watches service processes
// (SVC-01, SVC-04).
//
// Every child runs inside a kill-on-close Job Object on Windows, so if
// Ebur exits or crashes the OS kills them all and no orphan keeps port
// 80 or 3306 busy (SRS §4.4). A service that crashes is restarted up to
// MaxRestarts times; after that it is marked failed with its last output
// lines, so the GUI and CLI can show why.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type State string

const (
	Stopped    State = "stopped"
	Running    State = "running"
	Restarting State = "restarting"
	Failed     State = "failed"
)

const (
	DefaultMaxRestarts = 3
	DefaultStableAfter = 30 * time.Second
	tailLines          = 20
)

// Spec describes one service process.
type Spec struct {
	Name    string
	Path    string
	Args    []string
	Env     []string // added to the parent environment
	Dir     string
	LogFile string // stdout and stderr are appended here when set
	// Stop asks the process to exit cleanly (e.g. "nginx -s quit").
	// When nil, or when it fails, the process is killed.
	Stop func(ctx context.Context, pid int) error
	// MaxRestarts after a crash: 0 means DefaultMaxRestarts, -1 never.
	MaxRestarts int
	// StableAfter is how long a process must run before its crash count
	// resets. 0 means DefaultStableAfter.
	StableAfter time.Duration
}

// Status is a snapshot of one service.
type Status struct {
	Name      string
	State     State
	PID       int
	Restarts  int
	Error     string
	LastLines []string
}

type Supervisor struct {
	// Backoff is the wait before restart n (1-based). Tests shorten it.
	Backoff func(n int) time.Duration

	mu   sync.Mutex
	svcs map[string]*service
	job  error
}

type service struct {
	spec     Spec
	state    State
	cmd      *exec.Cmd
	logFile  *os.File
	restarts int
	err      string
	tail     *ring
	stopping bool
	stopCh   chan struct{}
	done     chan struct{}
}

// New returns a Supervisor. On Windows it puts the current process in a
// kill-on-close Job Object; if that fails, services still run but are
// not guaranteed to die with Ebur, and JobError reports why.
func New() *Supervisor {
	return &Supervisor{
		Backoff: func(n int) time.Duration { return time.Duration(n) * 500 * time.Millisecond },
		svcs:    map[string]*service{},
		job:     joinKillOnCloseJob(),
	}
}

// JobError reports whether the kill-on-close Job Object could be set up.
func (s *Supervisor) JobError() error { return s.job }

// Add registers a service. It does not start it.
func (s *Supervisor) Add(spec Spec) error {
	if spec.Name == "" || spec.Path == "" {
		return errors.New("supervisor: spec needs a name and a path")
	}
	if spec.MaxRestarts == 0 {
		spec.MaxRestarts = DefaultMaxRestarts
	}
	if spec.StableAfter == 0 {
		spec.StableAfter = DefaultStableAfter
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.svcs[spec.Name]; ok {
		return fmt.Errorf("supervisor: service %q already added", spec.Name)
	}
	s.svcs[spec.Name] = &service{spec: spec, state: Stopped, tail: newRing(tailLines)}
	return nil
}

// Clear forgets every stopped or failed service, so a new set of specs
// can be added (e.g. after a PHP version switch). Running services stay.
func (s *Supervisor) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for n, sv := range s.svcs {
		if sv.state == Stopped || sv.state == Failed {
			delete(s.svcs, n)
		}
	}
}

// Start launches a service. Starting a running service does nothing.
// An error is returned if the first launch fails (e.g. missing binary).
func (s *Supervisor) Start(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.svcs[name]
	if !ok {
		return fmt.Errorf("supervisor: unknown service %q", name)
	}
	if sv.state == Running || sv.state == Restarting {
		return nil
	}
	sv.restarts, sv.err, sv.stopping = 0, "", false
	sv.tail.reset()
	sv.stopCh = make(chan struct{})
	sv.done = make(chan struct{})
	if err := s.spawn(sv); err != nil {
		sv.state = Failed
		sv.err = err.Error()
		close(sv.done)
		return fmt.Errorf("start %s: %w", name, err)
	}
	go s.watch(sv)
	return nil
}

// spawn starts one process for sv. Caller holds s.mu.
func (s *Supervisor) spawn(sv *service) error {
	cmd := exec.Command(sv.spec.Path, sv.spec.Args...)
	cmd.Env = append(os.Environ(), sv.spec.Env...)
	cmd.Dir = sv.spec.Dir
	cmd.WaitDelay = 2 * time.Second // don't hang if a grandchild keeps our pipes open
	var out io.Writer = sv.tail
	var lf *os.File
	if sv.spec.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(sv.spec.LogFile), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(sv.spec.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		lf = f
		out = io.MultiWriter(f, sv.tail)
	}
	cmd.Stdout, cmd.Stderr = out, out
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		if lf != nil {
			lf.Close()
		}
		return err
	}
	sv.cmd, sv.logFile, sv.state = cmd, lf, Running
	return nil
}

// watch waits for the process and applies the restart policy.
func (s *Supervisor) watch(sv *service) {
	for {
		s.mu.Lock()
		cmd, lf := sv.cmd, sv.logFile
		s.mu.Unlock()

		started := time.Now()
		waitErr := cmd.Wait()
		if lf != nil {
			lf.Close()
		}

		s.mu.Lock()
		sv.cmd, sv.logFile = nil, nil
		if sv.stopping {
			sv.state = Stopped
			close(sv.done)
			s.mu.Unlock()
			return
		}
		if time.Since(started) >= sv.spec.StableAfter {
			sv.restarts = 0
		}
		sv.restarts++
		if sv.spec.MaxRestarts < 0 || sv.restarts > sv.spec.MaxRestarts {
			sv.state = Failed
			sv.err = fmt.Sprintf("%s stopped unexpectedly (%s)", sv.spec.Name, describeExit(waitErr))
			if sv.spec.MaxRestarts > 0 {
				sv.err += fmt.Sprintf(" and failed after %d restarts", sv.spec.MaxRestarts)
			}
			sv.restarts--
			close(sv.done)
			s.mu.Unlock()
			return
		}
		sv.state = Restarting
		n, stopCh := sv.restarts, sv.stopCh
		s.mu.Unlock()

		select {
		case <-time.After(s.Backoff(n)):
		case <-stopCh:
		}

		s.mu.Lock()
		if sv.stopping {
			sv.state = Stopped
			close(sv.done)
			s.mu.Unlock()
			return
		}
		if err := s.spawn(sv); err != nil {
			sv.state = Failed
			sv.err = fmt.Sprintf("%s could not be restarted: %v", sv.spec.Name, err)
			close(sv.done)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

func describeExit(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit code %d", ee.ExitCode())
	}
	if err == nil {
		return "exit code 0"
	}
	return err.Error()
}

// Stop stops a service: the Spec's Stop function first, then a kill if
// that fails or ctx expires. Stopping a stopped service does nothing.
func (s *Supervisor) Stop(ctx context.Context, name string) error {
	s.mu.Lock()
	sv, ok := s.svcs[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("supervisor: unknown service %q", name)
	}
	if sv.state != Running && sv.state != Restarting {
		s.mu.Unlock()
		return nil
	}
	if !sv.stopping {
		sv.stopping = true
		close(sv.stopCh)
	}
	var proc *os.Process
	if sv.cmd != nil {
		proc = sv.cmd.Process
	}
	done, stopFn := sv.done, sv.spec.Stop
	s.mu.Unlock()

	if proc != nil {
		if stopFn == nil || stopFn(ctx, proc.Pid) != nil {
			proc.Kill()
		}
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		if proc != nil {
			proc.Kill()
		}
		<-done
		return nil
	}
}

// StartAll starts every service in name order and returns all errors.
func (s *Supervisor) StartAll() error {
	var errs []error
	for _, n := range s.names() {
		if err := s.Start(n); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// StopAll stops every service concurrently.
func (s *Supervisor) StopAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, n := range s.names() {
		wg.Add(1)
		go func() { defer wg.Done(); s.Stop(ctx, n) }()
	}
	wg.Wait()
}

func (s *Supervisor) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.svcs))
	for n := range s.svcs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Status returns a snapshot of one service.
func (s *Supervisor) Status(name string) (Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.svcs[name]
	if !ok {
		return Status{}, false
	}
	st := Status{Name: name, State: sv.state, Restarts: sv.restarts, Error: sv.err, LastLines: sv.tail.lines()}
	if sv.cmd != nil && sv.cmd.Process != nil {
		st.PID = sv.cmd.Process.Pid
	}
	return st, true
}

// All returns every service's status, sorted by name.
func (s *Supervisor) All() []Status {
	var out []Status
	for _, n := range s.names() {
		if st, ok := s.Status(n); ok {
			out = append(out, st)
		}
	}
	return out
}

// Summary returns the tray colour state (GUI-01): "all", "some" or "none" running.
func (s *Supervisor) Summary() string {
	all := s.All()
	running := 0
	for _, st := range all {
		if st.State == Running {
			running++
		}
	}
	switch {
	case len(all) > 0 && running == len(all):
		return "all"
	case running > 0:
		return "some"
	}
	return "none"
}
