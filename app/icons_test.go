package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Set EBUR_ICON_PREVIEW=<dir> to also save the tray icons for a look.
func TestStatusIcons(t *testing.T) {
	for _, s := range []string{"all", "some", "none"} {
		b := statusIcon(s)
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Fatalf("%s: size %v", s, img.Bounds())
		}
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Errorf("%s: corner not transparent", s)
		}
		if _, _, _, a := img.At(16, 16).RGBA(); a != 0xFFFF {
			t.Errorf("%s: centre not opaque", s)
		}
		if d := os.Getenv("EBUR_ICON_PREVIEW"); d != "" {
			os.WriteFile(filepath.Join(d, "tray-"+s+".png"), b, 0o644)
		}
	}
	if _, err := png.Decode(bytes.NewReader(appIconPNG)); err != nil {
		t.Fatal("app icon:", err)
	}
}
