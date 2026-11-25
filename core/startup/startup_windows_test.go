package startup

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The tests use their own key, never the real Run key.
func testKey(t *testing.T) {
	t.Helper()
	old := runKey
	runKey = `Software\EburTest-startup`
	t.Cleanup(func() {
		runKey = old
		registry.DeleteKey(registry.CURRENT_USER, `Software\EburTest-startup`)
	})
}

func TestEnableRefreshDisable(t *testing.T) {
	testKey(t)
	if Enabled() {
		t.Fatal("enabled before Enable")
	}
	if err := Disable(); err != nil {
		t.Fatalf("Disable when off: %v", err)
	}
	if err := Refresh(`C:\new\ebur-app.exe`); err != nil || Enabled() {
		t.Fatalf("Refresh turned it on: %v", err)
	}
	if err := Enable(`C:\old\ebur-app.exe`); err != nil || !Enabled() {
		t.Fatalf("Enable: %v", err)
	}
	if err := Refresh(`C:\new\ebur-app.exe`); err != nil {
		t.Fatal(err)
	}
	if v, _ := read(); v != `"C:\new\ebur-app.exe" --background` {
		t.Fatalf("after moving the folder: %q", v)
	}
	if err := Disable(); err != nil || Enabled() {
		t.Fatalf("Disable: %v", err)
	}
}
