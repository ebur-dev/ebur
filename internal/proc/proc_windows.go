// Package proc holds process-launch helpers shared by core packages.
package proc

import (
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

// Hide starts cmd without a console window.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// Exe adds ".exe" on Windows.
func Exe(name string) string { return name + ".exe" }

// WaitExit waits up to d for process pid to exit and reports whether it
// has. A process that cannot be opened is taken as gone.
func WaitExit(pid int, d time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(d/time.Millisecond))
	return err == nil && ev == windows.WAIT_OBJECT_0
}
