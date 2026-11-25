package sites

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// makeLink creates a directory junction, which needs no administrator
// rights or Developer Mode (unlike a symbolic link).
func makeLink(link, target string) error {
	cmd := exec.Command("cmd.exe")
	// Quote each path for cmd; mklink takes the link first.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    fmt.Sprintf(`cmd.exe /c mklink /J "%s" "%s"`, link, target),
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("could not create the link: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// longPath expands 8.3 short names (C:\Users\JOHNSM~1) so paths compare.
// Windows only expands paths that exist, so the longest existing parent
// is expanded and the rest appended.
func longPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for dir := p; ; {
		if l, ok := expand(dir); ok {
			return filepath.Join(l, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

func expand(p string) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

func samePath(a, b string) bool { return strings.EqualFold(a, b) }
