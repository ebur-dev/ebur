// Package provision prepares a runtime after it is installed, so the
// first `ebur start` is fast and nothing is configured by hand:
//
//   - php: generate php.ini and run PHP once. Antivirus scans new binaries
//     on first launch (about 35 s for php-cgi with Webroot, SRS §8); doing
//     it here hides the cost behind the install progress.
//   - nginx: run it once for the same reason.
//   - mariadb: create the data folder with a generated root password
//     (NFR-09) and write my.ini.
package provision

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/dbserver"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/php"
	"github.com/ebur-dev/ebur/internal/fsutil"
	"github.com/ebur-dev/ebur/internal/proc"
)

// Report is what Prepare did, for display.
type Report struct {
	Steps    []string // human-readable steps done
	Warnings []string // non-fatal problems, e.g. an extension failed to load
}

// Prepare runs the post-install steps for one runtime version. say is
// called before slow steps so the caller can show progress.
func Prepare(ctx context.Context, root paths.Root, runtime, version string, say func(string)) (Report, error) {
	if say == nil {
		say = func(string) {}
	}
	dir := root.RuntimeDir(runtime, version)
	switch runtime {
	case "php":
		return preparePHP(ctx, root, dir, version, say)
	case "nginx":
		say("Running Nginx once so the first start is fast...")
		if out, err := run(ctx, filepath.Join(dir, proc.Exe("nginx")), "-v"); err != nil {
			return Report{}, fmt.Errorf("nginx does not run: %v: %s", err, out)
		}
		return Report{Steps: []string{"nginx runs"}}, nil
	case "mariadb":
		return prepareMariaDB(ctx, root, dir, version, say)
	case "mysql":
		return prepareMySQL(ctx, root, dir, version, say)
	case "composer":
		// A wrapper so `composer` works as a command. It uses whichever PHP
		// is first on PATH, which in Ebur's terminal is the active one.
		shim := "@echo off\r\nphp \"%~dp0composer.phar\" %*\r\n"
		if err := fsutil.WriteFileAtomic(filepath.Join(dir, "composer.bat"), []byte(shim), 0o755); err != nil {
			return Report{}, err
		}
		return Report{Steps: []string{"`composer` command available in Ebur's terminal"}}, nil
	}
	return Report{}, nil
}

func run(ctx context.Context, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = filepath.Dir(exe)
	proc.Hide(cmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func preparePHP(ctx context.Context, root paths.Root, dir, version string, say func(string)) (Report, error) {
	var rep Report
	ini, err := php.EnsureINI(root.Etc(), dir, version, filepath.Join(root.Data(), "tmp"))
	if err != nil {
		return rep, err
	}
	rep.Steps = append(rep.Steps, "php.ini at "+ini)
	say("Preparing PHP for first use (antivirus may scan it; this can take a minute)...")
	seen := map[string]bool{}
	for _, exe := range []string{"php-cgi", "php"} {
		out, err := run(ctx, filepath.Join(dir, proc.Exe(exe)), "-c", ini, "-m")
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && uint32(ee.ExitCode()) == statusDLLNotFound {
				return rep, fmt.Errorf("PHP %s needs the Microsoft Visual C++ Redistributable (x64), which is not installed. Install it from https://aka.ms/vs/17/release/vc_redist.x64.exe, then run this again", version)
			}
			return rep, fmt.Errorf("%s does not run: %v: %s", exe, err, lastLines(out, 3))
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "Unable to load") || strings.Contains(l, "PHP Warning") {
				if l = strings.TrimSpace(l); !seen[l] {
					seen[l] = true
					rep.Warnings = append(rep.Warnings, l)
				}
			}
		}
	}
	rep.Steps = append(rep.Steps, "php and php-cgi run with the generated php.ini")
	return rep, nil
}

// statusDLLNotFound (0xC0000135) is the exit code when Windows cannot load
// a program's DLLs, e.g. a PHP build whose Visual C++ runtime is missing.
const statusDLLNotFound = 0xC0000135

