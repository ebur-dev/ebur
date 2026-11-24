package shellenv

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// Regression: the Ebur terminal closed as soon as it opened, because
// cmd.exe got NUL as its input. A hidden console must stay open.
func TestConsoleStaysOpen(t *testing.T) {
	env := Env{Vars: map[string]string{"EBUR_TEST": "1"}}.Environ(os.Environ())
	pi, err := startConsole(`cmd.exe /K "echo test"`, env, os.TempDir(), windows.SW_HIDE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)
	ev, _ := windows.WaitForSingleObject(pi.Process, 1500)
	windows.TerminateProcess(pi.Process, 0)
	if ev != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("cmd.exe exited at once; the terminal would flash and close")
	}
}

func TestEnvBlock(t *testing.T) {
	b := envBlock([]string{"A=1", "B=é"})
	want := []uint16{'A', '=', '1', 0, 'B', '=', 'é', 0, 0}
	if len(b) != len(want) {
		t.Fatalf("got %v", b)
	}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("got %v", b)
		}
	}
}
