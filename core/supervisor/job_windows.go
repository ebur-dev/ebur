package supervisor

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobOnce sync.Once
	jobErr  error
)

// joinKillOnCloseJob puts the current process in a Job Object with
// KILL_ON_JOB_CLOSE. Children inherit the job, so when Ebur exits for
// any reason the OS kills every descendant (tested in spikes/phppool).
// Joining the whole process, rather than each child, avoids a race where
// a child (e.g. the nginx master) spawns workers before being assigned.
func joinKillOnCloseJob() error {
	jobOnce.Do(func() {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = err
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(job)
			jobErr = err
			return
		}
		// The handle is deliberately never closed: closing it is what
		// kills the children, and that must only happen at process exit.
		jobErr = windows.AssignProcessToJobObject(job, windows.CurrentProcess())
	})
	return jobErr
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
