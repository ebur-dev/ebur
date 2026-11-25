// Package catalog decides which runtime manifest Ebur uses (SEC-02,
// SEC-08): the one built into this binary, or a newer copy fetched from the
// project and verified with an Ed25519 signature against a public key
// compiled into Ebur. A copy that is unsigned, wrongly signed, broken or
// not newer than what Ebur already has is ignored, so a tampered or
// replayed list can never change what Ebur downloads.
//
// The fetched copy is cached in data\manifest\ and re-verified on every
// load. When offline, or before the project publishes a signed manifest,
// Ebur simply keeps using the built-in one.
package catalog

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/internal/fsutil"
	manifestdata "github.com/ebur-dev/ebur/manifest"
)

// PublicKeys verify signed manifests (base64 Ed25519 keys). The newest key
// is first; an older one may stay for a while after a key rotation.
var PublicKeys = []string{
	"7/GOLMT1ctBhD21Z1GF3VVk4a4qE80ellZMjD9ca1NE=", // generated 2026-11-25; private key: MANIFEST_SIGNING_KEY secret
}

// DefaultURL is where the signed manifest is published: an asset of the
// "manifest" release, kept current by .github/workflows/publish-manifest.yml.
// Its signature is at the same URL plus ".sig".
const DefaultURL = "https://github.com/ebur-dev/ebur/releases/download/manifest/runtimes.json"

// Source says where the manifest in use came from.
type Source struct {
	Kind      string    `json:"kind"` // "built-in" or "downloaded"
	Generated time.Time `json:"generated"`
	Path      string    `json:"path,omitempty"`
}

// ErrSignature means a manifest failed signature verification.
var ErrSignature = errors.New("manifest signature does not verify")

var errNotFound = errors.New("not found")

// Verify checks a base64 Ed25519 signature over data against PublicKeys.
func Verify(data, sig []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return ErrSignature
	}
	for _, k := range PublicKeys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(pub), data, raw) {
			return nil
		}
	}
	return ErrSignature
}

// Sign makes the base64 signature Verify accepts (used by the publishing
// tool and tests).
func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

func cacheDir(root paths.Root) string  { return filepath.Join(root.Data(), "manifest") }
func cachePath(root paths.Root) string { return filepath.Join(cacheDir(root), "runtimes.json") }

var (
	mu       sync.Mutex
	rootOnce sync.Once
	findRoot paths.Root
	haveRoot bool
)

// Load returns the manifest Ebur should use: a cached, verified, newer
// download when there is one, otherwise the built-in manifest. It finds the
// Ebur root itself (EBUR_ROOT or the program's folder).
func Load() (*manifest.Manifest, Source, error) {
	rootOnce.Do(func() {
		if r, err := paths.Find(); err == nil {
			findRoot, haveRoot = r, true
		}
	})
	if !haveRoot {
		return builtIn()
	}
	return LoadFrom(findRoot)
}

// LoadFrom is Load for a given root.
func LoadFrom(root paths.Root) (*manifest.Manifest, Source, error) {
	base, src, err := builtIn()
	if err != nil {
		return nil, src, err
	}
	if m, ok := cached(root, base.Generated); ok {
		return m, Source{Kind: "downloaded", Generated: m.Generated, Path: cachePath(root)}, nil
	}
	return base, src, nil
}

func builtIn() (*manifest.Manifest, Source, error) {
	m, err := manifest.Parse(manifestdata.JSON)
	if err != nil {
		return nil, Source{}, err
	}
	return m, Source{Kind: "built-in", Generated: m.Generated}, nil
}

// cached returns the cached download if it verifies and is newer than after.
func cached(root paths.Root, after time.Time) (*manifest.Manifest, bool) {
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(cachePath(root))
	if err != nil {
		return nil, false
	}
	sig, err := os.ReadFile(cachePath(root) + ".sig")
	if err != nil || Verify(data, sig) != nil {
		return nil, false
	}
	m, err := manifest.Parse(data)
	if err != nil || !m.Generated.After(after) {
		return nil, false
	}
	return m, true
}

// Result says what Refresh did.
type Result struct {
	Updated   bool      `json:"updated"`
	Generated time.Time `json:"generated"` // of the manifest now in use
	Message   string    `json:"message"`
}

// URL returns where to fetch the manifest from, or "" when the user turned
// fetching off (manifest_url: "off").
func URL(cfg config.Settings) string {
	switch cfg.ManifestURL {
	case "":
		return DefaultURL
	case "off":
		return ""
	}
	return cfg.ManifestURL
}

// Refresh fetches the signed manifest and caches it when it verifies and is
// newer than the one in use. Nothing identifying is sent: a plain GET.
func Refresh(ctx context.Context, root paths.Root, client *http.Client) (Result, error) {
	cfg, err := config.Load(root.Config())
	if err != nil {
		return Result{}, err
	}
	cur, _, err := LoadFrom(root)
	if err != nil {
		return Result{}, err
	}
	res := Result{Generated: cur.Generated}
	url := URL(cfg)
	if url == "" {
		res.Message = "fetching is off (manifest_url: off)"
		return res, nil
	}
	if !strings.HasPrefix(url, "https://") {
		return res, fmt.Errorf("manifest_url must be https: %s", url)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	data, err := fetch(ctx, client, url, 8<<20)
	if errors.Is(err, errNotFound) {
		// Normal until the project publishes a signed list (or while its
		// repository is private): keep the built-in one, quietly.
		res.Message = "no signed list is published at " + url + " yet"
		return res, nil
	}
	if err != nil {
		return res, err
	}
	sig, err := fetch(ctx, client, url+".sig", 4<<10)
	if err != nil {
		return res, err
	}
	if err := Verify(data, sig); err != nil {
		return res, fmt.Errorf("%w (from %s); keeping the manifest Ebur has", err, url)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return res, fmt.Errorf("downloaded manifest: %w; keeping the one Ebur has", err)
	}
	if !m.Generated.After(cur.Generated) {
		res.Message = "already up to date"
		return res, nil
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(cacheDir(root), 0o755); err != nil {
		return res, err
	}
	// Signature first: a crash in between leaves a pair that fails to
	// verify (and is ignored), never an unverified manifest.
	if err := fsutil.WriteFileAtomic(cachePath(root)+".sig", sig, 0o644); err != nil {
		return res, err
	}
	if err := fsutil.WriteFileAtomic(cachePath(root), data, 0o644); err != nil {
		return res, err
	}
	res.Updated, res.Generated = true, m.Generated
	res.Message = "updated to the manifest of " + m.Generated.Format("2006-01-02 15:04 MST")
	return res, nil
}

func fetch(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ebur-manifest")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", url, limit)
	}
	return b, nil
}
