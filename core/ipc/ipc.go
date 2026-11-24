// Package ipc connects the ebur CLI to the running Ebur host (the tray app
// or `ebur daemon`), which owns the service processes.
//
// Transport: a named pipe on Windows that only the current user can open,
// a Unix socket elsewhere. Protocol: one JSON request and one JSON
// response per connection, so there is no framing or session state.
package ipc

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"time"

	"github.com/ebur/ebur/core/paths"
)

const maxMessage = 1 << 20

// Request is a call from the CLI to the host.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response carries a result or an error with a suggested next step (NFR-14).
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Hint   string          `json:"hint,omitempty"`
}

// Error is returned by Call when the host reports a failure.
type Error struct{ Message, Hint string }

func (e *Error) Error() string { return e.Message }

// ErrNotRunning means no host is listening for this root.
var ErrNotRunning = errors.New("ebur host is not running")

// Handler serves one request.
type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// HintError lets a handler attach a next step to an error.
type HintError interface {
	error
	Hint() string
}

// endpoint is unique per user and per Ebur root, so a development root and
// an installed copy can run side by side.
func endpoint(root paths.Root) string {
	key := root.Dir()
	if runtime.GOOS == "windows" { // Windows paths are case-insensitive
		key = strings.ToLower(key)
	}
	h := sha256.Sum256([]byte(key))
	return "ebur-" + hex.EncodeToString(h[:6])
}

// Listen opens the host endpoint for root. It fails if another host
// already serves the same root.
func Listen(root paths.Root) (net.Listener, error) { return listen(root) }

// Serve answers requests until ln is closed.
func Serve(ctx context.Context, ln net.Listener, h Handler) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		go serveConn(ctx, conn, h)
	}
}

func serveConn(ctx context.Context, conn net.Conn, h Handler) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxMessage)).Decode(&req); err != nil {
		writeResponse(conn, Response{Error: "bad request: " + err.Error()})
		return
	}
	conn.SetReadDeadline(time.Time{})
	result, err := h(ctx, req.Method, req.Params)
	var resp Response
	if err != nil {
		resp.Error = err.Error()
		var he HintError
		if errors.As(err, &he) {
			resp.Hint = he.Hint()
		}
	} else if result != nil {
		b, merr := json.Marshal(result)
		if merr != nil {
			resp.Error = "encode result: " + merr.Error()
		} else {
			resp.Result = b
		}
	}
	writeResponse(conn, resp)
}

func writeResponse(conn net.Conn, resp Response) {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	b, _ := json.Marshal(resp)
	conn.Write(append(b, '\n'))
}

// Call sends one request to the host for root and decodes the result
// into out (which may be nil). It returns ErrNotRunning if nothing is
// listening, and *Error if the host reports a failure.
func Call(ctx context.Context, root paths.Root, method string, params, out any) error {
	conn, err := dial(ctx, root)
	if err != nil {
		return ErrNotRunning
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		conn.SetDeadline(d)
	}
	req := Request{Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = b
	}
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("send to ebur host: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(conn, maxMessage)).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("read from ebur host: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("bad reply from ebur host: %w", err)
	}
	if resp.Error != "" {
		return &Error{Message: resp.Error, Hint: resp.Hint}
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}
