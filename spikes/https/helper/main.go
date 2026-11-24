// ebur-helper is the only Ebur binary that runs elevated (NFR-08).
// It does exactly one job: rewrite the "# ebur begin" / "# ebur end"
// block of the hosts file. Because any local process could launch it,
// it trusts nothing it is given: every hostname is validated, and every
// entry always points at 127.0.0.1.
//
// Usage:
//
//	ebur-helper hosts-apply <hostlist-file> <result-file>
//	ebur-helper hosts-clear <result-file>
//
// The result file receives "ok" or "error: ...", because an elevated
// process launched through UAC has no stdout the caller can read.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	beginMark = "# ebur begin (managed by Ebur, do not edit)"
	endMark   = "# ebur end"
	maxHosts  = 1000
)

// Lowercase DNS labels ending in .test. The spike only allows .test;
// the real helper will take the configured TLD from SITE-04.
var lockRetries int

var hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+test$`)

func hostsPath() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
}

func main() {
	var result string
	var err error
	switch {
	case len(os.Args) == 4 && os.Args[1] == "hosts-apply":
		result = os.Args[3]
		var hosts []string
		if hosts, err = readHostList(os.Args[2]); err == nil {
			err = rewrite(hosts)
		}
	case len(os.Args) == 3 && os.Args[1] == "hosts-clear":
		result = os.Args[2]
		err = rewrite(nil)
	default:
		os.Exit(2)
	}
	msg := fmt.Sprintf("ok (%d lock retries)", lockRetries)
	if err != nil {
		msg = "error: " + err.Error()
	}
	os.WriteFile(result, []byte(msg), 0o644)
	if err != nil {
		os.Exit(1)
	}
}

func readHostList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	seen := map[string]bool{}
	var hosts []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		h := strings.TrimSpace(sc.Text())
		if h == "" {
			continue
		}
		if !hostRe.MatchString(h) || len(h) > 253 {
			return nil, fmt.Errorf("rejected hostname %q", h)
		}
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > maxHosts {
		return nil, errors.New("too many hostnames")
	}
	return hosts, sc.Err()
}

// rewrite replaces the Ebur block and leaves every other line untouched.
// It writes in place (not rename) so the file keeps its ACL and attributes.
func rewrite(hosts []string) error {
	p := hostsPath()
	var raw []byte
	err := retryLocked(func() (err error) { raw, err = os.ReadFile(p); return })
	if err != nil {
		return err
	}
	eol := "\n"
	if strings.Contains(string(raw), "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	var kept []string
	inBlock := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "# ebur begin"):
			inBlock = true
		case inBlock && strings.HasPrefix(l, endMark):
			inBlock = false
		case !inBlock:
			kept = append(kept, l)
		}
	}
	if inBlock {
		return errors.New("hosts file has '# ebur begin' without '# ebur end'; refusing to edit")
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if len(hosts) > 0 {
		kept = append(kept, "", beginMark)
		for _, h := range hosts {
			kept = append(kept, "127.0.0.1 "+h)
		}
		kept = append(kept, endMark)
	}
	out := strings.Join(kept, eol) + eol
	if err := retryLocked(func() error { return os.WriteFile(p, []byte(out), 0o644) }); err != nil {
		return err
	}
	flushDNS()
	return nil
}

// retryLocked retries while another process (antivirus, the DNS Client
// service, an editor) holds the hosts file open: ERROR_SHARING_VIOLATION
// or ERROR_LOCK_VIOLATION. Gives up after about 10 seconds.
func retryLocked(f func() error) error {
	delay := 50 * time.Millisecond
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := f()
		if err == nil || !(errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("hosts file still locked by another process after 10s: %w", err)
		}
		lockRetries++
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
	}
}

// flushDNS clears the resolver cache so a lookup made before the edit
// (cached as "not found") cannot hide the new entry.
func flushDNS() {
	windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache").Call()
}
