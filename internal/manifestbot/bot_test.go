package manifestbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func rel(v string) manifest.Release {
	return manifest.Release{Version: v, URL: "https://example.org/" + v + ".zip", SHA256: strings.Repeat("a", 64), Arch: "x64"}
}

func cand(rt, v string, age time.Duration, security, newOK bool) Candidate {
	return Candidate{Runtime: rt, Series: SeriesOf(rt, v), Release: rel(v), Released: now.Add(-age), Security: security, NewOK: newOK}
}

func TestApplyRules(t *testing.T) {
	day := 24 * time.Hour
	old := rel("24.22.3")
	old.EOL = "2028-04-30"
	m := &manifest.Manifest{Runtimes: map[string][]manifest.Release{
		"node":     {rel("26.13.1"), old, rel("22.23.5")},
		"composer": {rel("2.11.3")},
	}}
	applied, waiting := Apply(m, []Candidate{
		cand("node", "24.22.4", 20*day, false, true),  // old enough: replaces 24.22.3
		cand("node", "26.14.0", 3*day, false, true),   // too new: waits
		cand("node", "22.24.0", 2*day, true, true),    // security: taken now
		cand("node", "28.1.0", 30*day, false, true),   // new LTS line: added
		cand("node", "18.20.9", 30*day, false, false), // ended line: not added
		cand("composer", "2.11.4", 1*day, false, false),
		cand("composer", "2.11.2", 99*day, false, false), // older than what we have
	}, now)

	got := map[string]string{}
	for _, c := range applied {
		got[c.Runtime+" "+c.Series] = c.From + "->" + c.To
	}
	want := map[string]string{"node 24": "24.22.3->24.22.4", "node 22": "22.23.5->22.24.0", "node 28": "->28.1.0"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("applied %v, want %v", got, want)
	}
	waits := map[string]string{}
	for _, c := range waiting {
		waits[c.Runtime+" "+c.Series] = c.To + " on " + c.WaitUntil.Format("2006-01-02")
	}
	if waits["node 26"] != "26.14.0 on 2026-12-06" || waits["composer "] != "2.11.4 on 2026-12-08" || len(waits) != 2 {
		t.Fatalf("waiting %v", waits)
	}
	var versions []string
	for _, r := range m.Runtimes["node"] {
		versions = append(versions, r.Version)
	}
	if strings.Join(versions, ",") != "28.1.0,26.13.1,24.22.4,22.24.0" {
		t.Fatalf("node now %v", versions)
	}
	if m.Runtimes["node"][2].EOL != "2028-04-30" {
		t.Error("known end-of-life date lost on update")
	}
	if m.Runtimes["composer"][0].Version != "2.11.3" {
		t.Error("composer changed before its 14 days")
	}
}

func TestNothingNewChangesNothing(t *testing.T) {
	m := &manifest.Manifest{Runtimes: map[string][]manifest.Release{"mailpit": {rel("1.34.0")}}}
	applied, waiting := Apply(m, []Candidate{cand("mailpit", "1.34.0", 40*24*time.Hour, false, false)}, now)
	if len(applied)+len(waiting) != 0 {
		t.Fatalf("%v %v", applied, waiting)
	}
}

// fake serves canned bodies by URL.
type fake map[string]string

func (f fake) get(_ context.Context, url string) ([]byte, error) {
	if b, ok := f[url]; ok {
		return []byte(b), nil
	}
	return nil, fmt.Errorf("%s: 404 Not Found", url)
}

func TestNodeSource(t *testing.T) {
	sha := strings.Repeat("b", 64)
	f := fake{
		"https://nodejs.org/dist/index.json": `[
			{"version":"v27.2.0","date":"2026-11-01","files":["win-x64-zip"],"lts":false,"security":false},
			{"version":"v26.14.0","date":"2026-11-20","files":["win-x64-zip"],"lts":"Lithium","security":true},
			{"version":"v26.13.1","date":"2026-11-10","files":["win-x64-zip","linux-x64"],"lts":"Lithium","security":false},
			{"version":"v18.20.9","date":"2026-01-01","files":["win-x64-zip"],"lts":"Hydrogen","security":false}]`,
		"https://endoflife.date/api/nodejs.json":          `[{"cycle":"26","eol":"2029-04-30","latest":"26.14.0"},{"cycle":"18","eol":"2025-04-30","latest":"18.20.9"}]`,
		"https://nodejs.org/dist/v26.14.0/SHASUMS256.txt": sha + "  node-v26.14.0-win-x64.zip\n" + strings.Repeat("c", 64) + "  node-v26.14.0-linux-x64.tar.xz\n",
		"https://nodejs.org/dist/v26.13.1/SHASUMS256.txt": strings.Repeat("d", 64) + "  node-v26.13.1-win-x64.zip\n",
		"https://nodejs.org/dist/v18.20.9/SHASUMS256.txt": strings.Repeat("e", 64) + "  node-v18.20.9-win-x64.zip\n",
	}
	cands, _, err := Node{Now: now}.Find(context.Background(), f.get)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 3 {
		t.Fatalf("want 3 LTS candidates (27 is not LTS), got %d", len(cands))
	}
	c := cands[0]
	if c.Release.Version != "26.14.0" || c.Release.SHA256 != sha || !c.Security || c.Release.Root != "node-v26.14.0-win-x64" || c.Release.EOL != "2029-04-30" || !c.NewOK {
		t.Fatalf("candidate %+v", c)
	}
	if cands[2].NewOK {
		t.Error("an ended LTS line may be added")
	}
}

func TestGitHubSource(t *testing.T) {
	sha := strings.Repeat("f", 64)
	body, _ := json.Marshal([]map[string]any{
		{"tag_name": "v1.35.0", "published_at": "2026-11-20T10:00:00Z", "body": "Fixes GHSA-abcd-1234-wxyz", "draft": false, "prerelease": false,
			"assets": []map[string]string{{"name": "mailpit-windows-amd64.zip", "digest": "sha256:" + sha}}},
		{"tag_name": "v1.36.0-beta", "published_at": "2026-11-21T10:00:00Z", "prerelease": true, "assets": []map[string]string{}},
		{"tag_name": "v1.34.0", "published_at": "2026-10-01T10:00:00Z", "body": "Features", "assets": []map[string]string{{"name": "other.zip", "digest": "sha256:" + sha}}},
	})
	f := fake{"https://api.github.com/repos/axllent/mailpit/releases?per_page=15": string(body)}
	g := GitHub{Runtime: "mailpit", Repo: "axllent/mailpit", Asset: func(string) string { return "mailpit-windows-amd64.zip" }}
	cands, _, err := g.Find(context.Background(), f.get)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Release.Version != "1.35.0" || !cands[0].Security ||
		cands[0].Release.URL != "https://github.com/axllent/mailpit/releases/download/v1.35.0/mailpit-windows-amd64.zip" {
		t.Fatalf("candidates %+v", cands)
	}
}

func TestRunReportsSourceErrors(t *testing.T) {
	m := &manifest.Manifest{Runtimes: map[string][]manifest.Release{}}
	rep := Run(context.Background(), m, []Source{Composer{}}, fake{}.get, now)
	if len(rep.Errors) != 1 || rep.Changed() || !strings.Contains(rep.Markdown(), "could not be checked") {
		t.Fatalf("%+v", rep)
	}
}
