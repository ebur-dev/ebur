// Spike: can a Wails v3 tray app meet NFR-02 (<= 60 MB idle with the
// window closed)? The question is whether closing the window really
// releases WebView2 or leaves its browser processes running.
//
// Usage: traymem -auto   (scripted open/close cycles, prints memory, quits)
//        traymem         (manual: use the tray menu)
package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

//go:embed assets
var assets embed.FS

var app *application.App
var win *application.WebviewWindow

func openWindow() {
	if win != nil {
		win.Show()
		return
	}
	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Ebur",
		Width:  900,
		Height: 600,
		URL:    "/",
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { win = nil })
}

func closeWindow() {
	if win != nil {
		w := win
		win = nil
		w.Close()
	}
}

func main() {
	auto := flag.Bool("auto", false, "run scripted open/close cycles and print memory")
	flag.Parse()

	app = application.New(application.Options{
		Name:   "traymem",
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})

	tray := app.SystemTray.New()
	tray.SetTooltip("Ebur memory spike")
	menu := app.NewMenu()
	menu.Add("Open window").OnClick(func(*application.Context) { openWindow() })
	menu.Add("Close window").OnClick(func(*application.Context) { closeWindow() })
	menu.Add("Print memory").OnClick(func(*application.Context) { report("manual") })
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	tray.OnClick(openWindow)

	if *auto {
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			go func() {
				time.Sleep(3 * time.Second)
				report("tray only, window never opened")
				for i := 1; i <= 3; i++ {
					application.InvokeSync(openWindow)
					time.Sleep(6 * time.Second)
					report(fmt.Sprintf("cycle %d: window open", i))
					application.InvokeSync(closeWindow)
					time.Sleep(10 * time.Second)
					report(fmt.Sprintf("cycle %d: window closed 10s", i))
				}
				app.Quit()
			}()
		})
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// ---- memory measurement: this process plus all descendants ----

type memCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

var procGetMem = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func memOf(pid uint32) (ws, priv uintptr, ok bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(h)
	var m memCounters
	m.cb = uint32(unsafe.Sizeof(m))
	r, _, _ := procGetMem.Call(uintptr(h), uintptr(unsafe.Pointer(&m)), uintptr(m.cb))
	return m.WorkingSetSize, m.PrivateUsage, r != 0
}

func report(label string) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		log.Print(err)
		return
	}
	defer windows.CloseHandle(snap)
	parent := map[uint32]uint32{}
	name := map[uint32]string{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parent[e.ProcessID] = e.ParentProcessID
		name[e.ProcessID] = windows.UTF16ToString(e.ExeFile[:])
	}
	self := uint32(windows.GetCurrentProcessId())
	inTree := func(pid uint32) bool {
		for i := 0; i < 16 && pid != 0; i++ {
			if pid == self {
				return true
			}
			pid = parent[pid]
		}
		return false
	}
	var totWS, totPriv uintptr
	n := 0
	selfWS, selfPriv, _ := memOf(self)
	for pid := range parent {
		if !inTree(pid) {
			continue
		}
		ws, priv, ok := memOf(pid)
		if !ok {
			continue
		}
		totWS += ws
		totPriv += priv
		n++
	}
	mb := func(b uintptr) float64 { return float64(b) / (1 << 20) }
	fmt.Printf("%-32s procs=%d  self ws=%.1fMB priv=%.1fMB  tree ws=%.1fMB priv=%.1fMB\n",
		label, n, mb(selfWS), mb(selfPriv), mb(totWS), mb(totPriv))
	_ = name
}
