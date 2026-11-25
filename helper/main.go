// Command ebur-helper is the only Ebur program that runs as administrator
// (NFR-08). It does one thing: rewrite Ebur's block in the hosts file.
//
// Any program could launch it, so it trusts nothing it is given: every
// name must end in .test or .localhost and always maps to 127.0.0.1, and
// it only ever writes the system hosts file.
//
// Usage (via UAC, from ebur.exe or ebur-app.exe):
//
//	ebur-helper hosts-apply <name-list-file> <result-file>
//	ebur-helper hosts-clear <result-file>
//
// The result file receives "ok" or "error: ...", because a process
// started through UAC has no output the caller can read.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ebur-dev/ebur/core/hostsfile"
)

func main() {
	var result string
	var err error
	switch {
	case len(os.Args) == 4 && os.Args[1] == "hosts-apply":
		result = os.Args[3]
		var names []string
		if names, err = readNames(os.Args[2]); err == nil {
			err = hostsfile.Apply(hostsfile.Path(), names)
		}
	case len(os.Args) == 3 && os.Args[1] == "hosts-clear":
		result = os.Args[2]
		err = hostsfile.Apply(hostsfile.Path(), nil)
	default:
		fmt.Fprintln(os.Stderr, "ebur-helper is started by Ebur; it is not meant to be run by hand.")
		os.Exit(2)
	}
	msg := "ok"
	if err == nil {
		hostsfile.FlushDNS()
	} else {
		msg = "error: " + err.Error()
	}
	os.WriteFile(result, []byte(msg), 0o644)
	if err != nil {
		os.Exit(1)
	}
}

// readNames reads one name per line, validating each.
func readNames(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n := strings.TrimSpace(sc.Text())
		if n == "" {
			continue
		}
		if !hostsfile.ValidHost(n) {
			return nil, fmt.Errorf("rejected %q: only names ending in .test or .localhost are allowed", n)
		}
		names = append(names, n)
	}
	return names, sc.Err()
}
