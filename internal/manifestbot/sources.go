package manifestbot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
)

// eolDates reads endoflife.date's cycles for a product: cycle -> end date
// (YYYY-MM-DD), "" when it has none yet.
func eolDates(ctx context.Context, get Getter, product string) (map[string]string, map[string]string, error) {
	b, err := get(ctx, "https://endoflife.date/api/"+product+".json")
	if err != nil {
		return nil, nil, err
	}
	var cycles []struct {
		Cycle  string `json:"cycle"`
		EOL    any    `json:"eol"`
		Latest string `json:"latest"`
	}
	if err := json.Unmarshal(b, &cycles); err != nil {
		return nil, nil, fmt.Errorf("endoflife.date %s: %w", product, err)
	}
	eol, latest := map[string]string{}, map[string]string{}
	for _, c := range cycles {
		if s, ok := c.EOL.(string); ok {
			eol[c.Cycle] = s
		}
		latest[c.Cycle] = c.Latest
	}
	return eol, latest, nil
}

func ended(eol string, now time.Time) bool {
	t, err := time.Parse("2006-01-02", eol)
	return err == nil && now.After(t)
}

// Node: LTS lines only (odd majors are never LTS), from nodejs.org's
// index; checksums from each release's SHASUMS256.txt.
type Node struct{ Now time.Time }

func (Node) Name() string { return "nodejs.org" }

func (n Node) Find(ctx context.Context, get Getter) ([]Candidate, []Watch, error) {
	b, err := get(ctx, "https://nodejs.org/dist/index.json")
	if err != nil {
		return nil, nil, err
	}
	var index []struct {
		Version  string   `json:"version"`
		Date     string   `json:"date"`
		Files    []string `json:"files"`
		LTS      any      `json:"lts"`
		Security bool     `json:"security"`
	}
	if err := json.Unmarshal(b, &index); err != nil {
		return nil, nil, fmt.Errorf("index.json: %w", err)
	}
	eol, _, err := eolDates(ctx, get, "nodejs")
	if err != nil {
		return nil, nil, err
	}
	perMajor := map[string]int{}
	var out []Candidate
	for _, r := range index { // newest first
		codename, isLTS := r.LTS.(string)
		if !isLTS || !contains(r.Files, "win-x64-zip") {
			continue
		}
		v := strings.TrimPrefix(r.Version, "v")
		major := SeriesOf("node", v)
		if perMajor[major] >= 3 { // the newest few patches are enough
			continue
		}
		perMajor[major]++
		day, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			continue
		}
		file := "node-" + r.Version + "-win-x64.zip"
		sums, err := get(ctx, "https://nodejs.org/dist/"+r.Version+"/SHASUMS256.txt")
		if err != nil {
			return nil, nil, err
		}
		sum := shaFor(string(sums), file)
		if sum == "" {
			continue
		}
		out = append(out, Candidate{
			Runtime: "node", Series: major, Released: day, Security: r.Security,
			NewOK: !ended(eol[major], n.Now),
			Release: manifest.Release{
				Version: v, URL: "https://nodejs.org/dist/" + r.Version + "/" + file, SHA256: sum,
				Root: strings.TrimSuffix(file, ".zip"), Arch: "x64", EOL: eol[major],
				Notes: fmt.Sprintf("LTS (%s); sha256 from SHASUMS256.txt (manifest bot)", codename),
			},
		})
	}
	return out, nil, nil
}

