// Package startup opens the Ebur tray app when the user signs in to
// Windows (SVC-06), through the per-user Run key: no administrator rights,
// and only for the account that turned it on.
package startup

import (
	"os"
	"path/filepath"
	"strings"
)

// ValueName is the name of Ebur's entry under the Run key.
const ValueName = "Ebur"

// Command is what Windows runs at sign-in: the tray app, in the tray only.
func Command(app string) string { return `"` + app + `" --background` }

// AppNextTo returns ebur-app.exe in the same folder as the running program
// (ebur.exe and ebur-app.exe are installed side by side).
func AppNextTo() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Base(exe), "ebur-app.exe") {
		return exe, nil
	}
	return filepath.Join(filepath.Dir(exe), "ebur-app.exe"), nil
}

// Enabled reports whether Ebur opens at sign-in.
func Enabled() bool {
	v, err := read()
	return err == nil && v != ""
}

// Enable makes app open at sign-in (replacing an older path, e.g. after
// the Ebur folder moved).
func Enable(app string) error { return write(Command(app)) }

// Disable stops Ebur opening at sign-in. Not being enabled is not an error.
func Disable() error { return remove() }

// Refresh points an existing entry at app, so a portable Ebur folder that
// was moved still opens from its new place. It does nothing when the entry
// is off.
func Refresh(app string) error {
	v, err := read()
	if err != nil || v == "" || v == Command(app) {
		return nil
	}
	return write(Command(app))
}
