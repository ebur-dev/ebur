package hostsfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Simulates security software (Webroot's WRSVC on the dev machine) holding
// the hosts file open without sharing writes.
func TestApplyReportsLockedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(p, []byte(sample), 0o644)
	name, _ := windows.UTF16PtrFromString(p)
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)

	old := retryWindow
	retryWindow = 300 * time.Millisecond
	defer func() { retryWindow = old }()

	err = Apply(p, []string{"shop.test"})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != sample {
		t.Fatal("file changed despite the lock")
	}
}