func shaFor(sums, file string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == file && len(f[0]) == 64 {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// PHP: the newest non-thread-safe x64 build of every series from
// windows.php.net's releases.json (with its SHA-256); release date and
// the "security" tag from php.net's release API.
type PHP struct{ Now time.Time }

func (PHP) Name() string { return "windows.php.net" }

var phpBuildRe = regexp.MustCompile(`^nts-(vs|vc)(\d+)-x64$`)

func (p PHP) Find(ctx context.Context, get Getter) ([]Candidate, []Watch, error) {
	b, err := get(ctx, "https://downloads.php.net/~windows/releases/releases.json")
	if err != nil {
		return nil, nil, err
	}
	var series map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &series); err != nil {
		return nil, nil, fmt.Errorf("releases.json: %w", err)
	}
	eol, _, err := eolDates(ctx, get, "php")
	if err != nil {
		return nil, nil, err
	}
	var out []Candidate
	for s, builds := range series {
		var version string
		json.Unmarshal(builds["version"], &version)
		best, bestVS := "", -1
		for name := range builds {
			if m := phpBuildRe.FindStringSubmatch(name); m != nil {
				var n int
				fmt.Sscan(m[2], &n)
				if n > bestVS {
					best, bestVS = name, n
				}
			}
		}
		if version == "" || best == "" {
			continue
		}
		var build struct {
			MTime string `json:"mtime"`
			Zip   struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"zip"`
		}
		if json.Unmarshal(builds[best], &build) != nil || len(build.Zip.SHA256) != 64 {
			continue
		}
		released, _ := time.Parse(time.RFC3339, build.MTime)
		security := false
		if info, err := get(ctx, "https://www.php.net/releases/index.php?json&version="+version); err == nil {
			var r struct {
				Date string   `json:"date"`
				Tags []string `json:"tags"`
			}
			if json.Unmarshal(info, &r) == nil {
				security = contains(r.Tags, "security")
				if d, err := time.Parse("2 Jan 2006", r.Date); err == nil {
					released = d
				}
			}
		}
		if released.IsZero() {
			continue
		}
		out = append(out, Candidate{
			Runtime: "php", Series: s, Released: released, Security: security,
			NewOK: !strings.Contains(version, "RC") && !ended(eol[s], p.Now),
			Release: manifest.Release{
				Version: version, URL: "https://downloads.php.net/~windows/releases/" + build.Zip.Path,
				SHA256: strings.ToLower(build.Zip.SHA256), Arch: "x64", EOL: eol[s],
				Notes: strings.TrimPrefix(best, "nts-") + "; sha256 from releases.json (manifest bot)",
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Series < out[j].Series })
	return out, nil, nil
}

// Composer: the stable version from getcomposer.org with its published
// .sha256sum; date and security notes from its GitHub release.
type Composer struct{}

func (Composer) Name() string { return "getcomposer.org" }

func (Composer) Find(ctx context.Context, get Getter) ([]Candidate, []Watch, error) {
	b, err := get(ctx, "https://getcomposer.org/versions")
	if err != nil {
		return nil, nil, err
	}
	var v struct {
		Stable []struct {
			Version string `json:"version"`
		} `json:"stable"`
	}
	if err := json.Unmarshal(b, &v); err != nil || len(v.Stable) == 0 {
		return nil, nil, fmt.Errorf("versions: unexpected answer")
	}
	version := v.Stable[0].Version
	sum, err := get(ctx, "https://getcomposer.org/download/"+version+"/composer.phar.sha256sum")
	if err != nil {
		return nil, nil, err
	}
	sha := strings.ToLower(strings.Fields(string(sum) + " x")[0])
	if len(sha) != 64 {
		return nil, nil, fmt.Errorf("composer.phar.sha256sum: unexpected answer")
	}
	rel, err := githubRelease(ctx, get, "composer/composer", version)
	if err != nil {
		return nil, nil, err
	}
	return []Candidate{{
		Runtime: "composer", Released: rel.Published, Security: securityRe.MatchString(rel.Body),
		Release: manifest.Release{
			Version: version, URL: "https://getcomposer.org/download/" + version + "/composer.phar",
			SHA256: sha, File: "composer.phar", Arch: "x64",
			Notes: "PHP-07; sha256 from getcomposer.org (manifest bot)",
		},
	}}, nil, nil
}

// securityRe spots release notes that announce a security fix.
var securityRe = regexp.MustCompile(`(?i)(CVE-\d{4}-\d+|GHSA-[0-9a-z]{4}-|security fix|urgency:\s*security|VULN-\d+)`)

type ghRelease struct {
	Tag        string
	Published  time.Time
	Body       string
	Prerelease bool
	Assets     map[string]string // name -> sha256
}

func githubReleases(ctx context.Context, get Getter, repo string) ([]ghRelease, error) {
	b, err := get(ctx, "https://api.github.com/repos/"+repo+"/releases?per_page=15")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Tag        string    `json:"tag_name"`
		Published  time.Time `json:"published_at"`
		Body       string    `json:"body"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s releases: %w", repo, err)
	}
	var out []ghRelease
	for _, r := range raw {
		if r.Draft {
			continue
		}
		g := ghRelease{Tag: r.Tag, Published: r.Published, Body: r.Body, Prerelease: r.Prerelease, Assets: map[string]string{}}
		for _, a := range r.Assets {
			if sha, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && len(sha) == 64 {
				g.Assets[a.Name] = strings.ToLower(sha)
			}
		}
		out = append(out, g)
	}
	return out, nil
}

func githubRelease(ctx context.Context, get Getter, repo, version string) (ghRelease, error) {
	all, err := githubReleases(ctx, get, repo)
	if err != nil {
		return ghRelease{}, err
	}
	for _, r := range all {
		if strings.TrimPrefix(r.Tag, "v") == version {
			return r, nil
		}
	}
	return ghRelease{}, fmt.Errorf("%s: no GitHub release for %s", repo, version)
}

// GitHub covers tools published as GitHub release assets with SHA-256
// digests: Mailpit, cloudflared, Redis (redis-windows) and Adminer.
type GitHub struct {
	Runtime string
	Repo    string
	Asset   func(version string) string // asset file name
	File    string                      // single-file runtimes: saved as
	Root    func(version string) string // zip runtimes: folder inside
	Notes   string
}

func (g GitHub) Name() string { return "github.com/" + g.Repo }

func (g GitHub) Find(ctx context.Context, get Getter) ([]Candidate, []Watch, error) {
	rels, err := githubReleases(ctx, get, g.Repo)
	if err != nil {
		return nil, nil, err
	}
	var out []Candidate
	for _, r := range rels {
		if r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(r.Tag, "v")
		if _, err := manifest.ParseVersion(v); err != nil {
			continue
		}
		name := g.Asset(v)
		sha, ok := r.Assets[name]
		if !ok {
			continue
		}
		rel := manifest.Release{
			Version: v, URL: "https://github.com/" + g.Repo + "/releases/download/" + r.Tag + "/" + name,
			SHA256: sha, File: g.File, Arch: "x64", Notes: g.Notes + "; sha256 from the GitHub release digest (manifest bot)",
		}
		if g.Root != nil {
			rel.Root = g.Root(v)
		}
		out = append(out, Candidate{Runtime: g.Runtime, Released: r.Published, Security: securityRe.MatchString(r.Body), Release: rel})
	}
	return out, nil, nil
}

// Watched reports newer upstream versions Ebur cannot verify on its own
// (MySQL is GPG-signed with an MD5; MariaDB, Nginx and phpredis publish no
// machine-readable SHA-256 Ebur trusts yet). A maintainer adds them.
type Watched struct {
	Manifest *manifest.Manifest
}

func (Watched) Name() string { return "endoflife.date" }

func (w Watched) Find(ctx context.Context, get Getter) ([]Candidate, []Watch, error) {
	var out []Watch
	for _, p := range []struct{ runtime, product, how string }{
		{"mysql", "mysql", "verify Oracle's MD5 and GPG signature, then pin the SHA-256"},
		{"mariadb", "mariadb", "take the SHA-256 from downloads.mariadb.org"},
	} {
		_, latest, err := eolDates(ctx, get, p.product)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range w.Manifest.Runtimes[p.runtime] {
			s := SeriesOf(p.runtime, r.Version)
			if l := latest[s]; l != "" && manifest.CompareVersions(l, r.Version) > 0 {
				out = append(out, Watch{Runtime: p.runtime, Series: s, Have: r.Version, Latest: l, Why: p.how})
			}
		}
	}
	// Nginx: endoflife.date tracks only the newest cycles, so compare the
	// newest stable line with the one shipped.
	if _, latest, err := eolDates(ctx, get, "nginx"); err == nil {
		for _, r := range w.Manifest.Runtimes["nginx"] {
			for _, l := range latest {
				if SeriesOf("mysql", l) == SeriesOf("mysql", r.Version) && manifest.CompareVersions(l, r.Version) > 0 {
					out = append(out, Watch{Runtime: "nginx", Have: r.Version, Latest: l, Why: "nginx.org publishes PGP signatures only: verify, then pin the SHA-256"})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Runtime+out[i].Series < out[j].Runtime+out[j].Series })
	return nil, out, nil
}

// Defaults are the sources the scheduled job uses.
func Defaults(m *manifest.Manifest, now time.Time) []Source {
	return []Source{
		Node{Now: now},
		PHP{Now: now},
		Composer{},
		GitHub{Runtime: "mailpit", Repo: "axllent/mailpit", Notes: "TOOL-03 mail capture",
			Asset: func(string) string { return "mailpit-windows-amd64.zip" }},
		GitHub{Runtime: "cloudflared", Repo: "cloudflare/cloudflared", File: "cloudflared.exe", Notes: "TOOL-04 share links (quick tunnels)",
			Asset: func(string) string { return "cloudflared-windows-amd64.exe" }},
		GitHub{Runtime: "redis", Repo: "redis-windows/redis-windows", Notes: "SVC-05; MSYS2 build by the redis-windows project",
			Asset: func(v string) string { return "Redis-" + v + "-Windows-x64-msys2.zip" },
			Root:  func(v string) string { return "Redis-" + v + "-Windows-x64-msys2" }},
		GitHub{Runtime: "adminer", Repo: "vrana/adminer", File: "index.php", Notes: "TOOL-01",
			Asset: func(v string) string { return "adminer-" + v + ".php" }},
		Watched{Manifest: m},
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
