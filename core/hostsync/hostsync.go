// Package hostsync keeps the hosts file in step with Ebur's sites
// (SITE-02). It runs in the normal, non-elevated process: it works out
// which names are wanted, checks the file without admin rights, and only
// launches the elevated ebur-helper (one UAC prompt) when something must
// change. The file editing itself lives in core/hostsfile, the only Ebur
// code the elevated helper contains.
package hostsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/hostsfile"
	"github.com/ebur/ebur/core/paths"
	"github.com/ebur/ebur/core/runtimes"
	"github.com/ebur/ebur/core/sites"
	"github.com/ebur/ebur/internal/winutil"
)

// HelperName is the elevated helper's file name; it ships next to ebur.exe.
const HelperName = "ebur-helper.exe"

var (
	// ErrDeclined means the user said No to the administrator prompt.
	ErrDeclined = winutil.ErrDeclined
	// ErrLocked means security software or another program holds the file.
	ErrLocked = hostsfile.ErrLocked
)

// Wanted returns the names Ebur should map: every site, plus Adminer's
// db.<tld> when installed. Sorted.
func Wanted(root paths.Root) ([]string, error) {
	cfg, err := config.Load(root.Config())
	if err != nil {
		return nil, err
	}
	found, _, err := sites.Scan(root.WWW(), cfg.TLD)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, s := range found {
		set[s.Host] = true
	}
	if a, _ := runtimes.Installed(root, "adminer"); len(a) > 0 {
		set["db."+cfg.TLD] = true
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}

// lastAttempt records an automatic sync that did not succeed, so starts
// do not prompt again and again for the same names (a declined prompt, or
// security software blocking the file on every try).
type lastAttempt struct {
	Want   []string  `json:"want"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func statePath(root paths.Root) string { return filepath.Join(root.Data(), "hosts-sync.json") }

// ErrSkipped means an automatic sync was skipped because the last attempt
// for the same names was declined or blocked; Reason says which.
type ErrSkipped struct{ Reason string }

func (e *ErrSkipped) Error() string {
	return "site names are not in the hosts file (last attempt: " + e.Reason + ")"
}

// AutoSync is Sync for automatic use after a start. If the previous
// automatic attempt for exactly these names failed, it does not prompt
// again and returns *ErrSkipped. A manual Sync always tries.
func AutoSync(root paths.Root, want []string) (bool, error) {
	if ok, err := InSync(want); err == nil && ok {
		os.Remove(statePath(root))
		return false, nil
	}
	if s := Skipped(root, want); s != nil {
		return false, s
	}
	changed, err := Sync(want)
	reason := ""
	switch {
	case errors.Is(err, ErrDeclined):
		reason = "the administrator prompt was declined"
	case errors.Is(err, ErrLocked):
		reason = "security software blocked the change"
	}
	if reason != "" {
		if b, jerr := json.Marshal(lastAttempt{Want: want, Reason: reason, At: time.Now()}); jerr == nil {
			os.WriteFile(statePath(root), b, 0o644)
		}
	} else if err == nil {
		os.Remove(statePath(root))
	}
	return changed, err
}

// Skipped reports whether AutoSync would skip these names because the
// last automatic attempt for exactly them failed. Callers use it to avoid
// announcing a prompt that will not come.
func Skipped(root paths.Root, want []string) *ErrSkipped {
	var last lastAttempt
	if b, err := os.ReadFile(statePath(root)); err == nil && json.Unmarshal(b, &last) == nil && hostsfile.Equal(last.Want, want) {
		return &ErrSkipped{Reason: last.Reason}
	}
	return nil
}

// Forget clears the record of a failed automatic attempt; call it after
// a manual sync so the next start behaves normally.
func Forget(root paths.Root) { os.Remove(statePath(root)) }

// HelperPath finds ebur-helper.exe next to the running program.
func HelperPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	p := filepath.Join(filepath.Dir(exe), HelperName)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%s is missing next to %s; reinstall Ebur", HelperName, filepath.Base(exe))
	}
	return p, nil
}

// InSync reports whether Ebur's block already holds exactly want.
func InSync(want []string) (bool, error) {
	b, err := os.ReadFile(hostsfile.Path())
	if err != nil {
		return false, err
	}
	return hostsfile.Equal(hostsfile.Managed(string(b)), want), nil
}

// Sync makes Ebur's block hold exactly want, with one UAC prompt only if
// something must change. It returns whether the file was changed.
func Sync(want []string) (bool, error) {
	if ok, err := InSync(want); err == nil && ok {
		return false, nil
	}
	for _, h := range want {
		if !hostsfile.ValidHost(h) {
			return false, fmt.Errorf("cannot map %q: Ebur only manages names ending in .test or .localhost", h)
		}
	}
	helper, err := HelperPath()
	if err != nil {
		return false, err
	}
	tmp, err := os.MkdirTemp("", "ebur-hosts-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	result := filepath.Join(tmp, "result.txt")
	params := fmt.Sprintf("hosts-clear %q", result)
	if len(want) > 0 {
		list := filepath.Join(tmp, "hosts.txt")
		if err := os.WriteFile(list, []byte(strings.Join(want, "\n")+"\n"), 0o644); err != nil {
			return false, err
		}
		params = fmt.Sprintf("hosts-apply %q %q", list, result)
	}
	runErr := winutil.RunElevated(helper, params)
	if errors.Is(runErr, winutil.ErrDeclined) {
		return false, ErrDeclined
	}
	msg, _ := os.ReadFile(result)
	text := strings.TrimPrefix(strings.TrimSpace(string(msg)), "error: ")
	switch {
	case text == "ok":
		return true, nil
	case strings.HasPrefix(text, ErrLocked.Error()):
		// Keep ErrLocked matchable without repeating its text.
		return false, fmt.Errorf("%w%s", ErrLocked, strings.TrimPrefix(text, ErrLocked.Error()))
	case text != "":
		return false, errors.New(text)
	case runErr != nil:
		return false, fmt.Errorf("ebur-helper failed: %w", runErr)
	}
	return false, errors.New("ebur-helper gave no result")
}
