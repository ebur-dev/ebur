//go:build !windows

package supervisor

import "os/exec"

// TODO(v2): on Linux use Pdeathsig and a process group; on macOS a
// kqueue NOTE_EXIT watcher. Until then children may outlive Ebur.
func joinKillOnCloseJob() error { return nil }

func hideWindow(*exec.Cmd) {}
