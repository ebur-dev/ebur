//go:build !windows

package proc

import "os/exec"

func Hide(*exec.Cmd) {}

func Exe(name string) string { return name }
