//go:build !windows

// Package winutil wraps Windows APIs used for diagnostics.
package winutil

type Locker struct {
	PID     int
	App     string
	Service string
}

// FileLockers is Windows-only; elsewhere files are not locked this way.
func FileLockers(string) ([]Locker, error) { return nil, nil }
