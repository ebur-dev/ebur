//go:build !windows

package hostsfile

func isLocked(error) bool { return false }

// FlushDNS is a no-op where the OS does not cache hosts lookups this way.
func FlushDNS() {}
