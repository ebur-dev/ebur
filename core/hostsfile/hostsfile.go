// Package hostsfile manages Ebur's entries in the Windows hosts file
// (SITE-02). Ebur only ever edits lines between "# ebur begin" and
// "# ebur end"; everything else in the file is left as it was.
//
// This is the only Ebur code inside the elevated ebur-helper.exe, so it
// depends on nothing else in Ebur. Deciding what to write and launching
// the helper live in core/hostsync, in the normal process.
package hostsfile

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	BeginMark = "# ebur begin (managed by Ebur, do not edit)"
	EndMark   = "# ebur end"
	maxHosts  = 1000
)

// Path is the system hosts file.
func Path() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
}

// Only local development names are accepted, and the helper maps them to
// 127.0.0.1 only. Even if another program launched the elevated helper and
// the user approved the prompt, it could not redirect a real site.
var hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+(test|localhost)$`)

// ValidHost reports whether name may be written to the hosts file.
func ValidHost(name string) bool { return len(name) <= 253 && hostRe.MatchString(name) }

// Managed returns the names currently in Ebur's block, sorted.
func Managed(content string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "# ebur begin"):
			in = true
		case in && strings.HasPrefix(l, EndMark):
			in = false
		case in:
			if f := strings.Fields(l); len(f) >= 2 {
				out = append(out, f[1:]...)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Mapped returns every name the file maps to a loopback address, inside
// or outside Ebur's block (for `ebur doctor`).
func Mapped(content string) map[string]bool {
	m := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		f := strings.Fields(strings.SplitN(sc.Text(), "#", 2)[0])
		if len(f) >= 2 && (f[0] == "127.0.0.1" || f[0] == "::1") {
			for _, h := range f[1:] {
				m[strings.ToLower(h)] = true
			}
		}
	}
	return m
}

// Rewrite returns content with Ebur's block replaced by hosts (or removed
// when hosts is empty). Line endings follow the original file.
func Rewrite(content string, hosts []string) (string, error) {
	for _, h := range hosts {
		if !ValidHost(h) {
			return "", fmt.Errorf("refusing to map %q: only names ending in .test or .localhost are allowed", h)
		}
	}
	if len(hosts) > maxHosts {
		return "", fmt.Errorf("too many names (%d, max %d)", len(hosts), maxHosts)
	}
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	var kept []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "# ebur begin"):
			in = true
		case in && strings.HasPrefix(l, EndMark):
			in = false
		case !in:
			kept = append(kept, l)
		}
	}
	if in {
		return "", errors.New("the hosts file has '# ebur begin' without '# ebur end'; fix it by hand before Ebur edits it")
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if len(hosts) > 0 {
		sorted := append([]string(nil), hosts...)
		sort.Strings(sorted)
		kept = append(kept, "", BeginMark)
		for _, h := range dedupe(sorted) {
			kept = append(kept, "127.0.0.1 "+h)
		}
		kept = append(kept, EndMark)
	}
	return strings.Join(kept, eol) + eol, nil
}

func dedupe(sorted []string) []string {
	out := sorted[:0:0]
	for i, h := range sorted {
		if i == 0 || h != sorted[i-1] {
			out = append(out, h)
		}
	}
	return out
}

// Equal reports whether two name lists hold the same names.
func Equal(a, b []string) bool {
	a, b = dedupe(sortedCopy(a)), dedupe(sortedCopy(b))
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

// ErrLocked means another program holds the file open, e.g. security
// software that protects the hosts file (seen with Webroot).
var ErrLocked = errors.New("the hosts file is locked by another program, often security software protecting it")

// Apply rewrites the file at path with hosts. It runs in the elevated
// helper. It keeps a one-time backup, writes in place (so the file keeps
// its permissions) and retries while another program has the file locked.
func Apply(path string, hosts []string) error {
	var raw []byte
	err := retryLocked(func() (err error) { raw, err = os.ReadFile(path); return })
	if err != nil {
		return err
	}
	out, err := Rewrite(string(raw), hosts)
	if err != nil {
		return err
	}
	if out == string(raw) {
		return nil
	}
	backup := path + ".ebur-backup"
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		os.WriteFile(backup, raw, 0o644) // best effort: the original before Ebur's first edit
	}
	if err := retryLocked(func() error { return os.WriteFile(path, []byte(out), 0o644) }); err != nil {
		return err
	}
	// Read back: some security tools silently undo edits.
	check, err := os.ReadFile(path)
	if err == nil && !Equal(Managed(string(check)), hosts) {
		return errors.New("the hosts file did not keep Ebur's changes; security software may have reverted them")
	}
	return nil
}

// retryWindow bounds how long Apply waits for a locked file.
var retryWindow = 10 * time.Second

func retryLocked(f func() error) error {
	delay := 50 * time.Millisecond
	deadline := time.Now().Add(retryWindow)
	for {
		err := f()
		if err == nil || !isLocked(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (still locked after %v)", ErrLocked, retryWindow)
		}
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
	}
}
