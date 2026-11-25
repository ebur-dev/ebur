// Package doctor checks an Ebur install and suggests fixes (CLI-03).
// Every finding names the cause and a next step (NFR-14).
package doctor

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ebur-dev/ebur/core/certs"
	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/ports"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/provision"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/sites"
	"github.com/ebur-dev/ebur/internal/proc"
	"github.com/ebur-dev/ebur/internal/winutil"
)

type Level string

const (
	OK   Level = "ok"
	Info Level = "info"
	Warn Level = "warn"
	Fail Level = "fail"
)

type Check struct {
	Name   string `json:"name"`
	Level  Level  `json:"level"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

// Options lets tests point at a fake hosts file.
type Options struct {
	HostsFile   string // default: the system hosts file
	HostRunning bool   // whether the Ebur host answered
}

// Run performs every check. It never changes anything.
func Run(root paths.Root, opt Options) []Check {
	var out []Check
	add := func(c Check) { out = append(out, c) }

	cfg, err := config.Load(root.Config())
	if err != nil {
		add(Check{"Settings", Fail, err.Error(), "Fix or delete ebur.json; Ebur recreates it with defaults."})
		cfg = config.Defaults()
	} else {
		add(Check{"Settings", OK, root.Config(), ""})
	}
	add(checkWritable(root))

	active := map[string]string{}
	for _, rt := range []struct {
		name     string
		required bool
		install  string
	}{
		{"php", true, "ebur php install 8.4"},
		{"nginx", true, "ebur install nginx"},
		{"mariadb", false, "ebur install mariadb"},
		{"adminer", false, "ebur install adminer"},
		{"composer", false, "ebur install composer"},
		{"mailpit", false, "ebur install mailpit"},
	} {
		c, v := checkRuntime(root, cfg, rt.name, rt.required, rt.install)
		add(c)
		active[rt.name] = v
	}
	if v := active["php"]; v != "" {
		add(checkPHPIni(root, v))
	}
	if v := active["nginx"]; v != "" {
		add(checkNginxConf(root, v))
	}
	if v := active["mariadb"]; v != "" {
		add(checkMariaDBData(root, v))
	}

	wanted := []int{80, 9100}
	if certs.Dir(root.SSL()).Exists() {
		wanted = append(wanted, 443)
	}
	if active["mailpit"] != "" {
		wanted = append(wanted, provision.MailSMTPPort, provision.MailUIPort)
	}
	if active["mariadb"] != "" {
		wanted = append(wanted, cfg.DBPort)
	}
	for _, p := range wanted {
		add(checkPort(root, p, opt.HostRunning))
	}

	found, skipped, _ := sites.Scan(root.WWW(), cfg.TLD)
	var hosts []string
	for _, s := range found {
		if s.Name != "db" {
			hosts = append(hosts, s.Host)
		}
	}
	if active["adminer"] != "" {
		hosts = append(hosts, "db."+cfg.TLD)
	}
	if active["mailpit"] != "" {
		hosts = append(hosts, "mail."+cfg.TLD)
	}
	hostsFile := opt.HostsFile
	if hostsFile == "" {
		hostsFile = filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
	}
	add(checkHosts(hostsFile, hosts))
	for _, s := range skipped {
		add(Check{"Site " + s.Folder, Warn, s.Reason, "Rename the folder in www\\."})
	}
	// PHP-04: a site asking for a PHP version that is not installed still
	// runs, on the default version; say so.
	if def := active["php"]; def != "" {
		installed, _ := runtimes.Installed(root, "php")
		for _, s := range found {
			if c := project.Resolve(s.Dir, installed, def); c.Problem != "" {
				add(Check{"Site " + s.Name + " PHP", Warn, "asks for PHP " + c.Want + " in " + c.Source, c.Problem})
			}
		}
	}

	if opt.HostRunning {
		add(Check{"Ebur host", OK, "running", ""})
	} else {
		add(Check{"Ebur host", Info, "not running", "Run `ebur start`."})
	}
	add(checkHTTPS(root, cfg))
	return out
}

func checkHTTPS(root paths.Root, cfg config.Settings) Check {
	ca, err := certs.Dir(root.SSL()).LoadCA()
	if errors.Is(err, certs.ErrNoCA) {
		return Check{"HTTPS", Info, "not set up; sites are served over http:// only", "Run `ebur ssl install`."}
	}
	if err != nil {
		return Check{"HTTPS", Fail, "local CA is damaged: " + err.Error(), "Run `ebur ssl uninstall`, then `ebur ssl install`."}
	}
	if !ca.Permits("site." + cfg.TLD) {
		return Check{"HTTPS", Warn, fmt.Sprintf("the CA may only sign %s, but the TLD is .%s", strings.Join(ca.Cert.PermittedDNSDomains, ", "), cfg.TLD),
			"Run `ebur ssl uninstall`, then `ebur ssl install` to make a CA for the current TLD."}
	}
	if trusted, err := ca.IsTrusted(); err != nil {
		return Check{"HTTPS", Info, "cannot tell whether the CA is trusted: " + err.Error(), ""}
	} else if !trusted {
		return Check{"HTTPS", Warn, "the Ebur CA exists but Windows does not trust it, so browsers will warn", "Run `ebur ssl install` and click Yes."}
	}
	return Check{"HTTPS", OK, "Ebur CA trusted (SHA-256 " + ca.Fingerprint()[:16] + "...)", ""}
}

func checkWritable(root paths.Root) Check {
	if err := root.Ensure(); err != nil {
		return Check{"Ebur folder", Fail, err.Error(), "Move Ebur to a folder you can write to, e.g. C:\\ebur."}
	}
	f, err := os.CreateTemp(root.Data(), ".doctor-*")
	if err != nil {
		return Check{"Ebur folder", Fail, "cannot write to " + root.Data(), "Move Ebur to a folder you can write to, e.g. C:\\ebur."}
	}
	f.Close()
	os.Remove(f.Name())
	return Check{"Ebur folder", OK, root.Dir(), ""}
}

func checkRuntime(root paths.Root, cfg config.Settings, name string, required bool, install string) (Check, string) {
	label := "Runtime " + name
	installed, err := runtimes.Installed(root, name)
	if err != nil {
		return Check{label, Fail, err.Error(), ""}, ""
	}
	if len(installed) == 0 {
		lvl := Info
		if required {
			lvl = Fail
		}
		return Check{label, lvl, "not installed", "Run `" + install + "`."}, ""
	}
	v := cfg.Active[name]
	if v == "" {
		v = installed[0]
	}
	for _, iv := range installed {
		if iv == v {
			return Check{label, OK, fmt.Sprintf("%s (installed: %s)", v, strings.Join(installed, ", ")), ""}, v
		}
	}
	return Check{label, Fail, fmt.Sprintf("active version %s is not installed", v),
		fmt.Sprintf("Run `%s` or switch to one of: %s", install, strings.Join(installed, ", "))}, ""
}

var extRe = regexp.MustCompile(`^\s*extension\s*=\s*"?([A-Za-z0-9_]+)"?\s*$`)

func checkPHPIni(root paths.Root, version string) Check {
	path := filepath.Join(root.Etc(), "php", version, "php.ini")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Check{"php.ini", Warn, "not created yet for PHP " + version, "It is created on the next `ebur start`."}
	} else if err != nil {
		return Check{"php.ini", Fail, err.Error(), ""}
	}
	defer f.Close()
	extDir := filepath.Join(root.RuntimeDir("php", version), "ext")
	var missing []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := extRe.FindStringSubmatch(sc.Text()); m != nil {
			if _, err := os.Stat(filepath.Join(extDir, "php_"+m[1]+".dll")); err != nil {
				missing = append(missing, m[1])
			}
		}
	}
	if len(missing) > 0 {
		return Check{"php.ini", Warn, "enables extensions that are not in this PHP build: " + strings.Join(missing, ", "),
			"Remove those `extension =` lines from " + path}
	}
	return Check{"php.ini", OK, path, ""}
}

func checkNginxConf(root paths.Root, version string) Check {
	conf := filepath.Join(root.Etc(), "nginx", "nginx.conf")
	if _, err := os.Stat(conf); err != nil {
		return Check{"Nginx config", Info, "not generated yet", "It is generated on the next `ebur start`."}
	}
	exe := filepath.Join(root.RuntimeDir("nginx", version), proc.Exe("nginx"))
	cmd := exec.Command(exe, "-t", "-p", filepath.ToSlash(root.Dir())+"/", "-c", filepath.ToSlash(conf))
	proc.Hide(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return Check{"Nginx config", Fail, strings.TrimSpace(lines[0]), "Run `ebur restart` to regenerate it; check templates\\ for recent edits."}
	}
	return Check{"Nginx config", OK, "nginx -t passed", ""}
}

func checkMariaDBData(root paths.Root, version string) Check {
	data, _ := provision.MariaDBPaths(root, version)
	if _, err := os.Stat(filepath.Join(data, "mysql")); err != nil {
		return Check{"Database data", Warn, "not created yet in " + data, "It is created on the next `ebur start`, or run `ebur install mariadb`."}
	}
	return Check{"Database data", OK, data, ""}
}

func checkPort(root paths.Root, port int, hostRunning bool) Check {
	label := fmt.Sprintf("Port %d", port)
	err := ports.Check(port)
	if err == nil {
		return Check{label, OK, "free", ""}
	}
	var c *ports.Conflict
	if !errors.As(err, &c) {
		return Check{label, Warn, err.Error(), ""}
	}
	if c.Owner.Path != "" && strings.HasPrefix(strings.ToLower(c.Owner.Path), strings.ToLower(root.Dir())+string(filepath.Separator)) {
		return Check{label, OK, "used by Ebur (" + c.Owner.Name + ")", ""}
	}
	if hostRunning && c.Owner.PID == 0 {
		return Check{label, Info, "in use; could not tell whether by Ebur", ""}
	}
	return Check{label, Fail, c.Error(), c.Hint()}
}

// checkHosts reports site names missing from the hosts file and, if the
// file is held open by another program, names it (seen with Webroot).
func checkHosts(path string, want []string) Check {
	have := map[string]bool{}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(strings.SplitN(sc.Text(), "#", 2)[0])
			if len(fields) >= 2 && (fields[0] == "127.0.0.1" || fields[0] == "::1") {
				for _, h := range fields[1:] {
					have[strings.ToLower(h)] = true
				}
			}
		}
		f.Close()
	}
	var missing []string
	for _, h := range want {
		if !have[h] {
			missing = append(missing, h)
		}
	}
	lockers, lockErr := winutil.FileLockers(path)
	var lockNote, lockHint string
	if lockErr != nil {
		// The Restart Manager fails like this when the holder is a process
		// Ebur may not inspect, which in practice is security software
		// (seen with Webroot: a Microsoft-signed caller can name it, an
		// unsigned Ebur build cannot).
		lockNote = "; the file appears to be held open by a protected program, often security software, which may block edits"
		lockHint = " If edits fail, allow hosts-file changes in your security software's settings."
	}
	for _, l := range lockers {
		who := l.App
		if l.Service != "" {
			who += " (service " + l.Service + ")"
		}
		lockNote = fmt.Sprintf("; the file is held open by %s, PID %d, which may block edits", who, l.PID)
		lockHint = " If edits fail, allow hosts-file changes in that program."
		if strings.EqualFold(l.Service, "WRSVC") {
			lockHint = " Webroot blocks hosts-file edits: turn off \"Prevent any program from modifying the HOSTS file\" (Advanced Settings > Shields, or the policy in the Webroot console)."
		}
	}
	if len(missing) == 0 {
		if len(want) == 0 {
			return Check{"Hosts file", OK, "no sites to map yet" + lockNote, strings.TrimSpace(lockHint)}
		}
		return Check{"Hosts file", OK, fmt.Sprintf("all %d site names map to 127.0.0.1", len(want)) + lockNote, strings.TrimSpace(lockHint)}
	}
	return Check{"Hosts file", Warn,
		"not mapped to 127.0.0.1: " + strings.Join(missing, ", ") + lockNote,
		"Run `ebur hosts sync` (one administrator prompt)." + lockHint}
}
