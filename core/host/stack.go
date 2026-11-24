package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ebur/ebur/core/certs"
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
	httpsPort   = 443
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
	scanDir := filepath.Join(root.Etc(), "php", phpVer, "conf.d")
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		return Stack{}, err
	}
	pool := php.Pool{Version: phpVer, Dir: phpDir, INI: ini, BasePort: phpBasePort, Size: cfg.PHPPoolSize, LogDir: root.Logs(), ScanDir: scanDir}
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
	nc := nginxconf.Config{
		NginxDir: nginxDir,
		LogDir:   root.Logs(),
		TempDir:  tempDir,
		PIDFile:  filepath.Join(root.Data(), "nginx.pid"),
		Listen:   listen,
		Sites:    served,
		Pool:     pool,
	}
	if err := setupTLS(root, cfg, &nc, served, scanDir); err != nil {
		return Stack{}, err
	}
	if nc.TLSListen != "" {
		ports = append(ports, httpsPort)
		info.HTTPS = true
	}
	conf := nginxconf.Render(nc)
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
			// Fast stop: a graceful "quit" waits for open connections (a
			// browser's keep-alive, a slow request) and made restarts take
			// 10 s; a local dev server has nothing to drain.
			cmd := exec.CommandContext(ctx, nginxExe, append(nginxArgs, "-s", "stop")...)
			cmd.Dir = nginxDir
			proc.Hide(cmd)
			return cmd.Run()
		},
	})

	info.Listen = listen
	return Stack{Specs: specs, Ports: ports, Info: info}, nil
}

// setupTLS enables HTTPS once the user has created the local CA with
// `ebur ssl install` (SSL-01 consent). It issues or renews a certificate
// per served host (SSL-02), refreshes the CRL, and points PHP at a bundle
// of public roots plus the Ebur CA (SSL-04). Without a CA it removes the
// PHP CA settings so nothing points at missing files.
func setupTLS(root paths.Root, cfg config.Settings, nc *nginxconf.Config, served []sites.Site, scanDir string) error {
	sslIni := filepath.Join(scanDir, "ebur-ssl.ini")
	dir := certs.Dir(root.SSL())
	if !dir.Exists() {
		os.Remove(sslIni)
		return nil
	}
	ca, err := dir.LoadCA()
	if err != nil {
		return &hinted{"local CA is damaged: " + err.Error(), "Run `ebur ssl uninstall`, then `ebur ssl install`."}
	}
	crl, err := dir.EnsureCRL(ca)
	if err != nil {
		return err
	}
	crlURL := fmt.Sprintf("http://%s:%d%s", listenAddr, httpPort, certs.CRLPath)
	nc.TLS = map[string]nginxconf.TLSPair{}
	for _, s := range served {
		c, k, err := dir.EnsureSite(ca, s.Host, crlURL)
		if err != nil {
			return &hinted{err.Error(), "Run `ebur ssl uninstall` and `ebur ssl install` to make a CA for the current TLD."}
		}
		nc.TLS[s.Host] = nginxconf.TLSPair{Cert: c, Key: k}
	}
	nc.TLSListen = listenAddr + ":" + strconv.Itoa(httpsPort)
	nc.CRLFile, nc.CRLPath = crl, certs.CRLPath

	var roots []byte
	if v, _ := optional(root, cfg, "cacert"); v != "" {
		roots, _ = os.ReadFile(filepath.Join(root.RuntimeDir("cacert", v), "cacert.pem"))
	}
	bundle, err := dir.WriteBundle(ca, roots)
	if err != nil {
		return err
	}
	b := filepath.ToSlash(bundle)
	ini := fmt.Sprintf("; Generated by Ebur (SSL-04): trust public roots and the Ebur CA.\ncurl.cainfo = \"%s\"\nopenssl.cafile = \"%s\"\n", b, b)
	return fsutil.WriteFileAtomic(sslIni, []byte(ini), 0o644)
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
