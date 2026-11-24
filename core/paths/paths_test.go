package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewMakesAbsolute(t *testing.T) {
	r, err := New(".")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(r.Dir()) {
		t.Fatalf("not absolute: %s", r.Dir())
	}
	if _, err := New("  "); err == nil {
		t.Fatal("empty root accepted")
	}
}

func TestFindUsesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvRoot, dir)
	r, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if r.Dir() != filepath.Clean(dir) {
		t.Fatalf("got %s want %s", r.Dir(), dir)
	}
}

func TestFindFallsBackToExecutable(t *testing.T) {
	t.Setenv(EnvRoot, "")
	r, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if r.Dir() == "" {
		t.Fatal("empty root")
	}
}

func TestLayoutAndEnsure(t *testing.T) {
	r, _ := New(t.TempDir())
	if err := r.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{r.Bin(), r.Templates(), r.Etc(), r.Data(), r.Downloads(), r.SSL(), r.Logs(), r.WWW()} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("missing dir %s", d)
		}
	}
	if got, want := r.RuntimeDir("php", "8.4.28"), filepath.Join(r.Dir(), "bin", "php", "8.4.28"); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if filepath.Base(r.Config()) != "ebur.json" {
		t.Fatal(r.Config())
	}
}
