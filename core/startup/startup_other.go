//go:build !windows

package startup

import "errors"

// Opening at sign-in is Windows-only for now (macOS and Linux are v2).
var errUnsupported = errors.New("opening Ebur at sign-in is only supported on Windows")

func read() (string, error) { return "", nil }
func write(string) error    { return errUnsupported }
func remove() error         { return nil }
