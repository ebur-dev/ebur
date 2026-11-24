// Package winutil wraps Windows APIs used for diagnostics.
package winutil

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Locker is a process holding a file open.
type Locker struct {
	PID     int
	App     string
	Service string // short service name, e.g. "WRSVC" for Webroot
}

var (
	rstrtmgr           = windows.NewLazySystemDLL("rstrtmgr.dll")
	procRmStartSession = rstrtmgr.NewProc("RmStartSession")
	procRmEndSession   = rstrtmgr.NewProc("RmEndSession")
	procRmRegisterRes  = rstrtmgr.NewProc("RmRegisterResources")
	procRmGetList      = rstrtmgr.NewProc("RmGetList")
	errorMoreData      = uintptr(windows.ERROR_MORE_DATA)
	cchRmSessionKey    = 32
)

// rmProcessInfo mirrors RM_PROCESS_INFO (CCH_RM_MAX_APP_NAME+1 and
// CCH_RM_MAX_SVC_NAME+1 characters).
type rmProcessInfo struct {
	PID         uint32
	StartTime   windows.Filetime
	AppName     [256]uint16
	ServiceName [64]uint16
	AppType     uint32
	AppStatus   uint32
	SessionID   uint32
	Restartable int32
}

// FileLockers asks the Restart Manager which processes hold path open.
// This is how Ebur names the antivirus that blocks hosts-file edits.
func FileLockers(path string) ([]Locker, error) {
	var session uint32
	key := make([]uint16, cchRmSessionKey+1)
	if r, _, _ := procRmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0]))); r != 0 {
		return nil, fmt.Errorf("RmStartSession: %w", windows.Errno(r))
	}
	defer procRmEndSession.Call(uintptr(session))

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	files := []*uint16{p}
	if r, _, _ := procRmRegisterRes.Call(uintptr(session), 1, uintptr(unsafe.Pointer(&files[0])), 0, 0, 0, 0); r != 0 {
		return nil, fmt.Errorf("RmRegisterResources: %w", windows.Errno(r))
	}
	// Pass a buffer on the first call: a size-only query with no buffer
	// fails with ERROR_INVALID_HANDLE on this machine (Webroot's WRSVC
	// holding the hosts file). Grow and retry on ERROR_MORE_DATA.
	var needed, reasons uint32
	infos := make([]rmProcessInfo, 16)
	var r uintptr
	for attempt := 0; attempt < 3; attempt++ {
		count := uint32(len(infos))
		r, _, _ = procRmGetList.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&infos[0])), uintptr(unsafe.Pointer(&reasons)))
		if r == errorMoreData {
			infos = make([]rmProcessInfo, needed+4)
			continue
		}
		if r != 0 && (count == 0 || infos[0].PID == 0) {
			return nil, fmt.Errorf("RmGetList: %w", windows.Errno(r))
		}
		infos = infos[:count]
		break
	}
	if r == errorMoreData {
		return nil, fmt.Errorf("RmGetList: %w", windows.Errno(r))
	}
	count := uint32(len(infos))
	out := make([]Locker, 0, count)
	for _, in := range infos[:count] {
		out = append(out, Locker{
			PID:     int(in.PID),
			App:     windows.UTF16ToString(in.AppName[:]),
			Service: windows.UTF16ToString(in.ServiceName[:]),
		})
	}
	return out, nil
}
