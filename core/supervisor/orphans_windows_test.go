package supervisor

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
}

// Regression: after `ebur ssl uninstall` restarted the stack, an nginx
// worker outlived its stopped master and kept ports 80/443.
func TestStopEndsLeftoverChildren(t *testing.T) {
	s := fast()
	s.Add(helper("nginx", "parent"))
	if err := s.Start("nginx"); err != nil {
		t.Fatal(err)
	}
	var child int
	deadline := time.Now().Add(10 * time.Second)
	for child == 0 && time.Now().Before(deadline) {
		st, _ := s.Status("nginx")
		for _, l := range st.LastLines {
			if v, ok := strings.CutPrefix(l, "child="); ok {
				child, _ = strconv.Atoi(v)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if child == 0 || !alive(child) {
		t.Fatalf("child not started (pid %d)", child)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Stop(ctx, "nginx")
	if alive(child) {
		t.Fatalf("child %d outlived its stopped parent", child)
	}
	st, _ := s.Status("nginx")
	// The count includes the child's conhost.exe, also a descendant.
	if !strings.Contains(strings.Join(st.LastLines, "\n"), "leftover child process(es) of nginx") {
		t.Errorf("cleanup not reported: %q", st.LastLines)
	}
}
