package proc

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWaitExit(t *testing.T) {
	if !WaitExit(0x7ffffff0, 10*time.Millisecond) {
		t.Error("a process that does not exist should count as gone")
	}
	// A long-running child: not gone within 200 ms, gone once killed.
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=TestHelperSleep")
	cmd.Env = append(os.Environ(), "PROC_HELPER_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	if WaitExit(cmd.Process.Pid, 200*time.Millisecond) {
		t.Fatal("running process reported as exited")
	}
	if d := time.Since(t0); d < 150*time.Millisecond || d > 2*time.Second {
		t.Errorf("waited %v", d)
	}
	cmd.Process.Kill()
	if !WaitExit(cmd.Process.Pid, 3*time.Second) {
		t.Error("killed process not reported as exited")
	}
	cmd.Wait()
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("PROC_HELPER_SLEEP") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(30 * time.Second)
}