// Series returns "12.3" for "12.3.4": data folders are shared by patch
// releases of one series, so a patch update keeps the databases.
func Series(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// MariaDBPaths returns the data folder and my.ini for a version.
func MariaDBPaths(root paths.Root, version string) (dataDir, myIni string) {
	s := Series(version)
	return filepath.Join(root.Data(), "mariadb", s), filepath.Join(root.Etc(), "mariadb", s, "my.ini")
}

// Mailpit's ports (TOOL-03): PHP sends mail to the SMTP port, and
// mail.<tld> forwards to the UI port. Both listen on 127.0.0.1 only.
const (
	MailSMTPPort = 1025
	MailUIPort   = 8025
	// RedisPort is Redis's usual port, on 127.0.0.1 only (SVC-05).
	RedisPort = 6379
)

// PHPRedisDLL returns the phpredis extension for a PHP version, if
// installed: phpredis builds are stored per PHP series (bin\phpredis\8.5).
func PHPRedisDLL(root paths.Root, phpVersion string) (string, bool) {
	p := filepath.Join(root.RuntimeDir("phpredis", PHPSeries(phpVersion)), "php_redis.dll")
	_, err := os.Stat(p)
	return p, err == nil
}

// PHPSeries returns "8.5" for "8.5.13".
func PHPSeries(v string) string {
	p := strings.SplitN(v, ".", 3)
	if len(p) < 2 {
		return v
	}
	return p[0] + "." + p[1]
}

// WriteMyIni (re)writes my.ini for a version with the given port. The
// stack calls it on every start, so a db_port change needs no re-setup.
func WriteMyIni(root paths.Root, version string, port int) (string, error) {
	dataDir, myIni := MariaDBPaths(root, version)
	return myIni, writeMyIni(myIni, dataDir, port)
}

func prepareMariaDB(ctx context.Context, root paths.Root, dir, version string, say func(string)) (Report, error) {
	var rep Report
	cfg, err := config.Load(root.Config())
	if err != nil {
		return rep, err
	}
	changed := false
	if cfg.DBRootPassword == "" {
		pw, err := password(20)
		if err != nil {
			return rep, err
		}
		cfg.DBRootPassword, changed = pw, true
		rep.Steps = append(rep.Steps, "generated a database root password (see `ebur db`)")
	}
	// SVC-01: MariaDB added next to a MySQL that already holds its port
	// (e.g. MySQL chosen in the wizard took 3306) gets a free port instead.
	if _, mysqlOK := dbserver.Get(root, cfg, dbserver.MySQL); mysqlOK && cfg.MySQLPort == cfg.DBPort {
		cfg.DBPort, changed = PickMySQLPort(cfg.MySQLPort), true
		rep.Steps = append(rep.Steps, fmt.Sprintf("MariaDB will use port %d (MySQL uses %d)", cfg.DBPort, cfg.MySQLPort))
	}
	if changed {
		if err := config.Save(root.Config(), cfg); err != nil {
			return rep, err
		}
	}
	if freed, err := TrimMariaDB(dir); err != nil {
		rep.Warnings = append(rep.Warnings, "could not remove MariaDB's debug files: "+err.Error())
	} else if freed > 0 {
		rep.Steps = append(rep.Steps, fmt.Sprintf("removed %d MB of MariaDB debug and developer files", freed>>20))
	}
	dataDir, _ := MariaDBPaths(root, version)
	if _, err := WriteMyIni(root, version, cfg.DBPort); err != nil {
		return rep, err
	}
	if _, err := os.Stat(filepath.Join(dataDir, "mysql")); err == nil {
		rep.Steps = append(rep.Steps, "reusing existing databases in "+dataDir)
		return rep, nil
	}
	say("Creating the database data folder (first run can take a minute under antivirus)...")
	if err := os.MkdirAll(filepath.Dir(dataDir), 0o755); err != nil {
		return rep, err
	}
	// mariadb-install-db only accepts the password as an argument; it runs
	// once, locally, for a password that only guards a loopback-only server.
	out, err := run(ctx, filepath.Join(dir, "bin", proc.Exe("mariadb-install-db")),
		"--datadir="+dataDir, "--password="+cfg.DBRootPassword, fmt.Sprintf("--port=%d", cfg.DBPort))
	if err != nil {
		os.RemoveAll(dataDir) // never leave a half-initialised data folder
		return rep, fmt.Errorf("mariadb-install-db failed: %v: %s", err, lastLines(out, 5))
	}
	rep.Steps = append(rep.Steps, "databases created in "+dataDir)
	return rep, nil
}

// TrimMariaDB removes files from an installed MariaDB that only matter
// for debugging or compiling against it: *.pdb debug symbols, *.lib
// import libraries and the include\ headers (about 130 MB). Programs,
// DLLs, plugins and share\ stay. It returns the bytes freed and is safe
// to run again.
func TrimMariaDB(dir string) (int64, error) {
	var freed int64
	var errs []error
	remove := func(path string, size int64) {
		if err := os.Remove(path); err != nil {
			errs = append(errs, err)
		} else {
			freed += size
		}
	}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return nil // a link or junction: never follow or delete through it
		}
		if d.IsDir() {
			if path != dir && strings.EqualFold(d.Name(), "include") && strings.EqualFold(filepath.Dir(path), dir) {
				filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() {
						if fi, err := e.Info(); err == nil {
							remove(p, fi.Size())
						}
					}
					return nil
				})
				if err := os.RemoveAll(path); err != nil {
					errs = append(errs, err)
				}
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".pdb", ".lib":
			if fi, err := d.Info(); err == nil {
				remove(path, fi.Size())
			}
		}
		return nil
	})
	return freed, errors.Join(append(errs, err)...)
}

// writeMyIni writes the server config Ebur starts MariaDB with. Errors go
// to the console so the supervisor captures them (SVC-04).
func writeMyIni(path, dataDir string, port int) error {
	ini := fmt.Sprintf(`# Generated by Ebur.
[mysqld]
datadir="%s"
port=%d
bind-address=127.0.0.1
character-set-server=utf8mb4
collation-server=utf8mb4_unicode_ci
innodb_buffer_pool_size=128M
# 48 MB of redo log is plenty for development (the default is 96 MB);
# MariaDB 10.8+ resizes an existing log on start.
innodb_log_file_size=48M
max_allowed_packet=256M

[client]
port=%d
host=127.0.0.1
default-character-set=utf8mb4
`, filepath.ToSlash(dataDir), port, port)
	return fsutil.WriteFileAtomic(path, []byte(ini), 0o644)
}

const pwChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// password returns a random password without look-alike characters or
// symbols that need quoting in .env files.
func password(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		k, err := rand.Int(rand.Reader, big.NewInt(int64(len(pwChars))))
		if err != nil {
			return "", err
		}
		b[i] = pwChars[k.Int64()]
	}
	return string(b), nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
