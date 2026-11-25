// Package shellenv builds the environment for Ebur's terminal (GUI-04)
// and `ebur exec`: the active runtimes first on PATH, and PHP pointed at
// Ebur's php.ini, without ever changing the system PATH.
package shellenv

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/core/project"
	"github.com/ebur-dev/ebur/core/runtimes"
	"github.com/ebur-dev/ebur/core/sites"
)

// Env is what a console needs on top of the user's own environment.
type Env struct {
	Path       []string          // folders to put in front of PATH
	Vars       map[string]string // variables to set
	PHP        string            // the PHP version on PATH
	PHPSource  string            // the site that chose it, e.g. "shop (.ebur)"; "" for the default
	Node       string            // the Node version on PATH ("" when Node is not installed)
	NodeSource string            // like PHPSource, e.g. "shop (.nvmrc)"
}

func active(root paths.Root, cfg config.Settings, rt string) string {
	if v := cfg.Active[rt]; v != "" {
		return v
	}
	if inst, _ := runtimes.Installed(root, rt); len(inst) > 0 {
		return inst[0]
	}
	return ""
}

// For returns the environment for root's runtimes. When dir is inside a
// site, or inside a linked project's real folder (`ebur link`), PHP is
// that site's version (PHP-04), so `ebur exec composer` and
// `php artisan` match what serves the site.
func For(root paths.Root, dir string) Env {
	cfg, _ := config.Load(root.Config())
	env := Env{Vars: map[string]string{paths.EnvRoot: root.Dir()}}
	v := active(root, cfg, "php")
	env.Node = active(root, cfg, "node")
	if dir != "" {
		found, _, _ := sites.Scan(root.WWW(), cfg.TLD)
		if s, ok := sites.ForPath(found, dir); ok {
			if v != "" {
				installed, _ := runtimes.Installed(root, "php")
				c := project.Resolve(s.Dir, installed, v)
				v, env.PHPSource = c.Version, s.Name+" ("+c.Source+")"
			}
			// Node too (PROJ-03): .ebur, .nvmrc, then package.json engines.
			if nodes, _ := runtimes.Installed(root, "node"); len(nodes) > 0 {
				c := project.ResolveNode(s.Dir, nodes, env.Node)
				env.Node, env.NodeSource = c.Version, s.Name+" ("+c.Source+")"
			}
		}
	}
	return build(root, cfg, env, v)
}

// ForPHP is For with an explicit PHP version ("" means the default),
// used when no site exists yet, e.g. while `ebur new` runs Composer.
func ForPHP(root paths.Root, version string) Env {
	cfg, _ := config.Load(root.Config())
	env := Env{Vars: map[string]string{paths.EnvRoot: root.Dir()}, Node: active(root, cfg, "node")}
	if version == "" {
		version = active(root, cfg, "php")
	}
	return build(root, cfg, env, version)
}

func build(root paths.Root, cfg config.Settings, env Env, v string) Env {
	env.PHP = v
	if v != "" {
		env.Path = append(env.Path, root.RuntimeDir("php", v))
		// php.exe looks for php.ini next to itself; without these it runs
		// with no extensions (no OpenSSL, so Composer cannot download).
		phpEtc := filepath.Join(root.Etc(), "php", v)
		env.Vars["PHPRC"] = phpEtc
		env.Vars["PHP_INI_SCAN_DIR"] = filepath.Join(phpEtc, "conf.d")
	}
	if v := active(root, cfg, "composer"); v != "" {
		env.Path = append(env.Path, root.RuntimeDir("composer", v))
	}
	if env.Node != "" {
		env.Path = append(env.Path, root.RuntimeDir("node", env.Node)) // node, npm, npx, corepack
	}
	if v := active(root, cfg, "mariadb"); v != "" {
		env.Path = append(env.Path, filepath.Join(root.RuntimeDir("mariadb", v), "bin"))
	}
	if v := active(root, cfg, "nginx"); v != "" {
		env.Path = append(env.Path, root.RuntimeDir("nginx", v))
	}
	return env
}

// Environ merges env into base (usually os.Environ()), replacing existing
// values and prefixing PATH. Variable names match case-insensitively, as
// on Windows.
func (e Env) Environ(base []string) []string {
	set := map[string]string{}
	for k, v := range e.Vars {
		set[strings.ToUpper(k)] = k + "=" + v
	}
	var out []string
	pathSeen := false
	for _, kv := range base {
		name, val, _ := strings.Cut(kv, "=")
		up := strings.ToUpper(name)
		if up == "PATH" {
			pathSeen = true
			kv = name + "=" + strings.Join(append(append([]string(nil), e.Path...), val), string(os.PathListSeparator))
		} else if repl, ok := set[up]; ok {
			kv = repl
			delete(set, up)
		}
		out = append(out, kv)
	}
	if !pathSeen && len(e.Path) > 0 {
		out = append(out, "PATH="+strings.Join(e.Path, string(os.PathListSeparator)))
	}
	for _, kv := range set {
		out = append(out, kv)
	}
	return out
}

// Summary names what is on PATH, for the terminal's greeting.
func (e Env) Summary() string {
	var names []string
	for _, p := range e.Path {
		// bin\<runtime>\<version>[\bin] -> runtime
		parts := strings.Split(filepath.ToSlash(p), "/")
		for i := len(parts) - 1; i > 0; i-- {
			if parts[i-1] == "bin" {
				continue
			}
			if i >= 2 && parts[i-2] == "bin" {
				names = append(names, parts[i-1])
				break
			}
		}
	}
	return strings.Join(names, ", ")
}
