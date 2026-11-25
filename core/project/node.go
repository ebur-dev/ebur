package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/core/manifest"
	manifestdata "github.com/ebur-dev/ebur/manifest"
)

// Sources of a site's Node version, besides SourceDefault and SourceEbur.
const (
	SourceNvmrc   = ".nvmrc"
	SourcePackage = "package.json"
)

// nodeCodenames maps LTS names used in .nvmrc ("lts/jod") to majors.
var nodeCodenames = map[string]string{
	"hydrogen": "18", "iron": "20", "jod": "22", "krypton": "24", "lithium": "26",
}

// AvailableNode lists the Node versions Ebur can install, newest first.
// A variable so tests can replace it.
var AvailableNode = func() []string {
	m, err := manifest.Parse(manifestdata.JSON)
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range m.Releases("node", "x64") {
		out = append(out, r.Version)
	}
	return out
}

// NvmrcNode reads the version from .nvmrc or .node-version ("" if none).
func NvmrcNode(dir string) (file, want string) {
	for _, name := range []string{".nvmrc", ".node-version"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		line, _, _ := strings.Cut(strings.ReplaceAll(string(b), "\r", ""), "\n")
		if line, _, _ = strings.Cut(line, "#"); strings.TrimSpace(line) != "" {
			return name, strings.TrimSpace(line)
		}
	}
	return "", ""
}

// PackageNode returns package.json's "engines": {"node": ...} constraint.
func PackageNode(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var p struct {
		Engines map[string]any `json:"engines"`
	}
	if json.Unmarshal(b, &p) != nil {
		return ""
	}
	s, _ := p.Engines["node"].(string)
	return strings.TrimSpace(s)
}

// ResolveNode picks the Node version for the project in dir: .ebur's
// "node:", then .nvmrc / .node-version, then package.json engines.node,
// then the default. installed is newest first; active is the default
// ("" when Node is not installed).
func ResolveNode(dir string, installed []string, active string) Choice {
	def := Choice{Version: active, Source: SourceDefault}
	notInstalled := func(src, want, what string) Choice {
		c := def
		c.Source, c.Want = src, want
		using := "using " + orText(active, "no Node")
		c.Problem = fmt.Sprintf("Node %s is not installed; %s. %s", what, using, suggestNode(want))
		return c
	}
	s, err := ReadSettings(dir)
	if err != nil {
		def.Problem = err.Error()
		return def
	}
	if want := s["node"]; want != "" {
		if v := newestMatching(installed, strings.TrimPrefix(want, "v")); v != "" {
			return Choice{Version: v, Source: SourceEbur, Want: want}
		}
		return notInstalled(SourceEbur, want, want)
	}
	if file, want := NvmrcNode(dir); want != "" {
		series := nvmrcSeries(want, installed)
		if series == "" {
			c := def
			c.Source, c.Want = file, want
			c.Problem = fmt.Sprintf("%s asks for %q, which Ebur does not understand; using %s.", file, want, orText(active, "no Node"))
			return c
		}
		// nvm users often pin an exact patch (v22.11.0); Ebur ships the
		// latest patch of each LTS line, so the same major is used.
		if v := newestMatching(installed, series); v != "" {
			return Choice{Version: v, Source: file, Want: want}
		}
		return notInstalled(file, want, series)
	}
	if want := PackageNode(dir); want != "" {
		c, err := ParseConstraint(want)
		if err != nil {
			def.Problem = fmt.Sprintf("package.json engines.node %q not understood (%v); using %s.", want, err, orText(active, "no Node"))
			return def
		}
		if active != "" && Satisfies(c, active) {
			return Choice{Version: active, Source: SourcePackage, Want: want}
		}
		for _, v := range installed {
			if Satisfies(c, v) {
				return Choice{Version: v, Source: SourcePackage, Want: want}
			}
		}
		res := def
		res.Source, res.Want = SourcePackage, want
		hint := "Pin one with `ebur sites node <site> <version>`, or update engines.node."
		for _, v := range AvailableNode() {
			if Satisfies(c, v) {
				hint = fmt.Sprintf("Run `ebur node install %s`.", major(v))
				break
			}
		}
		res.Problem = fmt.Sprintf("no installed Node matches %q; using %s. %s", want, orText(active, "no Node"), hint)
		return res
	}
	return def
}

// nvmrcSeries turns an .nvmrc value into a major version to look for.
func nvmrcSeries(want string, installed []string) string {
	w := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(want), "v"))
	switch {
	case w == "node" || w == "stable" || w == "latest" || w == "lts/*" || w == "lts/latest":
		if len(installed) > 0 {
			return major(installed[0])
		}
		if a := AvailableNode(); len(a) > 0 {
			return major(a[0])
		}
		return ""
	case strings.HasPrefix(w, "lts/"):
		return nodeCodenames[strings.TrimPrefix(w, "lts/")]
	}
	if _, err := manifest.ParseVersion(w); err == nil {
		return major(w)
	}
	if _, _, err := parseVersion(w); err == nil && w != "" && !strings.ContainsAny(w, "<>=^~ ") {
		return major(w)
	}
	return ""
}

func newestMatching(installed []string, want string) string {
	for _, v := range installed {
		if v == want || strings.HasPrefix(v, want+".") {
			return v
		}
	}
	return ""
}

func suggestNode(want string) string {
	m := major(strings.TrimPrefix(want, "v"))
	for _, v := range AvailableNode() {
		if major(v) == m {
			return fmt.Sprintf("Run `ebur node install %s`.", m)
		}
	}
	return "Ebur offers Node " + strings.Join(majors(AvailableNode()), ", ") + "; pin one with `ebur sites node <site> <version>`."
}

func major(v string) string {
	m, _, _ := strings.Cut(v, ".")
	return m
}

func majors(vs []string) []string {
	var out []string
	for _, v := range vs {
		out = append(out, major(v))
	}
	return out
}

func orText(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}
