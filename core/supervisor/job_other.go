//go:build !windows

package supervisor

import (
	"os/exec"
	"time"
)

// TODO(v2): on Linux use Pdeathsig and a process group; on macOS a
// kqueue NOTE_EXIT watcher. Until then children may outlive Ebur.
func joinKillOnCloseJob() error { return nil }

// TODO(v2): kill the service's process group.
func killDescendants(int, time.Time) int { return 0 }

func hideWindow(*exec.Cmd) {}
