// Command ebur-app is the Ebur tray app and main window (SRS §4.1,
// GUI-01 to GUI-05). It embeds the same host as `ebur daemon`, so the CLI
// and the window always act on the same services.
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/ebur/ebur/core/host"
	"github.com/ebur/ebur/core/paths"
)

var version = "dev"

// The Svelte frontend is built into frontend/dist (`npm run build`).
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	host.Version = version
	root, err := paths.Find()
	if err != nil {
		log.Fatal(err)
	}
	if err := root.Ensure(); err != nil {
		log.Fatal(err)
	}
	logFile, err := os.OpenFile(filepath.Join(root.Logs(), "ebur-app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		log.SetOutput(logFile)
	}

	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	b := startBackend(root)
	svc := &App{root: root, b: b}

	// One app per Ebur root; launching it again just opens the window.
	h := sha256.Sum256([]byte(strings.ToLower(root.Dir())))
	var openWindow func()
	app := application.New(application.Options{
		Name:        "Ebur",
		Description: "Local PHP development environment",
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Windows:     application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "dev.ebur.app." + hex.EncodeToString(h[:6]),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { openWindow() },
		},
	})
	svc.app = app

	// The window is created on demand and destroyed on close, which
	// releases WebView2 and keeps idle memory low (NFR-02, spikes/traymem).
	var (
		winMu sync.Mutex
		win   *application.WebviewWindow
	)
	openWindow = func() {
		application.InvokeSync(func() {
			winMu.Lock()
			defer winMu.Unlock()
			if win != nil {
				win.Show()
				win.Restore()
				win.Focus()
				return
			}
			win = app.Window.NewWithOptions(application.WebviewWindowOptions{
				Title:            "Ebur",
				Width:            1040,
				Height:           680,
				MinWidth:         760,
				MinHeight:        480,
				URL:              "/",
				BackgroundColour: application.NewRGB(0xF6, 0xF1, 0xE4),
			})
			win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
				winMu.Lock()
				win = nil
				winMu.Unlock()
			})
		})
	}

	quitting := sync.Once{}
	quit := func() {
		quitting.Do(func() {
			go func() {
				b.shutdown() // nothing keeps running after Ebur quits (SRS §4.4)
				app.Quit()
			}()
		})
	}

	b.onHostExit = func() { app.Quit() }
	t := newTray(app, svc, openWindow, quit)
	svc.changed = t.refresh
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go t.run()
		if len(os.Args) < 2 || os.Args[1] != "--background" {
			openWindow()
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
	b.shutdown()
}
