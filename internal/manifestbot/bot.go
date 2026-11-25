// Package manifestbot keeps manifest/runtimes.json current (SEC-08): it
// asks official sources for new releases, applies Ebur's rules and
// rewrites the manifest, for a scheduled job to propose as a pull request
// that a maintainer reviews. Nothing reaches users without that review.
//
// Rules:
//   - A release must be at least MinAge old, unless it fixes a security
//     problem (then it is taken at once).
//   - Each series (a Node major, a PHP minor, ...) keeps one entry, its
//     newest eligible patch. Single-entry tools keep their newest release.
//   - New series are added only where the source can say they are ready:
//     Node LTS majors and new PHP minors.
//   - Checksums come from the vendor (a checksum file, an API, a GitHub
//     release digest). Where none exists, the runtime is only watched and
//     reported for a maintainer to add by hand.
package manifestbot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
)

// MinAge is how old a release must be before Ebur offers it, unless it is
// a security release.
const MinAge = 14 * 24 * time.Hour

// Getter fetches a URL's body. Tests replace it.
type Getter func(ctx context.Context, url string) ([]byte, error)

// Candidate is an upstream release a source found.
type Candidate struct {
	Runtime  string
	Series   string // "" for single-entry runtimes
	Release  manifest.Release
	Released time.Time
	Security bool
	NewOK    bool // may be added as a new series
}

// Watch is an upstream release Ebur cannot verify automatically.
type Watch struct {
	Runtime, Series, Have, Latest, Why string
}

// Source finds candidates (and watch-only findings) for some runtimes.
type Source interface {
	Name() string
	Find(ctx context.Context, get Getter) ([]Candidate, []Watch, error)
}

// Change is one edit the bot made or is waiting to make.
type Change struct {
	Runtime, Series, From, To string
	Security                  bool
	WaitUntil                 time.Time // zero when applied now
}

// Report is what a run found, for the pull request text.
type Report struct {
	Applied []Change
	Waiting []Change
	Watch   []Watch
	Errors  []string
}

// Changed reports whether the manifest was edited.
func (r Report) Changed() bool { return len(r.Applied) > 0 }

// Run asks every source and applies the rules to m in place.
func Run(ctx context.Context, m *manifest.Manifest, sources []Source, get Getter, now time.Time) Report {
	var rep Report
	var cands []Candidate
	for _, s := range sources {
		c, w, err := s.Find(ctx, get)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", s.Name(), err))
			continue
		}
		cands = append(cands, c...)
		rep.Watch = append(rep.Watch, w...)
	}
	applied, waiting := Apply(m, cands, now)
	rep.Applied, rep.Waiting = applied, waiting
	if rep.Changed() {
		m.Generated = now.UTC().Truncate(time.Second)
	}
	return rep
}

// SeriesOf returns the series a version belongs to for a runtime.
func SeriesOf(runtime, version string) string {
	parts := strings.Split(version, ".")
	switch runtime {
	case "node":
		return parts[0]
	case "php", "mysql", "mariadb", "phpredis":
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
	}
	return ""
}

func eligible(c Candidate, now time.Time) bool {
	return c.Security || !now.Before(c.Released.Add(MinAge))
}

