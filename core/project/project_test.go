package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var installed = []string{"8.5.13", "8.4.28", "8.3.37"} // newest first
const active = "8.5.13"

func site(t *testing.T, files map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for name, body := range files {
		os.WriteFile(filepath.Join(d, name), []byte(body), 0o644)
	}
	return d
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		version string
		source  string
		problem bool
	}{
		{"nothing", nil, "8.5.13", SourceDefault, false},
		{"ebur prefix", map[string]string{".ebur": "php: \"8.3\"\n"}, "8.3.37", SourceEbur, false},
		{"ebur exact", map[string]string{".ebur": "# team setup\nphp: 8.4.28 # pinned\nnode: 20\n"}, "8.4.28", SourceEbur, false},
		{"ebur missing", map[string]string{".ebur": "php: '8.1'"}, "8.5.13", SourceEbur, true},
		{"ebur wins over composer", map[string]string{".ebur": "php: 8.4", "composer.json": `{"require":{"php":"^8.3"}}`}, "8.4.28", SourceEbur, false},
		{"composer keeps default", map[string]string{"composer.json": `{"require":{"php":"^8.2"}}`}, "8.5.13", SourceComposer, false},
		{"composer needs older", map[string]string{"composer.json": `{"require":{"php":">=8.3 <8.5"}}`}, "8.4.28", SourceComposer, false},
		{"composer unmet", map[string]string{"composer.json": `{"require":{"php":"^7.4"}}`}, "8.5.13", SourceComposer, true},
		{"composer bad", map[string]string{"composer.json": `{"require":{"php":"banana"}}`}, "8.5.13", SourceDefault, true},
		{"composer no php", map[string]string{"composer.json": `{"require":{"laravel/framework":"^12"}}`}, "8.5.13", SourceDefault, false},
		{"bad ebur", map[string]string{".ebur": "this is not yaml"}, "8.5.13", SourceDefault, true},
	}
	for _, tc := range cases {
		c := Resolve(site(t, tc.files), installed, active)
		if c.Version != tc.version || c.Source != tc.source || (c.Problem != "") != tc.problem {
			t.Errorf("%s: got %+v", tc.name, c)
		}
	}
	// 7.4 is offered as a legacy version: suggest installing it.
	legacy := Resolve(site(t, map[string]string{"composer.json": `{"require":{"php":"^7.4"}}`}), installed, active)
	if !strings.Contains(legacy.Problem, "ebur php install 7.4") {
		t.Errorf("hint for ^7.4: %q", legacy.Problem)
	}
	// ^5.6 is not offered at all: the hint must say to change the
	// requirement, not suggest an install that would fail.
	unmet := Resolve(site(t, map[string]string{"composer.json": `{"require":{"php":"^5.6"}}`}), installed, active)
	if strings.Contains(unmet.Problem, "ebur php install") || !strings.Contains(unmet.Problem, "update require.php") {
		t.Errorf("hint for ^5.6: %q", unmet.Problem)
	}
	// ^8.6 can be installed: suggest exactly that.
	needs86 := Resolve(site(t, map[string]string{"composer.json": `{"require":{"php":"^8.6"}}`}), installed, active)
	if !strings.Contains(needs86.Problem, "ebur php install 8.6") {
		t.Errorf("hint for ^8.6: %q", needs86.Problem)
	}
}

func TestSetPHP(t *testing.T) {
	d := site(t, map[string]string{".ebur": "# mine\nnode: \"20\"\nphp: \"8.2\"\n"})
	if err := SetPHP(d, "8.3"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, ".ebur"))
	if got := string(b); got != "# mine\nnode: \"20\"\nphp: \"8.3\"\n" {
		t.Fatalf("got %q", got)
	}
	if err := SetPHP(d, ""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(d, ".ebur"))
	if strings.Contains(string(b), "php") || !strings.Contains(string(b), "node") {
		t.Fatalf("got %q", b)
	}

	fresh := t.TempDir()
	SetPHP(fresh, "8.4")
	if s, _ := ReadSettings(fresh); s["php"] != "8.4" {
		t.Fatalf("new file: %v", s)
	}
	SetPHP(fresh, "") // back to default: the file Ebur created is removed
	if _, err := os.Stat(filepath.Join(fresh, ".ebur")); err == nil {
		t.Fatal("empty .ebur left behind")
	}
	if err := SetPHP(fresh, "eight"); err == nil {
		t.Fatal("invalid version accepted")
	}
}

func TestVersions(t *testing.T) {
	got := Versions("8.5.13", []Choice{{Version: "8.3.37"}, {Version: "8.5.13"}, {Version: "8.4.28"}, {Version: "8.3.37"}})
	if strings.Join(got, ",") != "8.5.13,8.4.28,8.3.37" {
		t.Fatalf("%v", got)
	}
}
