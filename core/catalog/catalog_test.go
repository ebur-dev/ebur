package catalog

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/config"
	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
	manifestdata "github.com/ebur-dev/ebur/manifest"
)

// testKey replaces the built-in public keys for the duration of a test.
func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	old := PublicKeys
	PublicKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	t.Cleanup(func() { PublicKeys = old })
	return priv
}

// newer returns the built-in manifest with its date moved by d and a
// marker version added, as JSON.
func newer(t *testing.T, d time.Duration) []byte {
	t.Helper()
	m, err := manifest.Parse(manifestdata.JSON)
	if err != nil {
		t.Fatal(err)
	}
	m.Generated = m.Generated.Add(d)
	m.Runtimes["composer"] = append([]manifest.Release{{Version: "9.9.9", URL: "https://getcomposer.org/download/9.9.9/composer.phar",
		SHA256: strings.Repeat("a", 64), File: "composer.phar", Arch: "x64"}}, m.Runtimes["composer"]...)
	b, _ := json.Marshal(m)
	return b
}

func serve(t *testing.T, files map[string][]byte) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func setup(t *testing.T, url string) paths.Root {
	t.Helper()
	root, _ := paths.New(t.TempDir())
	cfg := config.Defaults()
	cfg.ManifestURL = url
	if err := config.Save(root.Config(), cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func hasComposer999(m *manifest.Manifest) bool {
	_, ok := m.Find("composer", "9.9.9")
	return ok
}

func TestRefreshAcceptsANewerSignedManifest(t *testing.T) {
	priv := testKey(t)
	data := newer(t, 48*time.Hour)
	base := serve(t, map[string][]byte{"/m.json": data, "/m.json.sig": Sign(priv, data)})
	root := setup(t, base+"/m.json")
	res, err := Refresh(context.Background(), root, insecure())
	if err != nil || !res.Updated {
		t.Fatalf("refresh: %+v %v", res, err)
	}
	m, src, err := LoadFrom(root)
	if err != nil || src.Kind != "downloaded" || !hasComposer999(m) {
		t.Fatalf("load after refresh: %s %v", src.Kind, err)
	}
	// Same list again: nothing to do.
	if res, err := Refresh(context.Background(), root, insecure()); err != nil || res.Updated {
		t.Fatalf("second refresh: %+v %v", res, err)
	}
}

func TestRefreshRejectsBadOrOldLists(t *testing.T) {
	priv := testKey(t)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	good := newer(t, 48*time.Hour)
	old := newer(t, -48*time.Hour)
	tampered := []byte(strings.Replace(string(good), "9.9.9", "6.6.6", 1))
	cases := map[string]map[string][]byte{
		"wrong key":      {"/m.json": good, "/m.json.sig": Sign(otherPriv, good)},
		"tampered":       {"/m.json": tampered, "/m.json.sig": Sign(priv, good)},
		"no signature":   {"/m.json": good},
		"garbage sig":    {"/m.json": good, "/m.json.sig": []byte("not base64!")},
		"older (replay)": {"/m.json": old, "/m.json.sig": Sign(priv, old)},
		"not a manifest": {"/m.json": []byte(`{"schema":99}`), "/m.json.sig": Sign(priv, []byte(`{"schema":99}`))},
	}
	for name, files := range cases {
		root := setup(t, serve(t, files)+"/m.json")
		res, err := Refresh(context.Background(), root, insecure())
		if res.Updated {
			t.Errorf("%s: accepted", name)
		}
		if name != "older (replay)" && err == nil {
			t.Errorf("%s: no error", name)
		}
		if name == "wrong key" || name == "tampered" || name == "garbage sig" {
			if !errors.Is(err, ErrSignature) {
				t.Errorf("%s: want ErrSignature, got %v", name, err)
			}
		}
		if _, src, _ := LoadFrom(root); src.Kind != "built-in" {
			t.Errorf("%s: using %s list", name, src.Kind)
		}
	}
}

func TestTamperedCacheIsIgnored(t *testing.T) {
	priv := testKey(t)
	data := newer(t, 48*time.Hour)
	root := setup(t, serve(t, map[string][]byte{"/m.json": data, "/m.json.sig": Sign(priv, data)})+"/m.json")
	if _, err := Refresh(context.Background(), root, insecure()); err != nil {
		t.Fatal(err)
	}
	// Someone edits the cached file on disk.
	os.WriteFile(cachePath(root), []byte(strings.Replace(string(data), "9.9.9", "6.6.6", 1)), 0o644)
	if _, src, _ := LoadFrom(root); src.Kind != "built-in" {
		t.Fatalf("tampered cache used (%s)", src.Kind)
	}
}

func TestFetchingOffAndHTTPOnly(t *testing.T) {
	testKey(t)
	root := setup(t, "off")
	if res, err := Refresh(context.Background(), root, nil); err != nil || res.Updated || !strings.Contains(res.Message, "off") {
		t.Fatalf("off: %+v %v", res, err)
	}
	root = setup(t, "http://example.org/m.json")
	if _, err := Refresh(context.Background(), root, nil); err == nil {
		t.Fatal("plain http accepted")
	}
}

// Before the project publishes a list (or while the repository is
// private) the URL is a 404: that is normal, not an error.
func TestNothingPublishedYet(t *testing.T) {
	testKey(t)
	root := setup(t, serve(t, map[string][]byte{})+"/m.json")
	res, err := Refresh(context.Background(), root, insecure())
	if err != nil || res.Updated || !strings.Contains(res.Message, "no signed list is published") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestBuiltInKeyIsValid(t *testing.T) {
	if len(PublicKeys) == 0 {
		t.Fatal("no public key built in")
	}
	b, err := base64.StdEncoding.DecodeString(PublicKeys[0])
	if err != nil || len(b) != ed25519.PublicKeySize {
		t.Fatalf("built-in key is not an Ed25519 public key: %v", err)
	}
}

// insecure trusts httptest's self-signed certificate (tests only: the
// signature, not TLS, is what these tests check).
func insecure() *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
}
