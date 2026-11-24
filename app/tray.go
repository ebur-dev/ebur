package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// tray keeps the tray icon, tooltip and menu in step with the services
// (GUI-01, GUI-02).
type tray struct {
	app        *application.App
	st         *application.SystemTray
	menu       *application.Menu
	svc        *App
	openWindow func()
	quit       func()

	mu      sync.Mutex
	lastSig string
	icons   map[string][]byte
	busy    bool
}

var summaryText = map[string]string{
	"all":  "All services running",
	"some": "Some services running",
	"none": "Services stopped",
}

func newTray(app *application.App, svc *App, openWindow, quit func()) *tray {
	t := &tray{app: app, svc: svc, openWindow: openWindow, quit: quit, icons: map[string][]byte{}}
	for _, s := range []string{"all", "some", "none"} {
		t.icons[s] = statusIcon(s)
	}
	t.st = app.SystemTray.New()
	t.menu = app.NewMenu()
	t.st.SetIcon(t.icons["none"])
	t.st.SetTooltip("Ebur")
	t.st.SetMenu(t.menu)
	t.st.OnClick(openWindow)
	return t
}

// run refreshes every two seconds; actions also refresh immediately.
func (t *tray) run() {
	for {
		t.refresh()
		time.Sleep(2 * time.Second)
	}
}

func (t *tray) refresh() {
	ov, err := t.svc.Overview()
	if err != nil {
		return
	}
	summary := ov.Status.Summary
	sig := fmt.Sprintf("%s|%s|%v|%v|%v", summary, ov.PHPActive, ov.PHPInstalled, siteURLs(ov), ov.Adminer)
	t.mu.Lock()
	defer t.mu.Unlock()
	if sig == t.lastSig {
		return
	}
	t.lastSig = sig
	t.st.SetIcon(t.icons[summary])
	t.st.SetTooltip("Ebur: " + strings.ToLower(summaryText[summary]))
	t.rebuild(ov)
}

func siteURLs(ov Overview) []string {
	var out []string
	for _, s := range ov.Sites {
		out = append(out, s.URL)
	}
	return out
}

// do runs an action off the UI thread and reports failures in a dialog.
func (t *tray) do(name string, f func() error) {
	go func() {
		t.mu.Lock()
		if t.busy {
			t.mu.Unlock()
			return
		}
		t.busy = true
		t.mu.Unlock()
		err := f()
		t.mu.Lock()
		t.busy = false
		t.lastSig = "" // force a menu rebuild
		t.mu.Unlock()
		t.refresh()
		if err != nil {
			t.app.Dialog.Error().SetTitle("Ebur: " + name + " failed").SetMessage(err.Error()).Show()
		}
	}()
}

func (t *tray) rebuild(ov Overview) {
	m := t.menu
	m.Clear()
	m.Add("Ebur: " + strings.ToLower(summaryText[ov.Status.Summary])).SetEnabled(false)
	m.AddSeparator()
	if ov.Status.Summary != "all" {
		m.Add("Start all").OnClick(func(*application.Context) {
			t.do("Start", func() error { _, err := t.svc.Start(); return err })
		})
	}
	if ov.Status.Summary != "none" {
		m.Add("Stop all").OnClick(func(*application.Context) {
			t.do("Stop", func() error { _, err := t.svc.Stop(); return err })
		})
		m.Add("Restart all").OnClick(func(*application.Context) {
			t.do("Restart", func() error { _, err := t.svc.Restart(); return err })
		})
	}
	m.AddSeparator()

	phpLabel := "PHP"
	if ov.PHPActive != "" {
		phpLabel = "PHP " + ov.PHPActive
	}
	php := m.AddSubmenu(phpLabel)
	if len(ov.PHPInstalled) == 0 {
		php.Add("No PHP installed (ebur php install 8.4)").SetEnabled(false)
	}
	for _, v := range ov.PHPInstalled {
		v := v
		php.AddRadio(v, v == ov.PHPActive).OnClick(func(*application.Context) {
			if v != ov.PHPActive {
				t.do("Switch PHP", func() error { _, err := t.svc.SetPHP(v); return err })
			}
		})
	}

	open := m.AddSubmenu("Open site")
	if len(ov.Sites) == 0 {
		open.Add("No sites yet: add a folder to www").SetEnabled(false)
	}
	for _, s := range ov.Sites {
		u := s.URL
		open.Add(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")).OnClick(func(*application.Context) {
			t.do("Open site", func() error { return t.svc.OpenURL(u) })
		})
	}
	if ov.Adminer {
		scheme := "http"
		if ov.HTTPS {
			scheme = "https"
		}
		u := scheme + "://db." + ov.TLD
		open.AddSeparator()
		open.Add("Adminer (db." + ov.TLD + ")").OnClick(func(*application.Context) {
			t.do("Open Adminer", func() error { return t.svc.OpenURL(u) })
		})
	}
	m.Add("Open www folder").OnClick(func(*application.Context) {
		t.do("Open folder", t.svc.OpenWWW)
	})
	m.Add("Open terminal").OnClick(func(*application.Context) {
		t.do("Open terminal", t.svc.OpenTerminal)
	})
	m.AddSeparator()
	m.Add("Open Ebur").OnClick(func(*application.Context) { t.openWindow() })
	m.Add("Quit Ebur").OnClick(func(*application.Context) { t.quit() })
	m.Update()
}
