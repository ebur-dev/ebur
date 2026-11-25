// Package install is the one install flow shared by the CLI and the tray
// app: resolve a version from the manifest, download and verify it,
// prepare it (core/provision), and make it active if nothing else is.
package install

import (
	"context"
	"fmt"
	goruntime "runtime"
	"time"

	"github.com/ebur-dev/ebur/core/catalog"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/runtimes"
)

// Manifest returns the manifest in use: the one built into this binary,
// or a newer signed copy Ebur fetched (core/catalog).
func Manifest() (*manifest.Manifest, error) {
	m, _, err := catalog.Load() // built-in, or a newer signed download (SEC-02)
	return m, err
}

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

// Support describes a version's security support (SEC-01).
type Support struct {
	EOL   string `json:"eol"`   // YYYY-MM-DD, "" if unknown
	Ended bool   `json:"ended"` // past end of life: legacy
}

// SupportOf looks a runtime version up in the built-in manifest.
func SupportOf(runtime, version string) Support {
	m, err := Manifest()
	if err != nil {
		return Support{}
	}
	r, ok := m.Find(runtime, version)
	if !ok {
		return Support{}
	}
	return Support{EOL: r.EOL, Ended: r.EndOfLife(time.Now())}
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
		in := &runtimes.Installer{Root: root, Say: say} // RUN-05: resume and retry notices
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
	if cfg.Active[name] == "" && name != "phpredis" { // phpredis is per PHP version, not switched
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
	// SVC-05: Redis brings the phpredis extension for every installed PHP
	// version, and a PHP installed later gets it too.
	switch name {
	case "redis":
		phps, _ := runtimes.Installed(root, "php")
		for _, v := range phps {
			res.Report.Warnings = append(res.Report.Warnings, installPHPRedis(ctx, root, v, &res.Report)...)
		}
	case "php":
		if r, _ := runtimes.Installed(root, "redis"); len(r) > 0 {
			res.Report.Warnings = append(res.Report.Warnings, installPHPRedis(ctx, root, rel.Version, &res.Report)...)
		}
	}
	return res, nil
}

// installPHPRedis installs the phpredis build for phpVersion's series,
// reporting a warning (not an error) when none exists yet.
func installPHPRedis(ctx context.Context, root paths.Root, phpVersion string, rep *provision.Report) []string {
	series := provision.PHPSeries(phpVersion)
	if _, ok := provision.PHPRedisDLL(root, phpVersion); ok {
		return nil
	}
	m, err := Manifest()
	if err != nil {
		return []string{err.Error()}
	}
	if _, err := Resolve(m, "phpredis", series); err != nil {
		return []string{fmt.Sprintf("no phpredis extension exists yet for PHP %s; Laravel can use the pure-PHP client instead: `composer require predis/predis` and REDIS_CLIENT=predis in .env", series)}
	}
	if _, err := Runtime(ctx, root, "phpredis", series, nil, nil); err != nil {
		return []string{fmt.Sprintf("phpredis for PHP %s was not installed: %v (run `ebur install phpredis %s`)", series, err, series)}
	}
	rep.Steps = append(rep.Steps, "phpredis extension for PHP "+series)
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
