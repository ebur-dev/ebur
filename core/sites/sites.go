// Package sites turns folders under www\ into sites (SITE-01, SITE-03).
package sites

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ebur-dev/ebur/core/project"
)

// Site is one folder served at <name>.<tld>.
type Site struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Dir     string `json:"dir"`
	DocRoot string `json:"doc_root"`
	// PHP is the version serving this site and why (PHP-04). Scan leaves
	// it empty; the stack fills it in with project.Resolve.
	PHP project.Choice `json:"php"`
	// Target is set for a linked site (`ebur link`): the project's real
	// folder outside www\, which Dir points to through a junction.
	Target string `json:"target,omitempty"`
}

// A folder name must be a valid DNS label to become a host name.
var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Skipped explains why a folder is not served, for `ebur doctor`.
type Skipped struct {
	Folder string `json:"folder"`
	Reason string `json:"reason"`
}

// Scan lists the sites in www, sorted by name. Folders whose names are not
// valid host names are returned in skipped rather than silently ignored.
func Scan(www, tld string) (sites []Site, skipped []Skipped, err error) {
	entries, err := os.ReadDir(www)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(www, e.Name())
		target := ""
		if !e.IsDir() {
			// A junction or symlink (`ebur link`) is listed as irregular or a
			// link, not as a folder: follow it.
			if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
				continue
			}
			target, _ = os.Readlink(dir)
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				skipped = append(skipped, Skipped{e.Name(), fmt.Sprintf("linked project folder %s is missing; run `ebur unlink %s` or restore the folder", target, e.Name())})
				continue
			}
		}
		name := strings.ToLower(e.Name())
		if !labelRe.MatchString(name) {
			skipped = append(skipped, Skipped{e.Name(), "folder name is not a valid host name: use letters, digits and dashes"})
			continue
		}
		sites = append(sites, Site{Name: name, Host: name + "." + tld, Dir: dir, DocRoot: DocRoot(dir), Target: target})
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })
	return sites, skipped, nil
}

// DocRoot picks public\ (Laravel, Symfony 4+), web\ (older Symfony,
// Bedrock) or the folder itself (WordPress, plain PHP).
func DocRoot(dir string) string {
	for _, sub := range []string{"public", "web"} {
		p := filepath.Join(dir, sub)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return dir
}
