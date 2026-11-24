package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "f.txt")
	if err := WriteFileAtomic(p, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
}

func TestRetryStopsOnPermanentError(t *testing.T) {
	calls := 0
	perm := errors.New("permanent")
	if err := Retry(func() error { calls++; return perm }); err != perm || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRenameMissingSource(t *testing.T) {
	d := t.TempDir()
	if err := Rename(filepath.Join(d, "nope"), filepath.Join(d, "x")); err == nil {
		t.Fatal("expected error")
	}
}
