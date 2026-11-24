package hostsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = "# Copyright (c) Microsoft\r\n127.0.0.1 localhost\r\n10.0.0.5 intranet.corp\r\n\r\n"

func TestValidHost(t *testing.T) {
	for _, ok := range []string{"shop.test", "api.shop.test", "db.test", "app.localhost", "a-b.test"} {
		if !ValidHost(ok) {
			t.Errorf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"bank.com", "test", "shop.test.com", "Shop.test", "-a.test", "a_b.test", "shop.test ", "x\n127.0.0.1 bank.com", "", "a..test"} {
		if ValidHost(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRewriteAddsReplacesRemoves(t *testing.T) {
	out, err := Rewrite(sample, []string{"shop.test", "blog.test", "shop.test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# Copyright (c) Microsoft\r\n127.0.0.1 localhost\r\n10.0.0.5 intranet.corp\r\n") {
		t.Fatalf("other lines changed:\n%q", out)
	}
	if !strings.Contains(out, "\r\n"+BeginMark+"\r\n127.0.0.1 blog.test\r\n127.0.0.1 shop.test\r\n"+EndMark+"\r\n") {
		t.Fatalf("block wrong or not CRLF:\n%q", out)
	}
	if got := Managed(out); strings.Join(got, ",") != "blog.test,shop.test" {
		t.Fatalf("managed %v", got)
	}
	out2, _ := Rewrite(out, []string{"db.test"})
	if got := Managed(out2); strings.Join(got, ",") != "db.test" || strings.Count(out2, "# ebur begin") != 1 {
		t.Fatalf("replace failed:\n%s", out2)
	}
	cleared, _ := Rewrite(out2, nil)
	if strings.Contains(cleared, "ebur") || !strings.Contains(cleared, "intranet.corp") {
		t.Fatalf("clear failed:\n%q", cleared)
	}
	again, _ := Rewrite(out, []string{"blog.test", "shop.test"})
	if again != out {
		t.Fatal("rewriting with the same names changed the file")
	}
}

func TestRewriteRefuses(t *testing.T) {
	if _, err := Rewrite(sample, []string{"bank.com"}); err == nil {
		t.Fatal("public name accepted")
	}
	if _, err := Rewrite(sample+"# ebur begin\r\n127.0.0.1 a.test\r\n", []string{"a.test"}); err == nil {
		t.Fatal("unterminated block accepted")
	}
	many := make([]string, maxHosts+1)
	for i := range many {
		many[i] = "s" + strings.Repeat("x", i%40) + string(rune('a'+i%26)) + ".test"
	}
	if _, err := Rewrite(sample, many); err == nil {
		t.Fatal("too many names accepted")
	}
}

func TestApplyAndBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(p, []byte(sample), 0o644)
	if err := Apply(p, []string{"shop.test"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if got := Managed(string(b)); len(got) != 1 || got[0] != "shop.test" {
		t.Fatalf("%v", got)
	}
	backup, err := os.ReadFile(p + ".ebur-backup")
	if err != nil || string(backup) != sample {
		t.Fatal("backup of the original missing")
	}
	Apply(p, []string{"blog.test"})
	if backup2, _ := os.ReadFile(p + ".ebur-backup"); string(backup2) != sample {
		t.Fatal("backup overwritten by a later edit")
	}
	if !Mapped(string(mustRead(t, p)))["blog.test"] || !Mapped(sample)["localhost"] {
		t.Fatal("Mapped wrong")
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEqual(t *testing.T) {
	if !Equal([]string{"b.test", "a.test", "a.test"}, []string{"a.test", "b.test"}) {
		t.Fatal("order/duplicates should not matter")
	}
	if Equal([]string{"a.test"}, []string{"a.test", "b.test"}) || !Equal(nil, []string{}) {
		t.Fatal("Equal wrong")
	}
}
