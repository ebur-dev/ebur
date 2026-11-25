package setup

import (
	"net"
	"os"
	"strings"
	"testing"

	"github.com/ebur/ebur/core/config"
	"github.com/ebur/ebur/core/paths"
)

func TestRecommendedPHP(t *testing.T) {
	cases := map[string][]string{
		"8.5.13": {"8.6.0", "8.5.13", "8.4.28"}, // skip a brand-new x.y.0
		"8.6.1":  {"8.6.1", "8.5.13"},
		"8.6.0":  {"8.6.0"}, // nothing patched: take what there is
		"":       {},
	}
	for want, in := range cases {
		if got := RecommendedPHP(in); got != want {
			t.Errorf("RecommendedPHP(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestGetOptions(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	o, err := GetOptions(root)
	if err != nil {
		t.Fatal(err)
	}
	if !o.NeedsSetup || o.HavePHP || o.WebServer != "Nginx" {
		t.Fatalf("%+v", o)
	}
	if len(o.PHP) == 0 || o.PHPDefault == "" || len(o.DB) == 0 || o.DBDefault == "" {
		t.Fatalf("options from the manifest missing: %+v", o)
	}
	if !strings.Contains(o.DB[0].Label, "supported until") {
		t.Errorf("label %q", o.DB[0].Label)
	}
	// Once PHP is installed (e.g. with the CLI), the wizard is not needed.
	os.MkdirAll(root.RuntimeDir("php", "8.4.28"), 0o755)
	if NeedsSetup(root) {
		t.Fatal("setup needed although PHP is installed")
	}
	cfg := config.Defaults()
	cfg.SetupDone = true
	config.Save(root.Config(), cfg)
	if NeedsSetup(root) {
		t.Fatal("setup needed after it was done")
	}
}

func TestPickDBPortAvoidsBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port
	got, note := pickDBPort(busy)
	if got == busy || !strings.Contains(note, "in use") {
		t.Fatalf("got %d, note %q", got, note)
	}
	free, note := pickDBPort(got)
	if free != got || note != "" {
		t.Fatalf("free port changed: %d %q", free, note)
	}
}
