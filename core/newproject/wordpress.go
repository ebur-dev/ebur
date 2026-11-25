package newproject

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ebur-dev/ebur/core/runtimes"
)

// WordPress publishes its latest release through this API, and a SHA-1
// of each zip on wordpress.org (a different host from the download).
var (
	wpVersionAPI = "https://api.wordpress.org/core/version-check/1.7/"
	wpChecksum   = "https://wordpress.org/wordpress-%s.zip.sha1"
	httpClient   = http.DefaultClient
)

var wpVersionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

func get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ebur-new-project")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s returned %s", u, resp.Status)
	}
	return resp, nil
}

// latestWordPress returns the newest release's version and zip URL.
func latestWordPress(ctx context.Context) (string, string, error) {
	resp, err := get(ctx, wpVersionAPI)
	if err != nil {
		return "", "", fmt.Errorf("find the latest WordPress: %w", err)
	}
	defer resp.Body.Close()
	var v struct {
		Offers []struct {
			Download string `json:"download"`
			Current  string `json:"current"`
		} `json:"offers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil || len(v.Offers) == 0 {
		return "", "", errors.New("find the latest WordPress: unexpected answer from api.wordpress.org")
	}
	o := v.Offers[0]
	u, err := url.Parse(o.Download)
	if !wpVersionRe.MatchString(o.Current) || err != nil || u.Scheme != "https" ||
		(u.Host != "downloads.wordpress.org" && u.Host != "wordpress.org") {
		return "", "", fmt.Errorf("find the latest WordPress: unexpected release %q at %q", o.Current, o.Download)
	}
	return o.Current, o.Download, nil
}

func (r runner) wordpress(ctx context.Context, tmp string, res Result) error {
	r.say("Finding the latest WordPress")
	version, zipURL, err := latestWordPress(ctx)
	if err != nil {
		return err
	}
	resp, err := get(ctx, fmt.Sprintf(wpChecksum, version))
	if err != nil {
		return fmt.Errorf("get the WordPress checksum: %w", err)
	}
	sumText, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	resp.Body.Close()
	want := strings.ToLower(strings.TrimSpace(strings.Fields(string(sumText) + " x")[0]))
	if len(want) != 40 {
		return errors.New("get the WordPress checksum: unexpected answer from wordpress.org")
	}

	if err := os.MkdirAll(r.root.Downloads(), 0o755); err != nil {
		return err
	}
	zipPath := filepath.Join(r.root.Downloads(), "wordpress-"+version+".zip")
	if sha1File(zipPath) != want { // reuse a verified earlier download
		r.say("Downloading WordPress " + version)
		if err := download(ctx, zipURL, zipPath, want); err != nil {
			return err
		}
	}
	r.say("Unpacking WordPress " + version)
	if err := runtimes.Extract(zipPath, tmp, "wordpress"); err != nil {
		return fmt.Errorf("unpack WordPress: %w", err)
	}
	r.say("Writing wp-config.php")
	sample, err := os.ReadFile(filepath.Join(tmp, "wp-config-sample.php"))
	if err != nil {
		return err
	}
	cfg, err := wpConfig(string(sample), res.Database, r.cfg.DBRootPassword, fmt.Sprintf("127.0.0.1:%d", r.db.c.Server.Port))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(tmp, "wp-config.php"), []byte(cfg), 0o644)
}

func sha1File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func download(ctx context.Context, u, to, wantSHA1 string) error {
	resp, err := get(ctx, u)
	if err != nil {
		return fmt.Errorf("download WordPress: %w", err)
	}
	defer resp.Body.Close()
	part := to + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha1.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(part)
		return fmt.Errorf("download WordPress: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA1 {
		os.Remove(part)
		return fmt.Errorf("the WordPress download does not match its published checksum (got %s, want %s); it was deleted", got, wantSHA1)
	}
	os.Remove(to)
	return os.Rename(part, to)
}

// wpConfig fills WordPress's wp-config-sample.php: database settings,
// fresh secret keys, and WP_ENVIRONMENT_TYPE "local".
func wpConfig(sample, dbName, password, host string) (string, error) {
	php := func(s string) string { return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) }
	out := sample
	for _, r := range []struct{ old, new string }{
		{"'database_name_here'", "'" + php(dbName) + "'"},
		{"'username_here'", "'root'"},
		{"'password_here'", "'" + php(password) + "'"},
		{"'DB_HOST', 'localhost'", "'DB_HOST', '" + php(host) + "'"},
	} {
		if !strings.Contains(out, r.old) {
			return "", fmt.Errorf("wp-config-sample.php has an unexpected layout (no %s); copy it to wp-config.php and fill it in by hand", r.old)
		}
		out = strings.Replace(out, r.old, r.new, 1)
	}
	const phrase = "'put your unique phrase here'"
	if strings.Count(out, phrase) < 8 {
		return "", errors.New("wp-config-sample.php has an unexpected layout (secret keys); copy it to wp-config.php and fill it in by hand")
	}
	for strings.Contains(out, phrase) {
		key, err := secret(64)
		if err != nil {
			return "", err
		}
		out = strings.Replace(out, phrase, "'"+key+"'", 1)
	}
	const marker = "/* That's all, stop editing!"
	if i := strings.Index(out, marker); i >= 0 {
		out = out[:i] + "define( 'WP_ENVIRONMENT_TYPE', 'local' );\n\n" + out[i:]
	}
	return out, nil
}

// secret is a random key like WordPress's own, without ' and \.
func secret(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_[]{}<>~`+=,.;:/?|"
	b := make([]byte, n)
	for i := range b {
		k, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		b[i] = chars[k.Int64()]
	}
	return string(b), nil
}
