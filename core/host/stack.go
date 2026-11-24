package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/nginxconf"
	"github.com/ebur/ebur/core/paths"
	"github.com/ebur/ebur/core/php"
	"github.com/ebur/ebur/core/provision"
	"github.com/ebur/ebur/core/runtimes"
	"github.com/ebur/ebur/core/sites"
	"github.com/ebur/ebur/core/supervisor"
	"github.com/ebur/ebur/internal/fsutil"
	"github.com/ebur/ebur/internal/proc"
)

const (
	listenAddr  = "127.0.0.1" // NFR-07
	httpPort    = 80
	phpBasePort = 9100
)

// activeOrNewest returns the configured version if installed, else the
// newest installed one.
func activeOrNewest(root paths.Root, cfg config.Settings, runtime string) (string, error) {
	installed, err := runtimes.Installed(root, runtime)
	if err != nil {
		return "", err
	}
	if v := cfg.Active[runtime]; v != "" {
		for _, iv := range installed {
			if iv == v {
				return v, nil
			}
		}
		return "", &hinted{
			fmt.Sprintf("%s %s is set as active but is not installed", runtime, v),
			fmt.Sprintf("Run `ebur %s install %s`, or pick an installed version.", runtime, v),
		}
	}
	if len(installed) == 0 {
		example := "ebur install " + runtime
		if runtime == "php" {
			example = "ebur php install 8.4"
		}
		return "", &hinted{fmt.Sprintf("%s is not installed", runtime), "Run `" + example + "`."}
	}
	return installed[0], nil
}

// optional is activeOrNewest for runtimes the stack can run without
// (MariaDB, Adminer): it returns "" when none is installed.
func optional(root paths.Root, cfg config.Settings, runtime string) (string, error) {
	if installed, _ := runtimes.Installed(root, runtime); len(installed) == 0 && cfg.Active[runtime] == "" {
		return "", nil
	}
	return activeOrNewest(root, cfg, runtime)
}

// reserved folder names under www\ that Ebur serves itself.
var reserved = map[string]string{"db": "Adminer"}

// mariadbSpec runs mariadbd with Ebur's my.ini, setting up the data folder
// first if it is missing.
func mariadbSpec(root paths.Root, version, password string, port int) (supervisor.Spec, error) {
	dir := root.RuntimeDir("mariadb", version)
	dataDir, _ := provision.MariaDBPaths(root, version)
	myIni, err := provision.WriteMyIni(root, version, port)
	if err != nil {
		return supervisor.Spec{}, err
	}
	if _, err := os.Stat(filepath.Join(dataDir, "mysql")); err != nil {
		if _, err := provision.Prepare(context.Background(), root, "mariadb", version, nil); err != nil {
			return supervisor.Spec{}, err
		}
		cfg, _ := config.Load(root.Config())
		password = cfg.DBRootPassword
	}
	defaults := "--defaults-file=" + filepath.ToSlash(myIni) // must be the first option
	admin := filepath.Join(dir, "bin", proc.Exe("mariadb-admin"))
	return supervisor.Spec{
		Name:    "mariadb",
		Path:    filepath.Join(dir, "bin", proc.Exe("mariadbd")),
		Args:    []string{defaults, "--console"},
		Dir:     dir,
		LogFile: filepath.Join(root.Logs(), "mariadb.log"),
		Stop: func(ctx context.Context, _ int) error {
			// A clean shutdown avoids InnoDB crash recovery on next start.
			cmd := exec.CommandContext(ctx, admin, defaults, "--user=root", "shutdown")
			cmd.Env = append(os.Environ(), "MYSQL_PWD="+password) // keeps it off the command line
			proc.Hide(cmd)
			return cmd.Run()
		},
	}, nil
}

