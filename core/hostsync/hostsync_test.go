package hostsync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebur-dev/ebur/core/paths"
)

func TestWanted(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	os.MkdirAll(filepath.Join(root.WWW(), "shop", "public"), 0o755)
	os.MkdirAll(filepath.Join(root.WWW(), "blog"), 0o755)
	os.MkdirAll(root.RuntimeDir("adminer", "6.3.1"), 0o755)
	got, err := Wanted(root)
	if err != nil || strings.Join(got, ",") != "blog.test,db.test,shop.test" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestSyncRejectsBadNamesBeforePrompting(t *testing.T) {
	if _, err := Sync([]string{"bank.com"}); err == nil || !strings.Contains(err.Error(), "bank.com") {
		t.Fatalf("got %v", err)
	}
}

// A blocked or declined automatic attempt must not prompt again on every
// start for the same names.
func TestAutoSyncSkipsAfterFailure(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	root.Ensure()
	want := []string{"zz-ebur-test-never-mapped.test"}
	os.WriteFile(statePath(root), []byte(`{"want":["zz-ebur-test-never-mapped.test"],"reason":"security software blocked the change"}`), 0o644)
	_, err := AutoSync(root, want)
	var skipped *ErrSkipped
	if !errors.As(err, &skipped) || !strings.Contains(skipped.Reason, "security software") {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
	// Different names: it would try again (here it stops at the missing
	// helper, before any prompt).
	if _, err := AutoSync(root, []string{"other-name.test"}); errors.As(err, &skipped) {
		t.Fatal("skipped although the names changed")
	}
	Forget(root)
	if _, err := os.Stat(statePath(root)); err == nil {
		t.Fatal("Forget left the record")
	}
}

func TestHelperPathMissing(t *testing.T) {
	if _, err := HelperPath(); err == nil || !strings.Contains(err.Error(), HelperName) {
		t.Fatalf("test binary has no helper next to it; got %v", err)
	}
}
