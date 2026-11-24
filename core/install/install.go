// Package install is the one install flow shared by the CLI and the tray
// app: resolve a version from the manifest, download and verify it,
// prepare it (core/provision), and make it active if nothing else is.
package install

import (
	"context"
	"fmt"
	goruntime "runtime"

	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/manifest"
	"github.com/ebur/ebur/core/paths"
	"github.com/ebur/ebur/core/provision"
	"github.com/ebur/ebur/core/runtimes"
	manifestdata "github.com/ebur/ebur/manifest"
)

// Manifest returns the manifest built into this binary.
func Manifest() (*manifest.Manifest, error) { return manifest.Parse(manifestdata.JSON) }

// Resolve picks a release for this machine. Official PHP and MariaDB
// builds are x64 only, so ARM64 falls back to x64 under emulation.
func Resolve(m *manifest.Manifest, name, want string) (manifest.Release, error) {
	if goruntime.GOARCH == "arm64" {
		if rel, err := m.Resolve(name, want, "arm64"); err == nil {
			return rel, nil
		}
	}
	return m.Resolve(name, want, "x64")
}

// Result describes what Runtime did.
type Result struct {
	Version    string
	Downloaded bool // false if it was already installed
	Activated  bool // became the active version
	Report     provision.Report
}

// Runtime installs name (want may be "", "8.4" or "8.4.28"). progress
// receives download bytes; say receives messages before slow steps. Both
// may be nil.
func Runtime(ctx context.Context, root paths.Root, name, want string, progress runtimes.Progress, say func(string)) (Result, error) {
	var res Result
	m, err := Manifest()
	if err != nil {
		return res, err
	}
	rel, err := Resolve(m, name, want)
	if err != nil {
		return res, err
	}
	res.Version = rel.Version
	installed, err := runtimes.Installed(root, name)
	if err != nil {
		return res, err
	}
	if !contains(installed, rel.Version) {
		in := &runtimes.Installer{Root: root}
		if _, err := in.Install(ctx, name, rel, progress); err != nil {
			return res, err
		}
		res.Downloaded = true
	}
	rep, err := provision.Prepare(ctx, root, name, rel.Version, say)
	res.Report = rep
	if err != nil {
		return res, fmt.Errorf("prepare %s %s: %w", name, rel.Version, err)
	}
	cfg, err := config.Load(root.Config())
	if err != nil {
		return res, err
	}
	if cfg.Active[name] == "" {
		cfg.Active[name] = rel.Version
		if err := config.Save(root.Config(), cfg); err != nil {
			return res, err
		}
		res.Activated = true
	}
	// Composer comes with PHP (PHP-07). A failure here does not undo the
	// PHP install; it is reported and `ebur install composer` retries.
	if name == "php" {
		if inst, _ := runtimes.Installed(root, "composer"); len(inst) == 0 {
			if say != nil {
				say("Installing Composer, which comes with PHP...")
			}
			if _, err := Runtime(ctx, root, "composer", "", nil, nil); err != nil {
				res.Report.Warnings = append(res.Report.Warnings, "Composer was not installed: "+err.Error()+" (run `ebur install composer`)")
			} else {
				res.Report.Steps = append(res.Report.Steps, "Composer installed")
			}
		}
	}
	return res, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
