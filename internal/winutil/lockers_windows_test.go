package winutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileLockersFindsThisProcess(t *testing.T) {
	p := filepath.Join(t.TempDir(), "held.txt")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := FileLockers(p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range got {
		if l.PID == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Fatalf("this process not listed: %+v", got)
	}
	f.Close()
	if got, err := FileLockers(p); err != nil || len(got) != 0 {
		t.Fatalf("after close: %+v %v", got, err)
	}
}
