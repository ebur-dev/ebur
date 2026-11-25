// Package manifest parses the runtime manifest: which versions of each
// runtime exist, where to download them, and their SHA-256 (RUN-01).
//
// Signature checks (SEC-08) and security status (SEC-01) arrive in v1;
// this package already rejects anything that could make an install unsafe.
package manifest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the manifest format this build understands.
const SchemaVersion = 1

type Manifest struct {
	Schema    int                  `json:"schema"`
	Generated time.Time            `json:"generated"`
	Runtimes  map[string][]Release `json:"runtimes"`
}

// Release is one downloadable version of a runtime.
type Release struct {
	Version string `json:"version"`         // full version, e.g. "8.4.28"
	URL     string `json:"url"`             // https URL of the zip
	SHA256  string `json:"sha256"`          // lowercase hex
	Root    string `json:"root,omitempty"`  // folder inside the zip to extract, e.g. "nginx-1.30.8"
	File    string `json:"file,omitempty"`  // set for single-file downloads (not zips): saved under this name
	Arch    string `json:"arch"`            // "x64" or "arm64"
	Notes   string `json:"notes,omitempty"` // e.g. build flavour "nts-vs17"
	// EOL is the end of security support, YYYY-MM-DD (SEC-01). Releases
	// past it stay installable for legacy projects, with a warning.
	EOL string `json:"eol,omitempty"`
}

// EndOfLife reports whether the release is past its end of support.
func (r Release) EndOfLife(now time.Time) bool {
	if r.EOL == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", r.EOL)
	// Supported through the EOL date itself; end of life from the next day.
	return err == nil && !now.Before(t.Add(24*time.Hour))
}

// Find returns the release of runtime with exactly this version.
func (m *Manifest) Find(runtime, version string) (Release, bool) {
	for _, r := range m.Runtimes[runtime] {
		if r.Version == version {
			return r, true
		}
	}
	return Release{}, false
}

// Parse decodes and validates a manifest.
func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks every release so a bad manifest is rejected whole.
func (m *Manifest) Validate() error {
	if m.Schema != SchemaVersion {
		return fmt.Errorf("manifest: schema %d not supported (this Ebur reads schema %d; update Ebur)", m.Schema, SchemaVersion)
	}
	if len(m.Runtimes) == 0 {
		return fmt.Errorf("manifest: no runtimes")
	}
	for name, rels := range m.Runtimes {
		if !ValidName(name) {
			return fmt.Errorf("manifest: invalid runtime name %q", name)
		}
		seen := map[string]bool{}
		for _, r := range rels {
			where := fmt.Sprintf("manifest: %s %s", name, r.Version)
			if _, err := ParseVersion(r.Version); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			key := r.Version + "/" + r.Arch
			if seen[key] {
				return fmt.Errorf("%s: duplicate entry for %s", where, r.Arch)
			}
			seen[key] = true
			u, err := url.Parse(r.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				return fmt.Errorf("%s: url must be https", where)
			}
			if h, err := hex.DecodeString(r.SHA256); err != nil || len(h) != 32 || strings.ToLower(r.SHA256) != r.SHA256 {
				return fmt.Errorf("%s: sha256 must be 64 lowercase hex characters", where)
			}
			if r.Arch != "x64" && r.Arch != "arm64" {
				return fmt.Errorf("%s: arch must be x64 or arm64", where)
			}
			if strings.Contains(r.Root, "..") || strings.ContainsAny(r.Root, `\:`) || strings.HasPrefix(r.Root, "/") {
				return fmt.Errorf("%s: root must be a plain relative folder", where)
			}
			if r.File != "" && (r.Root != "" || strings.ContainsAny(r.File, `/\:`) || strings.HasPrefix(r.File, ".")) {
				return fmt.Errorf("%s: file must be a plain file name and cannot be combined with root", where)
			}
			if r.EOL != "" {
				if _, err := time.Parse("2006-01-02", r.EOL); err != nil {
					return fmt.Errorf("%s: eol must be YYYY-MM-DD", where)
				}
			}
		}
	}
	return nil
}

// ValidName reports whether s is a safe runtime name: lowercase letters,
// digits and dashes. Names become folder names, so this also blocks paths.
func ValidName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// Releases returns a runtime's releases for arch, newest first.
func (m *Manifest) Releases(runtime, arch string) []Release {
	var out []Release
	for _, r := range m.Runtimes[runtime] {
		if r.Arch == arch {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) > 0 })
	return out
}

// Resolve picks a release. want may be "" (newest), a prefix such as
// "8.4" (newest 8.4.x) or an exact version such as "8.4.28".
func (m *Manifest) Resolve(runtime, want, arch string) (Release, error) {
	rels := m.Releases(runtime, arch)
	if len(rels) == 0 {
		if _, ok := m.Runtimes[runtime]; !ok {
			return Release{}, fmt.Errorf("unknown runtime %q", runtime)
		}
		return Release{}, fmt.Errorf("no %s builds for %s", runtime, arch)
	}
	if want == "" {
		return rels[0], nil
	}
	for _, r := range rels {
		if r.Version == want || strings.HasPrefix(r.Version, want+".") {
			return r, nil
		}
	}
	var have []string
	for _, r := range rels {
		have = append(have, r.Version)
	}
	return Release{}, fmt.Errorf("%s %s is not in the manifest (available: %s)", runtime, want, strings.Join(have, ", "))
}

// ParseVersion splits "8.4.28" into numeric parts. Up to four parts.
func ParseVersion(v string) ([]int, error) {
	parts := strings.Split(v, ".")
	if v == "" || len(parts) > 4 {
		return nil, fmt.Errorf("invalid version %q", v)
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return nil, fmt.Errorf("invalid version %q", v)
		}
		out[i] = n
	}
	return out, nil
}

// CompareVersions returns -1, 0 or 1. Invalid versions sort first.
func CompareVersions(a, b string) int {
	pa, errA := ParseVersion(a)
	pb, errB := ParseVersion(b)
	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
