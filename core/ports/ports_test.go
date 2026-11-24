package ports

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func listen(t *testing.T, network, addr string) int {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Skipf("cannot listen on %s: %v", addr, err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func TestConflictNamesThisProcess(t *testing.T) {
	port := listen(t, "tcp4", "127.0.0.1:0")
	err := Check(port)
	var c *Conflict
	if !errors.As(err, &c) {
		t.Fatalf("want *Conflict, got %v", err)
	}
	if runtime.GOOS == "windows" {
		exe, _ := os.Executable()
		if c.Owner.PID != os.Getpid() || !strings.EqualFold(c.Owner.Name, filepath.Base(exe)) {
			t.Fatalf("owner = %+v, want pid %d %s", c.Owner, os.Getpid(), filepath.Base(exe))
		}
		if !strings.Contains(c.Error(), "PID") || !strings.Contains(c.Hint(), "Close") {
			t.Fatalf("message: %s / %s", c.Error(), c.Hint())
		}
	}
}

func TestIPv6Listener(t *testing.T) {
	port := listen(t, "tcp6", "[::1]:0")
	if runtime.GOOS != "windows" {
		t.Skip("owner lookup is Windows-only for now")
	}
	var c *Conflict
	if err := Check(port); !errors.As(err, &c) || c.Owner.PID != os.Getpid() {
		t.Fatalf("IPv6 listener not found: %v", err)
	}
}

func TestFreePort(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := Check(port); err != nil {
		t.Fatalf("free port reported busy: %v", err)
	}
	if got := CheckAll(port); len(got) != 0 {
		t.Fatalf("CheckAll: %v", got)
	}
}

func TestInvalidPort(t *testing.T) {
	for _, p := range []int{0, -1, 70000} {
		if err := Check(p); err == nil {
			t.Errorf("port %d accepted", p)
		}
	}
}

func TestMessages(t *testing.T) {
	sys := &Conflict{Port: 80, Owner: Owner{PID: 4, Name: "System (http.sys)"}}
	if !strings.Contains(sys.Hint(), "IIS") {
		t.Error(sys.Hint())
	}
	unknown := &Conflict{Port: 443}
	if !strings.Contains(unknown.Error(), "another program") || !strings.Contains(unknown.Hint(), "Settings") {
		t.Error(unknown.Error(), unknown.Hint())
	}
}

func TestCheckAllFindsBusy(t *testing.T) {
	port := listen(t, "tcp4", "127.0.0.1:0")
	if got := CheckAll(port); len(got) != 1 || got[0].Port != port {
		t.Fatalf("CheckAll = %v", got)
	}
}
