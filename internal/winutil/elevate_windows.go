package winutil

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrDeclined means the user answered No to the UAC prompt.
var ErrDeclined = errors.New("the administrator prompt was declined")

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	errorCancelled        = windows.Errno(1223)
)

var procShellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// RunElevated runs exe with params as administrator, showing the UAC
// prompt, and waits for it. It returns ErrDeclined if the user says no,
// and an error for a non-zero exit code.
func RunElevated(exe, params string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	args, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: args,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(callErr, errorCancelled) {
			return ErrDeclined
		}
		return callErr
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exit code %d", code)
	}
	return nil
}
