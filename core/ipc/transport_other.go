//go:build !windows

package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ebur-dev/ebur/core/paths"
)

// Unix socket paths are limited to about 100 bytes, so use the temp dir.
func socketPath(root paths.Root) string {
	return filepath.Join(os.TempDir(), endpoint(root)+".sock")
}

func listen(root paths.Root) (net.Listener, error) {
	p := socketPath(root)
	if c, err := net.Dial("unix", p); err == nil {
		c.Close()
		return nil, errors.New("another Ebur host is already running for " + root.Dir())
	}
	os.Remove(p) // stale socket from a crash
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	return net.Listen("unix", p)
}

func dial(ctx context.Context, root paths.Root) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", socketPath(root))
}
