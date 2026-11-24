// Package fsutil holds file helpers that cope with Windows realities:
// antivirus and indexers briefly lock new files, so renames can fail
// with "access denied" or "sharing violation" for a moment.
package fsutil

import (
	"os"
	"path/filepath"
	"time"
)

// retryWindow is how long transient lock errors are retried.
var retryWindow = 5 * time.Second

// Retry runs f until it succeeds, returns a non-transient error, or the
// retry window passes.
func Retry(f func() error) error {
	delay := 20 * time.Millisecond
	deadline := time.Now().Add(retryWindow)
	for {
		err := f()
		if err == nil || !isTransient(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
		if delay < 500*time.Millisecond {
			delay *= 2
		}
	}
}

// Rename is os.Rename with retries for transient Windows lock errors.
func Rename(from, to string) error {
	return Retry(func() error { return os.Rename(from, to) })
}

// WriteFileAtomic writes data to a temp file in the same folder, flushes
// it to disk, then renames it over path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	if err := Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}
