package shellenv

import (
	"os"
	"path/filepath"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ebur-dev/ebur/core/paths"
)

// Open starts cmd.exe in its own console window with Ebur's environment,
// in workDir (GUI-04, `ebur terminal`).
func Open(root paths.Root, workDir string) error {
	env := For(root)
	greet := "Ebur terminal. On PATH: " + env.Summary()
	cmdline := `cmd.exe /K "title Ebur terminal && echo ` + greet + ` && echo."`
	pi, err := startConsole(cmdline, env.Environ(os.Environ()), workDir, windows.SW_SHOWNORMAL)
	if err != nil {
		return err
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return nil
}

// startConsole creates a process with a new console through CreateProcess
// directly. os/exec always passes standard handles (NUL when none are
// given), so an interactive cmd.exe read end-of-file from NUL and exited
// at once; the console window flashed and closed. Without
// STARTF_USESTDHANDLES the process uses its new console's own input.
func startConsole(cmdline string, environ []string, dir string, show uint16) (*windows.ProcessInformation, error) {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	app, err := windows.UTF16PtrFromString(comspec)
	if err != nil {
		return nil, err
	}
	cl, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return nil, err
	}
	var wd *uint16
	if dir != "" {
		if wd, err = windows.UTF16PtrFromString(dir); err != nil {
			return nil, err
		}
	}
	block := envBlock(environ)
	si := windows.StartupInfo{Flags: windows.STARTF_USESHOWWINDOW, ShowWindow: show}
	si.Cb = uint32(unsafe.Sizeof(si))
	pi := new(windows.ProcessInformation)
	err = windows.CreateProcess(app, cl, nil, nil, false,
		windows.CREATE_NEW_CONSOLE|windows.CREATE_UNICODE_ENVIRONMENT, &block[0], wd, &si, pi)
	if err != nil {
		return nil, err
	}
	return pi, nil
}

// envBlock encodes "K=V" strings as a Windows environment block: each
// entry NUL-terminated, the block ending with an extra NUL.
func envBlock(environ []string) []uint16 {
	var b []uint16
	for _, kv := range environ {
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	return append(b, 0)
}
