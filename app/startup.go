package main

import (
	"context"
	"log"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/host"
	"github.com/ebur-dev/ebur/core/ipc"
	"github.com/ebur-dev/ebur/core/startup"
)

// startOnLaunch starts every service when the tray app opens (SVC-06), so
// sites work again after a restart without clicking Start all. It does not
// touch the hosts file: opening Ebur must never show an administrator
// prompt by itself. An explicit auto_start list is started by the host
// instead (host.Run).
func (a *App) startOnLaunch() {
	if app, err := startup.AppNextTo(); err == nil {
		if err := startup.Refresh(app); err != nil { // the folder may have moved
			log.Printf("sign-in entry: %v", err)
		}
	}
	cfg, err := config.Load(a.root.Config())
	if err != nil || !cfg.SetupDone || !cfg.StartsServices() || len(cfg.AutoStart) > 0 {
		return // the wizard starts services itself on a new install
	}
	// The embedded host starts listening a moment after the app does.
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := ipc.Call(ctx, a.root, "ping", nil, nil)
		cancel()
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	var st host.Status
	if err := a.b.call("start", host.Selection{}, &st); err != nil {
		// The tray and window show what is stopped; Start all shows the
		// full message with its next step.
		log.Printf("start on launch: %v", err)
	} else {
		log.Printf("started services on launch (%s)", st.Summary)
	}
	if a.changed != nil {
		a.changed()
	}
}

// Startup is the Settings card's state.
type Startup struct {
	StartServices bool `json:"startServices"` // start services when Ebur opens
	OpenAtSignIn  bool `json:"openAtSignIn"`  // open Ebur when the user signs in to Windows
}

// StartupSettings returns both switches.
func (a *App) StartupSettings() Startup {
	cfg, _ := config.Load(a.root.Config())
	return Startup{StartServices: cfg.StartsServices(), OpenAtSignIn: startup.Enabled()}
}

// SetStartServices turns starting services on launch on or off.
func (a *App) SetStartServices(on bool) (Startup, error) {
	cfg, err := config.Load(a.root.Config())
	if err != nil {
		return a.StartupSettings(), withHint(err)
	}
	cfg.StartServices = &on
	if err := config.Save(a.root.Config(), cfg); err != nil {
		return a.StartupSettings(), err
	}
	return a.StartupSettings(), nil
}

// SetOpenAtSignIn adds or removes Ebur from the programs Windows opens at
// sign-in (per user, no administrator prompt).
func (a *App) SetOpenAtSignIn(on bool) (Startup, error) {
	var err error
	if on {
		var app string
		if app, err = startup.AppNextTo(); err == nil {
			err = startup.Enable(app)
		}
	} else {
		err = startup.Disable()
	}
	return a.StartupSettings(), err
}