// BuildStack is the production stack: Nginx in front of one php-cgi pool
// for the active PHP version, plus MariaDB and Adminer when installed.
func BuildStack(root paths.Root, cfg config.Settings) (Stack, error) {
	phpVer, err := activeOrNewest(root, cfg, "php")
	if err != nil {
		return Stack{}, err
	}
	nginxVer, err := activeOrNewest(root, cfg, "nginx")
	if err != nil {
		return Stack{}, err
	}
	if err := root.Ensure(); err != nil {
		return Stack{}, err
	}
	phpDir := root.RuntimeDir("php", phpVer)
	nginxDir := root.RuntimeDir("nginx", nginxVer)

	ini, err := php.EnsureINI(root.Etc(), phpDir, phpVer, filepath.Join(root.Data(), "tmp"))
	if err != nil {
		return Stack{}, err
	}
	pool := php.Pool{Version: phpVer, Dir: phpDir, INI: ini, BasePort: phpBasePort, Size: cfg.PHPPoolSize, LogDir: root.Logs()}
	specs, err := pool.Specs()
	if err != nil {
		return Stack{}, err
	}

	scanned, skipped, err := sites.Scan(root.WWW(), cfg.TLD)
	if err != nil {
		return Stack{}, err
	}
	var found []sites.Site
	for _, s := range scanned {
		if tool, ok := reserved[s.Name]; ok {
			skipped = append(skipped, sites.Skipped{Folder: s.Name, Reason: fmt.Sprintf("%s is reserved for %s; rename the folder", s.Host, tool)})
			continue
		}
		found = append(found, s)
	}
	ports := append([]int{httpPort}, pool.Ports()...)
	info := Info{PHP: phpVer, Nginx: nginxVer, Sites: found, Skipped: skipped}

	dbVer, err := optional(root, cfg, "mariadb")
	if err != nil {
		return Stack{}, err
	}
	if dbVer != "" {
		s, err := mariadbSpec(root, dbVer, cfg.DBRootPassword, cfg.DBPort)
		if err != nil {
			return Stack{}, err
		}
		specs = append(specs, s)
		ports = append(ports, cfg.DBPort)
		info.MariaDB = dbVer
	}
	served := found
	adminerVer, err := optional(root, cfg, "adminer")
	if err != nil {
		return Stack{}, err
	}
	if adminerVer != "" { // TOOL-01: Adminer is served like any other site
		tool := sites.Site{Name: "db", Host: "db." + cfg.TLD, DocRoot: root.RuntimeDir("adminer", adminerVer)}
		served = append(append([]sites.Site(nil), found...), tool)
		info.Adminer = "http://" + tool.Host
	}

	listen := listenAddr + ":" + strconv.Itoa(httpPort)
	tempDir := filepath.Join(root.Data(), "nginx-temp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return Stack{}, err
	}
	conf := nginxconf.Render(nginxconf.Config{
		NginxDir: nginxDir,
		LogDir:   root.Logs(),
		TempDir:  tempDir,
		PIDFile:  filepath.Join(root.Data(), "nginx.pid"),
		Listen:   listen,
		Sites:    served,
		Pool:     pool,
	})
	nginxExe := filepath.Join(nginxDir, "nginx.exe")
	// nginx resolves its default log path against the prefix before it
	// reads the config, so the prefix is the Ebur root (which has logs\).
	prefix := filepath.ToSlash(root.Dir()) + "/"
	confPath := filepath.Join(root.Etc(), "nginx", "nginx.conf")
	if err := writeTested(confPath, conf, nginxExe, prefix); err != nil {
		return Stack{}, err
	}
	nginxArgs := []string{"-p", prefix, "-c", filepath.ToSlash(confPath)}
	specs = append(specs, supervisor.Spec{
		Name:    "nginx",
		Path:    nginxExe,
		Args:    nginxArgs,
		Dir:     nginxDir,
		LogFile: filepath.Join(root.Logs(), "nginx-console.log"),
		Stop: func(ctx context.Context, _ int) error {
			cmd := exec.CommandContext(ctx, nginxExe, append(nginxArgs, "-s", "quit")...)
			cmd.Dir = nginxDir
			proc.Hide(cmd)
			return cmd.Run()
		},
	})

	info.Listen = listen
	return Stack{Specs: specs, Ports: ports, Info: info}, nil
}

// writeTested writes conf next to path, checks it with `nginx -t`, and
// only then replaces path. A bad config never replaces a working one
// (NFR-10).
func writeTested(path, conf, nginxExe, prefix string) error {
	candidate := path + ".new"
	if err := fsutil.WriteFileAtomic(candidate, []byte(conf), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(nginxExe, "-t", "-p", prefix, "-c", filepath.ToSlash(candidate))
	proc.Hide(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &hinted{
			fmt.Sprintf("generated Nginx config failed its test; the previous config was kept: %s", lastLine(string(out))),
			"Check templates\\ for recent edits, then run `ebur doctor`. The rejected config is at " + candidate,
		}
	}
	return fsutil.Rename(candidate, path)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
