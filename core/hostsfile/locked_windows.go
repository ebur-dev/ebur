package hostsfile

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isLocked(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// FlushDNS clears the resolver cache so a lookup made before an edit
// (cached as "not found") cannot hide the new entry.
func FlushDNS() {
	windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache").Call()
}
