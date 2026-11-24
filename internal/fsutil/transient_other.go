//go:build !windows

package fsutil

// On Unix-like systems open files do not block renames.
func isTransient(error) bool { return false }
