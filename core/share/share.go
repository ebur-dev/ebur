// Package share keeps the state of public share links (TOOL-04): which
// sites are shared through a Cloudflare quick tunnel, on which private
// local port, until when, and with which optional password.
//
// The host owns shares; this package holds the parts that do not need a
// running host so they can be tested: the state file, password files,
// reading the public address from cloudflared's log, and the WordPress
// helper plugin.
package share

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/internal/fsutil"
)

// Share is one shared site.
type Share struct {
	Site     string    `json:"site"`
	Port     int       `json:"port"` // private Nginx listener the tunnel points at
	Started  time.Time `json:"started"`
	Expires  time.Time `json:"expires"`
	URL      string    `json:"url,omitempty"`      // https://<words>.trycloudflare.com
	User     string    `json:"user,omitempty"`     // set when password-protected
	Password string    `json:"password,omitempty"` // shown to the user so they can pass it on
}

// Limits on how long a share may last.
const (
	DefaultDuration = time.Hour
	MaxDuration     = 24 * time.Hour
	firstPort       = 8180
	lastPort        = 8299
)

// File is where active shares are kept while the host runs.
func File(root paths.Root) string { return filepath.Join(root.Data(), "shares.json") }

// LogFile is the cloudflared log for one shared site.
func LogFile(root paths.Root, site string) string {
	return filepath.Join(root.Logs(), "share-"+site+".log")
}

// HtpasswdFile is the Nginx password file for one shared site.
func HtpasswdFile(root paths.Root, site string) string {
	return filepath.Join(root.Data(), "share-"+site+".htpasswd")
}

// EmptyConfig is passed to cloudflared so a ~/.cloudflared/config.yml
// meant for named tunnels cannot change how a quick tunnel behaves.
func EmptyConfig(root paths.Root) (string, error) {
	p := filepath.Join(root.Data(), "cloudflared-quick-tunnel.yml")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return p, fsutil.WriteFileAtomic(p, []byte("# Empty on purpose: Ebur runs quick tunnels only.\n"), 0o644)
}

// Load reads the active shares, sorted by site. A missing file means none.
func Load(root paths.Root) ([]Share, error) {
	b, err := os.ReadFile(File(root))
	if errors.Is(err, os.ErrNotExist) {
		return []Share{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Share
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", File(root), err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Site < out[j].Site })
	return out, nil
}

// Save writes the active shares; an empty list removes the file.
func Save(root paths.Root, shares []Share) error {
	if len(shares) == 0 {
		err := os.Remove(File(root))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.MarshalIndent(shares, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root.Data(), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(File(root), append(b, '\n'), 0o600)
}

// Clear ends every share's state: the file and the password files.
func Clear(root paths.Root) error {
	shares, _ := Load(root)
	for _, s := range shares {
		os.Remove(HtpasswdFile(root, s.Site))
	}
	return Save(root, nil)
}

// Find returns the share for site.
func Find(shares []Share, site string) (Share, int) {
	for i, s := range shares {
		if s.Site == site {
			return s, i
		}
	}
	return Share{}, -1
}

// Expired returns the shares whose time is up.
func Expired(shares []Share, now time.Time) []Share {
	var out []Share
	for _, s := range shares {
		if !s.Expires.IsZero() && !now.Before(s.Expires) {
			out = append(out, s)
		}
	}
	return out
}

// PickPort returns the first port in Ebur's share range that no share
// uses and free() accepts.
func PickPort(shares []Share, free func(int) bool) (int, error) {
	used := map[int]bool{}
	for _, s := range shares {
		used[s.Port] = true
	}
	for p := firstPort; p <= lastPort; p++ {
		if !used[p] && free(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port between %d and %d for a share", firstPort, lastPort)
}

const pwChars = "abcdefghijkmnpqrstuvwxyz23456789" // no l/1/o/0: easy to read out

// NewPassword makes a password that is easy to type on a phone.
func NewPassword() (string, error) {
	b := make([]byte, 12)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(pwChars))))
		if err != nil {
			return "", err
		}
		b[i] = pwChars[n.Int64()]
	}
	return string(b[:4]) + "-" + string(b[4:8]) + "-" + string(b[8:]), nil
}

// Htpasswd returns an Nginx user file line. Nginx on Windows has no
// crypt(), so it uses the salted SHA-1 scheme it supports there.
func Htpasswd(user, password string) (string, error) {
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := sha1.Sum(append([]byte(password), salt...))
	return user + ":{SSHA}" + base64.StdEncoding.EncodeToString(append(h[:], salt...)) + "\n", nil
}

var (
	urlRe        = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	registeredRe = regexp.MustCompile(`Registered tunnel connection`)
)

// URLFromLog returns the newest public address in a cloudflared log
// ("" if none yet). After a tunnel restart the address changes, so the
// last one wins.
func URLFromLog(log string) string {
	all := urlRe.FindAllString(log, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// Connected reports whether cloudflared has registered a connection to
// Cloudflare since its address was announced.
func Connected(log string) bool {
	u := URLFromLog(log)
	if u == "" {
		return false
	}
	return registeredRe.MatchString(log[strings.LastIndex(log, u):])
}

// LastLines returns up to n non-empty lines from the end of a log, for
// error messages.
func LastLines(log string, n int) string {
	var out []string
	lines := strings.Split(strings.ReplaceAll(log, "\r", ""), "\n")
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			out = append([]string{l}, out...)
		}
	}
	return strings.Join(out, "\n")
}
