package main

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// startDetached starts the host with no console and outside the
// terminal's job, so closing the terminal does not kill Ebur. Some
// terminals forbid breaking away from their job; then it starts without.
func startDetached(cmd *exec.Cmd) error {
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW)
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: flags | windows.CREATE_BREAKAWAY_FROM_JOB}
	if err := cmd.Start(); err == nil {
		return nil
	}
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: flags}
	return cmd.Start()
}
