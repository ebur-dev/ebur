// Package project reads a site's own settings and decides which PHP
// version serves it (PHP-04): a `.ebur` file first (SRS §6.2), then the
// PHP constraint in composer.json, then the default version.
package project

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/internal/fsutil"
	manifestdata "github.com/ebur-dev/ebur/manifest"
)

// FileName is the per-project settings file, committed with the project.
const FileName = ".ebur"

// Sources of a site's PHP version.
const (
	SourceDefault  = "default"
	SourceEbur     = ".ebur"
	SourceComposer = "composer.json"
)

// Choice is the PHP version picked for one site, and why.
type Choice struct {
	Version string `json:"version"`           // what serves the site
	Source  string `json:"source"`            // default, .ebur or composer.json
	Want    string `json:"want,omitempty"`    // what the project asked for, e.g. "8.3" or "^8.2"
	Problem string `json:"problem,omitempty"` // set when the request could not be met
}

// ReadSettings parses a .ebur file: one "key: value" per line, # for
// comments, values optionally quoted. A missing file is not an error.
func ReadSettings(dir string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(dir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		k, v, ok := strings.Cut(text, ":")
		if !ok {
			return out, fmt.Errorf("%s line %d: expected \"key: value\"", FileName, line)
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(v, `"'`)
	}
	return out, sc.Err()
}

// ComposerPHP returns require.php from composer.json, if any.
func ComposerPHP(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return ""
	}
	var c struct {
		Require map[string]string `json:"require"`
	}
	if json.Unmarshal(b, &c) != nil {
		return ""
	}
	return strings.TrimSpace(c.Require["php"])
}

// Resolve picks the PHP version for the site in dir. installed is
// newest first; active is the default version.
func Resolve(dir string, installed []string, active string) Choice {
	def := Choice{Version: active, Source: SourceDefault}
	if s, err := ReadSettings(dir); err != nil {
		def.Problem = err.Error()
		return def
	} else if want := s["php"]; want != "" {
		for _, v := range installed {
			if v == want || strings.HasPrefix(v, want+".") {
				return Choice{Version: v, Source: SourceEbur, Want: want}
			}
		}
		def.Source, def.Want = SourceEbur, want
		def.Problem = fmt.Sprintf("PHP %s is not installed; using %s. Run `ebur php install %s`.", want, active, want)
		return def
	}
	if want := ComposerPHP(dir); want != "" {
		c, err := ParseConstraint(want)
		if err != nil {
			def.Problem = fmt.Sprintf("composer.json PHP constraint %q not understood (%v); using %s.", want, err, active)
			return def
		}
		// Prefer the default version when it fits: fewer PHP versions run.
		if Satisfies(c, active) {
			return Choice{Version: active, Source: SourceComposer, Want: want}
		}
		for _, v := range installed {
			if Satisfies(c, v) {
				return Choice{Version: v, Source: SourceComposer, Want: want}
			}
		}
		def.Source, def.Want = SourceComposer, want
		def.Problem = fmt.Sprintf("no installed PHP matches %q; using %s. %s", want, active, suggest(c, Available()))
		return def
	}
	return def
}

// Available lists the PHP versions Ebur can install (from the built-in
// manifest), newest first. A variable so tests can replace it.
var Available = func() []string {
	m, err := manifest.Parse(manifestdata.JSON)
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range m.Releases("php", "x64") {
		out = append(out, r.Version)
	}
	return out
}

// suggest tells the user how to meet a constraint: install a matching
// version Ebur offers, or, when none does (e.g. ^7.4, end of life),
// change the project's requirement.
func suggest(c Constraint, available []string) string {
	for _, v := range available {
		if Satisfies(c, v) {
			p := strings.SplitN(v, ".", 3)
			return fmt.Sprintf("Install a matching version: `ebur php install %s.%s`.", p[0], p[1])
		}
	}
	if len(available) == 0 {
		return "Install a matching PHP version."
	}
	return fmt.Sprintf("Ebur offers PHP %s to %s, and none matches: update require.php in composer.json, or pin a version with `ebur sites php <site> <version>`.",
		available[len(available)-1], available[0])
}

// SetPHP writes "php: <version>" into dir's .ebur, keeping other lines,
// or removes the line when version is "" (back to the default).
func SetPHP(dir, version string) error { return setVersion(dir, "php", "PHP", version) }

// SetNode writes "node: <version>" into dir's .ebur (or removes it).
func SetNode(dir, version string) error { return setVersion(dir, "node", "Node", version) }

func setVersion(dir, key, label, version string) error {
	if version != "" {
		if _, err := manifest.ParseVersion(version); err != nil {
			return fmt.Errorf("invalid %s version %q", label, version)
		}
	}
	path := filepath.Join(dir, FileName)
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var lines []string
	done := false
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if k, _, ok := strings.Cut(strings.TrimSpace(l), ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			if version != "" && !done {
				lines = append(lines, fmt.Sprintf("%s: %q", key, version))
				done = true
			}
			continue
		}
		lines = append(lines, l)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if version != "" && !done {
		if len(lines) == 0 {
			lines = append(lines, "# Ebur settings for this project (see ebur sites).")
		}
		lines = append(lines, fmt.Sprintf("%s: %q", key, version))
	}
	if len(lines) == 0 || (len(lines) == 1 && strings.HasPrefix(lines[0], "# Ebur settings")) {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return fsutil.WriteFileAtomic(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// Versions returns the distinct PHP versions in choices, with active
// first, so the default pool always gets the first port range.
func Versions(active string, choices []Choice) []string {
	set := map[string]bool{}
	for _, c := range choices {
		if c.Version != "" && c.Version != active {
			set[c.Version] = true
		}
	}
	out := []string{active}
	var rest []string
	for v := range set {
		rest = append(rest, v)
	}
	sort.Slice(rest, func(i, j int) bool { return manifest.CompareVersions(rest[i], rest[j]) > 0 })
	return append(out, rest...)
}
