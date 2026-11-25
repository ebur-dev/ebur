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
	if !strings.Contains(unknown.Error(), "another program") || strings.Contains(unknown.Hint(), "Settings") {
		t.Error(unknown.Error(), unknown.Hint())
	}
}

// Other local stacks are named, with how to stop them, and no hint points
// at a setting that does not exist (web ports are fixed at 80 and 443).
func TestHintsNameOtherStacks(t *testing.T) {
	cases := []struct {
		owner Owner
		want  string
	}{
		{Owner{PID: 11376, Name: "httpd.exe", Path: `C:\xampp\apache\bin\httpd.exe`}, "XAMPP's Apache web server"},
		{Owner{PID: 2, Name: "mysqld.exe", Path: `C:\xampp\mysql\bin\mysqld.exe`}, "XAMPP's database"},
		{Owner{PID: 3, Name: "httpd.exe", Path: `C:\wamp64\bin\apache\apache2.4.62\bin\httpd.exe`}, "WampServer"},
		{Owner{PID: 4 + 1, Name: "httpd.exe", Path: `C:\laragon\bin\apache\httpd-2.4\bin\httpd.exe`}, "Laragon"},
		{Owner{PID: 6, Name: "httpd.exe", Path: `C:\Apache24\bin\httpd.exe`}, "Apache2.4"},
		{Owner{PID: 51488, Name: "wslrelay.exe", Path: `C:\Program Files\WSL\wslrelay.exe`}, "inside WSL"},
		{Owner{PID: 9, Name: "com.docker.backend.exe", Path: `C:\Program Files\Docker\Docker\resources\com.docker.backend.exe`}, "Docker container"},
		{Owner{PID: 7, Name: "node.exe", Path: `C:\Program Files\nodejs\node.exe`}, `Close C:\Program Files\nodejs\node.exe`},
		{Owner{PID: 8, Name: "node.exe"}, "Close node.exe"},
	}
	for _, c := range cases {
		h := (&Conflict{Port: 80, Owner: c.owner}).Hint()
		if !strings.Contains(h, c.want) || strings.Contains(h, "Settings") {
			t.Errorf("%s: %s", c.owner.Path, h)
		}
	}
}

func TestCheckAllFindsBusy(t *testing.T) {
	port := listen(t, "tcp4", "127.0.0.1:0")
	if got := CheckAll(port); len(got) != 1 || got[0].Port != port {
		t.Fatalf("CheckAll = %v", got)
	}
}
