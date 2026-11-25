package share

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/paths"
)

func TestSaveLoadClear(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	if s, err := Load(root); err != nil || len(s) != 0 {
		t.Fatalf("empty: %v %v", s, err)
	}
	now := time.Now().Truncate(time.Second)
	in := []Share{{Site: "shop", Port: 8181, Expires: now.Add(time.Hour), User: "guest", Password: "abcd-efgh-ijkm"}, {Site: "blog", Port: 8180, Expires: now}}
	if err := Save(root, in); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(HtpasswdFile(root, "shop"), []byte("x"), 0o600)
	out, err := Load(root)
	if err != nil || len(out) != 2 || out[0].Site != "blog" || out[1].Password != "abcd-efgh-ijkm" || !out[1].Expires.Equal(in[0].Expires) {
		t.Fatalf("load: %+v %v", out, err)
	}
	if _, i := Find(out, "shop"); i != 1 {
		t.Error("find")
	}
	if exp := Expired(out, now); len(exp) != 1 || exp[0].Site != "blog" {
		t.Errorf("expired: %+v", exp)
	}
	if err := Clear(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(File(root)); !os.IsNotExist(err) {
		t.Error("state file kept")
	}
	if _, err := os.Stat(HtpasswdFile(root, "shop")); !os.IsNotExist(err) {
		t.Error("password file kept")
	}
}

func TestPickPort(t *testing.T) {
	p, err := PickPort([]Share{{Port: 8180}}, func(p int) bool { return p != 8181 })
	if err != nil || p != 8182 {
		t.Fatalf("got %d %v", p, err)
	}
	if _, err := PickPort(nil, func(int) bool { return false }); err == nil {
		t.Error("no free port should fail")
	}
}

func TestPasswordAndHtpasswd(t *testing.T) {
	pw, err := NewPassword()
	if err != nil || len(pw) != 14 || strings.Count(pw, "-") != 2 || strings.ContainsAny(pw, "lo01") {
		t.Fatalf("password %q %v", pw, err)
	}
	line, err := Htpasswd("guest", pw)
	if err != nil {
		t.Fatal(err)
	}
	user, rest, _ := strings.Cut(strings.TrimSpace(line), ":")
	if user != "guest" || !strings.HasPrefix(rest, "{SSHA}") {
		t.Fatalf("line %q", line)
	}
	// Verify the way Nginx does: SHA-1(password + salt) followed by the salt.
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(rest, "{SSHA}"))
	if err != nil || len(raw) != 28 {
		t.Fatalf("decode: %v (%d bytes)", err, len(raw))
	}
	sum := sha1.Sum(append([]byte(pw), raw[20:]...))
	if !bytes.Equal(sum[:], raw[:20]) {
		t.Error("hash does not verify")
	}
	wrong := sha1.Sum(append([]byte("nope"), raw[20:]...))
	if bytes.Equal(wrong[:], raw[:20]) {
		t.Error("wrong password verifies")
	}
}

const sampleLog = `2026-11-25T10:00:00Z INF Thank you for trying Cloudflare Tunnel.
2026-11-25T10:00:01Z INF +--------------------------------------------------------------------------------------------+
2026-11-25T10:00:01Z INF |  https://quiet-river-tables-moon.trycloudflare.com                                          |
2026-11-25T10:00:01Z INF +--------------------------------------------------------------------------------------------+
`

func TestLogParsing(t *testing.T) {
	if URLFromLog("starting...") != "" || Connected("starting...") {
		t.Error("no address yet")
	}
	if u := URLFromLog(sampleLog); u != "https://quiet-river-tables-moon.trycloudflare.com" {
		t.Errorf("url %q", u)
	}
	if Connected(sampleLog) {
		t.Error("not registered yet")
	}
	reg := sampleLog + "2026-11-25T10:00:03Z INF Registered tunnel connection connIndex=0 location=fra08 protocol=quic\n"
	if !Connected(reg) {
		t.Error("registered connection missed")
	}
	// After a restart the new address wins, and an old registration does not count.
	restarted := reg + strings.ReplaceAll(sampleLog, "quiet-river-tables-moon", "bright-new-words-here")
	if u := URLFromLog(restarted); !strings.Contains(u, "bright-new-words-here") || Connected(restarted) {
		t.Errorf("after restart: %q connected=%v", u, Connected(restarted))
	}
	if got := LastLines("a\n\nb\nc\n", 2); got != "b\nc" {
		t.Errorf("last lines %q", got)
	}
}

func TestWordPressPlugin(t *testing.T) {
	plain := t.TempDir()
	if wrote, err := EnsureWordPressPlugin(plain); wrote || err != nil {
		t.Fatal("non-WordPress site changed")
	}
	if _, err := os.Stat(filepath.Join(plain, "wp-content")); !os.IsNotExist(err) {
		t.Fatal("folder created in a non-WordPress site")
	}
	wp := t.TempDir()
	os.MkdirAll(filepath.Join(wp, "wp-includes"), 0o755)
	os.WriteFile(filepath.Join(wp, "wp-includes", "version.php"), []byte("<?php"), 0o644)
	if wrote, err := EnsureWordPressPlugin(wp); !wrote || err != nil {
		t.Fatalf("first: %v %v", wrote, err)
	}
	b, _ := os.ReadFile(filepath.Join(wp, "wp-content", "mu-plugins", PluginName))
	if !strings.Contains(string(b), "EBUR_SHARE") || !strings.HasPrefix(string(b), "<?php") {
		t.Fatal("plugin content")
	}
	if wrote, _ := EnsureWordPressPlugin(wp); wrote {
		t.Error("rewrote an up-to-date plugin")
	}
}

func TestEmptyConfig(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	p, err := EmptyConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.HasPrefix(string(b), "#") {
		t.Errorf("content %q", b)
	}
}
