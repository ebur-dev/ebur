package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/ebur/ebur/core/host"
	"github.com/ebur/ebur/core/ipc"
	"github.com/ebur/ebur/core/paths"
)

// backend reaches the Ebur host. Normally the app embeds the host, so the
// services live and die with the tray app (SRS §4.4) and the CLI talks to
// it over IPC. If a host started by the CLI is already running, the app
// becomes a client of it instead, so there is never more than one.
type backend struct {
	root   paths.Root
	h      *host.Host // nil when using an external host
	cancel context.CancelFunc
	done   chan struct{}
	// onHostExit runs when the embedded host stops on its own, e.g. after
	// `ebur quit` from a terminal, so the tray app does not linger.
	onHostExit func()
}

func startBackend(root paths.Root) *backend {
	b := &backend{root: root}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if ipc.Call(ctx, root, "ping", nil, nil) == nil {
		log.Printf("using the Ebur host already running for %s", root.Dir())
		return b
	}
	b.h = host.New(root)
	runCtx, runCancel := context.WithCancel(context.Background())
	b.cancel, b.done = runCancel, make(chan struct{})
	go func() {
		err := b.h.Run(runCtx)
		close(b.done)
		if err != nil {
			log.Printf("host: %v", err)
			return
		}
		if runCtx.Err() == nil && b.onHostExit != nil { // not stopped by us
			log.Printf("host shut down (ebur quit); exiting the app")
			b.onHostExit()
		}
	}()
	return b
}

// call runs one host method, in-process or over IPC.
func (b *backend) call(method string, params, out any) error {
	if b.h == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		err := ipc.Call(ctx, b.root, method, params, out)
		if errors.Is(err, ipc.ErrNotRunning) {
			return errors.New("the Ebur host stopped; restart the Ebur app")
		}
		return err
	}
	var raw json.RawMessage
	if params != nil {
		var err error
		if raw, err = json.Marshal(params); err != nil {
			return err
		}
	}
	res, err := b.h.Handle(context.Background(), method, raw)
	if err != nil {
		return err
	}
	if out != nil && res != nil {
		j, err := json.Marshal(res)
		if err != nil {
			return err
		}
		return json.Unmarshal(j, out)
	}
	return nil
}

// shutdown stops every service: the embedded host's, or the external
// host's, since quitting Ebur must leave nothing running.
func (b *backend) shutdown() {
	if b.h == nil {
		b.call("shutdown", nil, nil)
		return
	}
	b.cancel()
	select {
	case <-b.done:
	case <-time.After(20 * time.Second):
		log.Printf("services did not stop within 20s")
	}
}
