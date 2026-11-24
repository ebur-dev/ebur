// Package proc holds process-launch helpers shared by core packages.
package proc

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// Hide starts cmd without a console window.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// Exe adds ".exe" on Windows.
func Exe(name string) string { return name + ".exe" }
