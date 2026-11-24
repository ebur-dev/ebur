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
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/paths"
	"github.com/ebur/ebur/core/php"
	"github.com/ebur/ebur/internal/fsutil"
	"github.com/ebur/ebur/internal/proc"
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
	if cfg.DBRootPassword == "" {
		pw, err := password(20)
		if err != nil {
			return rep, err
		}
		cfg.DBRootPassword = pw
		if err := config.Save(root.Config(), cfg); err != nil {
			return rep, err
		}
		rep.Steps = append(rep.Steps, "generated a database root password (see `ebur db`)")
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
