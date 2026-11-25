//go:build !windows

package proc

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func Hide(*exec.Cmd) {}

func Exe(name string) string { return name }

// WaitExit waits up to d for process pid to exit and reports whether it
// has.
func WaitExit(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		p, err := os.FindProcess(pid)
		if err != nil || p.Signal(syscall.Signal(0)) != nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
