package supervisor

import (
	"os/exec"
	"sync"
	"time"
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

// killDescendants terminates every live process descended from pid that
// was created at or after since. The creation-time check stops a reused
// PID from matching an unrelated process. It returns how many it ended.
func killDescendants(pid int, since time.Time) int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
	}
	killed := 0
	queue := []uint32{uint32(pid)}
	seen := map[uint32]bool{uint32(pid): true}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, c := range children[parent] {
			if seen[c] {
				continue
			}
			seen[c] = true
			h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, c)
			if err != nil {
				continue
			}
			var created, exited, kernel, user windows.Filetime
			if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) == nil &&
				!time.Unix(0, created.Nanoseconds()).Before(since) {
				queue = append(queue, c) // its children go too
				if windows.TerminateProcess(h, 1) == nil {
					// TerminateProcess is asynchronous: wait until the
					// process is gone so its ports are free for a restart.
					windows.WaitForSingleObject(h, 3000)
					killed++
				}
			}
			windows.CloseHandle(h)
		}
	}
	return killed
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
