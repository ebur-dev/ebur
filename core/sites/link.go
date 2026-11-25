package sites

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Projects kept outside www\ are served through a junction in www\
// (`ebur link`), as Laravel Valet's "link" does. www\ stays the one list
// of sites, Explorer shows it, and the watcher picks links up like
// folders. Ebur creates and removes links itself because some tools
// delete a junction's target along with it.

var nameCleanRe = regexp.MustCompile(`[^a-z0-9-]+`)

// NameFor turns a folder name into a site name: "My_Shop 2" -> "my-shop-2".
func NameFor(folder string) string {
	n := nameCleanRe.ReplaceAllString(strings.ToLower(folder), "-")
	n = strings.Trim(n, "-")
	if len(n) > 63 {
		n = strings.Trim(n[:63], "-")
	}
	return n
}

// IsLink reports whether www\<name> is a link rather than a real folder.
func IsLink(www, name string) bool {
	fi, err := os.Lstat(filepath.Join(www, name))
	return err == nil && fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0
}

// within reports whether path is dir or inside it (case-insensitive on
// Windows, after expanding 8.3 short names).
func within(path, dir string) bool {
	p, d := longPath(path), longPath(dir)
	if samePath(p, d) {
		return true
	}
	sep := string(filepath.Separator)
	return len(p) > len(d) && samePath(p[:len(d)+1], strings.TrimSuffix(d, sep)+sep)
}

// Reserved maps folder names under www\ that Ebur serves itself to the tool.
var Reserved = map[string]string{"db": "Adminer", "mail": "Mailpit"}

// ValidName reports whether name can be a site name (one DNS label).
func ValidName(name string) bool { return labelRe.MatchString(name) }

// Link serves the project folder target as <name>.<tld>. An empty name
// is derived from the folder name. It returns the site name used.
func Link(root, www, target, name string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	abs = longPath(abs) // store C:\Users\Jane, not C:\Users\JANE~1
	// Checked before touching the path: a missing server can take seconds
	// to time out.
	if strings.HasPrefix(abs, `\\`) {
		return "", errors.New("projects on network shares cannot be linked (Windows junctions only point to local drives); copy the project to a local drive")
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a folder", abs)
	}
	if within(abs, www) {
		return "", fmt.Errorf("%s is already inside www\\; it is served without a link", abs)
	}
	if within(root, abs) {
		return "", fmt.Errorf("%s contains the Ebur folder itself; link the project folder, not a parent of Ebur", abs)
	}
	if name == "" {
		name = NameFor(filepath.Base(abs))
	}
	name = strings.ToLower(name)
	if !labelRe.MatchString(name) {
		return "", fmt.Errorf("%q is not a valid site name: use letters, digits and dashes", name)
	}
	link := filepath.Join(www, name)
	if _, err := os.Lstat(link); err == nil {
		return "", fmt.Errorf("www\\%s already exists; choose another name with --name", name)
	}
	if err := os.MkdirAll(www, 0o755); err != nil {
		return "", err
	}
	if err := makeLink(link, abs); err != nil {
		return "", err
	}
	return name, nil
}

// Unlink removes www\<name> if, and only if, it is a link. The project
// folder it points to is never touched.
func Unlink(www, name string) error {
	link := filepath.Join(www, strings.ToLower(name))
	if _, err := os.Lstat(link); err != nil {
		return fmt.Errorf("no site named %q", name)
	}
	if !IsLink(www, name) {
		return fmt.Errorf("www\\%s is a real folder, not a link; Ebur will not delete it", name)
	}
	// os.Remove does not recurse: it removes the junction itself.
	return os.Remove(link)
}

// ForPath returns the site whose folder (or linked project folder)
// contains path, so tools run inside a linked project's real folder use
// that site's settings (PHP-04).
func ForPath(found []Site, path string) (Site, bool) {
	for _, s := range found {
		if within(path, s.Dir) || (s.Target != "" && within(path, s.Target)) {
			return s, true
		}
	}
	return Site{}, false
}
