// Command winres writes the Windows icon resource (.syso) for Ebur's
// programs from build\ebur.ico. Run it from the repository root after
// changing the icon:
//
//	go run ./build/winres
//
// It writes rsrc_windows_amd64.syso into cli\, app\ and helper\, which the
// Go linker then embeds in ebur.exe, ebur-app.exe and ebur-helper.exe.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ebur-dev/ebur/internal/winres"
)

// Targets are the main packages that get the icon.
var Targets = []string{"cli", "app", "helper"}

const sysoName = "rsrc_windows_amd64.syso"

func main() {
	ico := flag.String("ico", filepath.Join("build", "ebur.ico"), "icon file")
	check := flag.Bool("check", false, "only report whether the committed .syso files are up to date")
	flag.Parse()
	b, err := os.ReadFile(*ico)
	if err != nil {
		fail(err)
	}
	syso, err := winres.SysoFromICO(b)
	if err != nil {
		fail(err)
	}
	stale := 0
	for _, dir := range Targets {
		path := filepath.Join(dir, sysoName)
		if *check {
			if old, err := os.ReadFile(path); err != nil || !bytes.Equal(old, syso) {
				fmt.Printf("%s is missing or out of date; run `go run ./build/winres`\n", path)
				stale++
			}
			continue
		}
		if err := os.WriteFile(path, syso, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(syso))
	}
	if stale > 0 {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "winres:", err)
	os.Exit(1)
}
