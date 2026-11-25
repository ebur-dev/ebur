//go:build integration

// Downloads real runtimes from the repo manifest. Run with:
//
//	go test -tags integration ./core/runtimes
package runtimes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
)

func TestInstallFromRepoManifest(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("manifest lists Windows builds")
	}
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "manifest", "runtimes.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := paths.New(filepath.Join(t.TempDir(), "ebur root with spaces"))
	in := &Installer{Root: root}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	checks := map[string][]string{
		"nginx": {"nginx.exe", "-v"},
		"php":   {"php.exe", "-v"},
	}
	for name, cmd := range checks {
		rel, err := m.Resolve(name, "", "x64")
		if err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		dir, err := in.Install(ctx, name, rel, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, err := exec.Command(filepath.Join(dir, cmd[0]), cmd[1:]...).CombinedOutput()
		if err != nil || !strings.Contains(string(out), rel.Version) {
			t.Fatalf("%s %s: %v\n%s", name, rel.Version, err, out)
		}
		t.Logf("%s %s installed and runs (%v)", name, rel.Version, time.Since(t0).Round(time.Millisecond))
	}
}
