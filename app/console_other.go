//go:build !windows

package main

import "errors"

// TODO(v2): open Terminal.app / the user's terminal with PATH set.
func openConsole(string, []string, string) error {
	return errors.New("opening a terminal is only implemented on Windows so far")
}
