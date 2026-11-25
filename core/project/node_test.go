package project

import (
	"strings"
	"testing"
)

func TestResolveNode(t *testing.T) {
	nodes := []string{"24.22.3", "22.23.5"} // newest first
	const def = "24.22.3"
	cases := []struct {
		name    string
		files   map[string]string
		version string
		source  string
		problem string // substring; "" means no problem
	}{
		{"nothing", nil, def, SourceDefault, ""},
		{"ebur", map[string]string{".ebur": "php: 8.4\nnode: 22\n"}, "22.23.5", SourceEbur, ""},
		{"ebur v-prefix", map[string]string{".ebur": "node: v22"}, "22.23.5", SourceEbur, ""},
		{"ebur missing", map[string]string{".ebur": "node: 26"}, def, SourceEbur, "ebur node install 26"},
		{"ebur wins", map[string]string{".ebur": "node: 22", ".nvmrc": "24"}, "22.23.5", SourceEbur, ""},
		{"nvmrc major", map[string]string{".nvmrc": "22\n"}, "22.23.5", SourceNvmrc, ""},
		{"nvmrc exact patch", map[string]string{".nvmrc": "v22.11.0"}, "22.23.5", SourceNvmrc, ""},
		{"nvmrc codename", map[string]string{".nvmrc": "lts/jod"}, "22.23.5", SourceNvmrc, ""},
		{"nvmrc lts star", map[string]string{".nvmrc": "lts/*"}, def, SourceNvmrc, ""},
		{"node-version file", map[string]string{".node-version": "22.1"}, "22.23.5", ".node-version", ""},
		{"nvmrc legacy", map[string]string{".nvmrc": "20"}, def, SourceNvmrc, "ebur node install 20"},
		{"nvmrc nonsense", map[string]string{".nvmrc": "banana"}, def, SourceNvmrc, "does not understand"},
		{"nvmrc beats package", map[string]string{".nvmrc": "22", "package.json": `{"engines":{"node":">=24"}}`}, "22.23.5", SourceNvmrc, ""},
		{"engines keeps default", map[string]string{"package.json": `{"engines":{"node":">=20.0.0"}}`}, def, SourcePackage, ""},
		{"engines or", map[string]string{"package.json": `{"engines":{"node":"^20.19 || ^22.12"}}`}, "22.23.5", SourcePackage, ""},
		{"engines x", map[string]string{"package.json": `{"engines":{"node":"22.x"}}`}, "22.23.5", SourcePackage, ""},
		{"engines unmet", map[string]string{"package.json": `{"engines":{"node":"^26"}}`}, def, SourcePackage, "ebur node install 26"},
		{"engines bad", map[string]string{"package.json": `{"engines":{"node":"pizza"}}`}, def, SourceDefault, "not understood"},
		{"no engines", map[string]string{"package.json": `{"scripts":{"dev":"vite"}}`}, def, SourceDefault, ""},
	}
	for _, tc := range cases {
		c := ResolveNode(site(t, tc.files), nodes, def)
		if c.Version != tc.version || c.Source != tc.source || (tc.problem == "") != (c.Problem == "") || !strings.Contains(c.Problem, tc.problem) {
			t.Errorf("%s: got %+v", tc.name, c)
		}
	}
	// No Node installed: a project's request is reported, not silently ignored.
	none := ResolveNode(site(t, map[string]string{".nvmrc": "24"}), nil, "")
	if none.Version != "" || !strings.Contains(none.Problem, "ebur node install 24") {
		t.Errorf("no node: %+v", none)
	}
}

func TestSetNode(t *testing.T) {
	dir := site(t, map[string]string{".ebur": "php: \"8.4\"\n"})
	if err := SetNode(dir, "22"); err != nil {
		t.Fatal(err)
	}
	s, _ := ReadSettings(dir)
	if s["php"] != "8.4" || s["node"] != "22" {
		t.Fatalf("settings %v", s)
	}
	if err := SetNode(dir, ""); err != nil {
		t.Fatal(err)
	}
	if s, _ = ReadSettings(dir); s["node"] != "" || s["php"] != "8.4" {
		t.Fatalf("after clearing: %v", s)
	}
	if err := SetNode(dir, "lts"); err == nil {
		t.Fatal("invalid version accepted")
	}
}
