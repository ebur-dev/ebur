package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// openConsole starts cmd.exe in its own console window with dirs in front
// of PATH. Only that console's environment changes, never the system PATH.
func openConsole(workDir string, dirs []string, root string) error {
	path := strings.Join(append(dirs, os.Getenv("PATH")), ";")
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
		CmdLine:       `cmd.exe /K "title Ebur terminal && echo Ebur runtimes are on PATH for this window: php, mariadb, nginx. && echo."`,
	}
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "PATH="+path, "EBUR_ROOT="+root)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
