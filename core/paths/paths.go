// Package paths resolves every location inside the Ebur root (SRS §6.1).
//
// Nothing is hard-coded to C:\ebur: the root is found at run time, so the
// whole install keeps working after the folder is moved (NFR-11).
package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// EnvRoot overrides the root, mainly for tests and portable setups.
const EnvRoot = "EBUR_ROOT"

// Root is an absolute path to an Ebur install folder.
type Root struct{ dir string }

// New returns a Root for dir, made absolute.
func New(dir string) (Root, error) {
	if strings.TrimSpace(dir) == "" {
		return Root{}, errors.New("paths: empty root")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Root{}, err
	}
	return Root{dir: filepath.Clean(abs)}, nil
}

// Find returns $EBUR_ROOT if set, otherwise the folder holding the
// running executable.
func Find() (Root, error) {
	if d := os.Getenv(EnvRoot); d != "" {
		return New(d)
	}
	exe, err := os.Executable()
	if err != nil {
		return Root{}, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return New(filepath.Dir(exe))
}

func (r Root) Dir() string       { return r.dir }
func (r Root) Config() string    { return filepath.Join(r.dir, "ebur.json") }
func (r Root) Bin() string       { return filepath.Join(r.dir, "bin") }
func (r Root) Templates() string { return filepath.Join(r.dir, "templates") }
func (r Root) Etc() string       { return filepath.Join(r.dir, "etc") }
func (r Root) Data() string      { return filepath.Join(r.dir, "data") }
func (r Root) Downloads() string { return filepath.Join(r.dir, "data", "downloads") }
func (r Root) SSL() string       { return filepath.Join(r.dir, "ssl") }
func (r Root) Logs() string      { return filepath.Join(r.dir, "logs") }
func (r Root) WWW() string       { return filepath.Join(r.dir, "www") }

// RuntimeDir is where one installed runtime version lives: bin\<name>\<version>.
func (r Root) RuntimeDir(name, version string) string {
	return filepath.Join(r.dir, "bin", name, version)
}

// Ensure creates the standard folders if they are missing.
func (r Root) Ensure() error {
	for _, d := range []string{r.Bin(), r.Templates(), r.Etc(), r.Data(), r.Downloads(), r.SSL(), r.Logs(), r.WWW()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
