package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const sha = "38808a798485e1c0938dfcce7e942131415fc6c7834f83a5a85a4c4edb5539d6"

func doc(body string) []byte {
	return []byte(`{"schema":1,"generated":"2026-11-24T00:00:00Z","runtimes":` + body + `}`)
}

func rel(v string) string {
	return `{"version":"` + v + `","url":"https://example.org/` + v + `.zip","sha256":"` + sha + `","arch":"x64"}`
}

func TestParseAndResolve(t *testing.T) {
	m, err := Parse(doc(`{"php":[` + rel("8.3.37") + `,` + rel("8.4.28") + `,` + rel("8.4.9") + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"": "8.4.28", "8.4": "8.4.28", "8.3": "8.3.37", "8.4.9": "8.4.9", "8": "8.4.28"}
	for want, got := range cases {
		r, err := m.Resolve("php", want, "x64")
		if err != nil || r.Version != got {
			t.Errorf("Resolve(%q) = %q, %v; want %q", want, r.Version, err, got)
		}
	}
	// "8.4.2" must not match 8.4.28 by string prefix.
	if _, err := m.Resolve("php", "8.4.2", "x64"); err == nil || !strings.Contains(err.Error(), "available") {
		t.Errorf("want not-found error listing versions, got %v", err)
	}
	if _, err := m.Resolve("ruby", "", "x64"); err == nil {
		t.Error("unknown runtime resolved")
	}
	if _, err := m.Resolve("php", "", "arm64"); err == nil || !strings.Contains(err.Error(), "arm64") {
		t.Errorf("want arch error, got %v", err)
	}
}

func TestRejects(t *testing.T) {
	bad := map[string]string{
		"schema":    `{"schema":2,"runtimes":{"php":[` + rel("8.4.1") + `]}}`,
		"empty":     `{"schema":1,"runtimes":{}}`,
		"unknown":   `{"schema":1,"extra":true,"runtimes":{"php":[` + rel("8.4.1") + `]}}`,
		"name":      string(doc(`{"PHP!":[` + rel("8.4.1") + `]}`)),
		"version":   string(doc(`{"php":[` + rel("8.x") + `]}`)),
		"http":      string(doc(`{"php":[{"version":"8.4.1","url":"http://e.org/a.zip","sha256":"` + sha + `","arch":"x64"}]}`)),
		"sha short": string(doc(`{"php":[{"version":"8.4.1","url":"https://e.org/a.zip","sha256":"abc","arch":"x64"}]}`)),
		"sha upper": string(doc(`{"php":[{"version":"8.4.1","url":"https://e.org/a.zip","sha256":"` + strings.ToUpper(sha) + `","arch":"x64"}]}`)),
		"arch":      string(doc(`{"php":[{"version":"8.4.1","url":"https://e.org/a.zip","sha256":"` + sha + `","arch":"x86"}]}`)),
		"root":      string(doc(`{"php":[{"version":"8.4.1","url":"https://e.org/a.zip","sha256":"` + sha + `","arch":"x64","root":"../x"}]}`)),
		"duplicate": string(doc(`{"php":[` + rel("8.4.1") + `,` + rel("8.4.1") + `]}`)),
	}
	for name, body := range bad {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"8.4.28", "8.4.9", 1}, {"8.4", "8.4.0", 0}, {"1.30.8", "1.31.0", -1},
		{"bad", "1.0", -1}, {"1.0", "bad", 1}, {"a", "b", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := ParseVersion("08.1"); err == nil {
		t.Error("leading zero accepted")
	}
}

// The manifest shipped in the repo must always parse.
func TestRepoManifest(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "manifest", "runtimes.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"php", "nginx"} {
		if _, err := m.Resolve(name, "", "x64"); err != nil {
			t.Error(err)
		}
	}
}
