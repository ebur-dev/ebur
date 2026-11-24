package php

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureINI(t *testing.T) {
	d := t.TempDir()
	phpDir := filepath.Join(d, "bin", "php", "8.4.28")
	os.MkdirAll(filepath.Join(phpDir, "ext"), 0o755)
	os.WriteFile(filepath.Join(phpDir, "php.ini-development"), []byte("memory_limit = 128M\n"), 0o644)
	os.WriteFile(filepath.Join(phpDir, "ext", "php_curl.dll"), nil, 0o644)
	etc, tmp := filepath.Join(d, "etc"), filepath.Join(d, "data", "tmp")

	p, err := EnsureINI(etc, phpDir, "8.4.28", tmp)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"memory_limit = 128M", "memory_limit = 512M", "extension = curl", "extension_dir", marker} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "extension = gd") {
		t.Error("enabled an extension whose DLL is missing")
	}

	// User edits survive a second call.
	os.WriteFile(p, []byte("user edit\n"), 0o644)
	if _, err := EnsureINI(etc, phpDir, "8.4.28", tmp); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "user edit\n" {
		t.Fatal("existing php.ini was overwritten")
	}
}

func TestEnsureINIMissingTemplate(t *testing.T) {
	d := t.TempDir()
	if _, err := EnsureINI(filepath.Join(d, "etc"), filepath.Join(d, "nophp"), "8.4.28", filepath.Join(d, "tmp")); err == nil {
		t.Fatal("expected error")
	}
}
