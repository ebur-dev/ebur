//go:build !windows

package ports

// TODO(v2): read /proc/net/tcp on Linux and use lsof on macOS.
func findListener(int) (Owner, bool, error) { return Owner{}, false, nil }
