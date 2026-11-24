package ipc

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/ebur/ebur/core/paths"
)

func pipePath(root paths.Root) string { return `\\.\pipe\` + endpoint(root) }

func listen(root paths.Root) (net.Listener, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	// Protected DACL granting full access to the current user only: other
	// accounts on the machine cannot control this user's services.
	ln, err := winio.ListenPipe(pipePath(root), &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)", sid),
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("another Ebur host may already be running for %s: %w", root.Dir(), err)
	}
	return ln, nil
}

func dial(ctx context.Context, root paths.Root) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipePath(root))
}

func currentUserSID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return tu.User.Sid.String(), nil
}