// Apply edits m with the candidates and returns what changed and what is
// newer but still waiting out MinAge.
func Apply(m *manifest.Manifest, cands []Candidate, now time.Time) (applied, waiting []Change) {
	type key struct{ rt, series string }
	best := map[key]Candidate{}   // newest eligible per series
	newest := map[key]Candidate{} // newest overall, to report waiting ones
	for _, c := range cands {
		k := key{c.Runtime, c.Series}
		if n, ok := newest[k]; !ok || manifest.CompareVersions(c.Release.Version, n.Release.Version) > 0 {
			newest[k] = c
		}
		if !eligible(c, now) {
			continue
		}
		if b, ok := best[k]; !ok || manifest.CompareVersions(c.Release.Version, b.Release.Version) > 0 {
			best[k] = c
		}
	}
	// Index current entries by series.
	current := func(rt, series string) (int, bool) {
		for i, r := range m.Runtimes[rt] {
			if r.Arch == "x64" && SeriesOf(rt, r.Version) == series {
				return i, true
			}
		}
		return 0, false
	}
	keys := make([]key, 0, len(newest))
	for k := range newest {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].rt != keys[j].rt {
			return keys[i].rt < keys[j].rt
		}
		return keys[i].series < keys[j].series
	})
	for _, k := range keys {
		i, have := current(k.rt, k.series)
		haveVer := ""
		if have {
			haveVer = m.Runtimes[k.rt][i].Version
		}
		if b, ok := best[k]; ok && (haveVer == "" || manifest.CompareVersions(b.Release.Version, haveVer) > 0) {
			switch {
			case have:
				rel := b.Release
				if rel.EOL == "" {
					rel.EOL = m.Runtimes[k.rt][i].EOL // the source gave none: keep the known date
				}
				m.Runtimes[k.rt][i] = rel
				applied = append(applied, Change{Runtime: k.rt, Series: k.series, From: haveVer, To: b.Release.Version, Security: b.Security})
			case b.NewOK:
				m.Runtimes[k.rt] = append(m.Runtimes[k.rt], b.Release)
				applied = append(applied, Change{Runtime: k.rt, Series: k.series, To: b.Release.Version, Security: b.Security})
			}
		}
		// Newer but too young: say when it becomes eligible.
		n := newest[k]
		after := haveVer
		if b, ok := best[k]; ok && manifest.CompareVersions(b.Release.Version, after) > 0 && (have || b.NewOK) {
			after = b.Release.Version
		}
		if !eligible(n, now) && manifest.CompareVersions(n.Release.Version, after) > 0 && (have || n.NewOK) {
			waiting = append(waiting, Change{Runtime: k.rt, Series: k.series, From: after, To: n.Release.Version, WaitUntil: n.Released.Add(MinAge)})
		}
	}
	// Keep each runtime newest first, as the manifest is written.
	for rt := range m.Runtimes {
		rels := m.Runtimes[rt]
		sort.SliceStable(rels, func(i, j int) bool { return manifest.CompareVersions(rels[i].Version, rels[j].Version) > 0 })
	}
	return applied, waiting
}

// Markdown renders the report for a pull request.
func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString("Automated check of the official release sources (SEC-08). Review before merging: merged versions reach every Ebur install.\n\n")
	if len(r.Applied) > 0 {
		b.WriteString("### Updated in manifest/runtimes.json\n\n")
		for _, c := range r.Applied {
			from := c.From
			if from == "" {
				from = "(new series)"
			}
			sec := ""
			if c.Security {
				sec = " **security release, taken without the 14-day wait**"
			}
			fmt.Fprintf(&b, "- %s %s: %s → %s%s\n", c.Runtime, c.Series, from, c.To, sec)
		}
		b.WriteString("\n")
	}
	if len(r.Waiting) > 0 {
		b.WriteString("### Waiting out the 14-day rule\n\n")
		for _, c := range r.Waiting {
			fmt.Fprintf(&b, "- %s %s: %s, eligible on %s\n", c.Runtime, c.Series, c.To, c.WaitUntil.Format("2006-01-02"))
		}
		b.WriteString("\n")
	}
	if len(r.Watch) > 0 {
		b.WriteString("### Needs a manual update (no machine-checkable checksum)\n\n")
		for _, w := range r.Watch {
			fmt.Fprintf(&b, "- %s %s: have %s, upstream %s (%s)\n", w.Runtime, w.Series, w.Have, w.Latest, w.Why)
		}
		b.WriteString("\n")
	}
	if len(r.Errors) > 0 {
		b.WriteString("### Sources that could not be checked\n\n")
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
		b.WriteString("\n")
	}
	return b.String()
}
