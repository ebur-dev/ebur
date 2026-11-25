// Package runtimes downloads, verifies, installs and removes runtime
// versions under bin\<runtime>\<version> (RUN-02 to RUN-04, NFR-06).
//
// An install is all-or-nothing: the zip is hashed while it downloads,
// rejected on any mismatch, extracted into a hidden staging folder and
// only then renamed into place. A failed install leaves nothing behind.
package runtimes

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
	"github.com/ebur-dev/ebur/internal/fsutil"
)

var (
	ErrChecksum = errors.New("checksum mismatch")
	ErrInUse    = errors.New("runtime version is in use")
)

// Progress is called as bytes arrive. total is -1 when unknown.
type Progress func(done, total int64)

type Installer struct {
	Root   paths.Root
	Client *http.Client // nil means http.DefaultClient
}

// Install downloads rel and installs it as runtime. It returns the
// install folder. Installing a version that is already present is a no-op.
func (in *Installer) Install(ctx context.Context, runtime string, rel manifest.Release, progress Progress) (string, error) {
	if err := checkIDs(runtime, rel.Version); err != nil {
		return "", err
	}
	dest := in.Root.RuntimeDir(runtime, rel.Version)
	if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
		return dest, nil
	}
	if err := os.MkdirAll(in.Root.Downloads(), 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(in.Root.Downloads(), fmt.Sprintf("%s-%s.zip.part", runtime, rel.Version))
	defer os.Remove(zipPath)

	if err := in.download(ctx, rel, zipPath, progress); err != nil {
		return "", err
	}

	staging := filepath.Join(in.Root.Bin(), runtime, "."+rel.Version+".partial")
	os.RemoveAll(staging) // leftover from a crashed install
	if rel.File != "" {
		// Single-file runtime (e.g. Adminer): place the verified download.
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return "", err
		}
		if err := fsutil.Rename(zipPath, filepath.Join(staging, rel.File)); err != nil {
			os.RemoveAll(staging)
			return "", err
		}
	} else if err := extract(zipPath, staging, rel.Root); err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("extract %s %s: %w", runtime, rel.Version, err)
	}
	if err := fsutil.Rename(staging, dest); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	return dest, nil
}

func (in *Installer) download(ctx context.Context, rel manifest.Release, to string, progress Progress) error {
	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ebur-runtime-manager")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", rel.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: server returned %s", rel.URL, resp.Status)
	}
	f, err := os.Create(to)
	if err != nil {
		return err
	}
	h := sha256.New()
	var src io.Reader = resp.Body
	if progress != nil {
		src = &progressReader{r: resp.Body, total: resp.ContentLength, fn: progress}
	}
	_, copyErr := io.Copy(io.MultiWriter(f, h), src)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("download %s: %w", rel.URL, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != rel.SHA256 {
		return fmt.Errorf("%w for %s: expected %s, got %s; the file was deleted and nothing was installed",
			ErrChecksum, path.Base(rel.URL), rel.SHA256, got)
	}
	return nil
}

type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	fn    Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.fn(p.done, p.total)
	return n, err
}

// Extract is extract for other packages (e.g. WordPress for `ebur new`).
func Extract(zipPath, dest, root string) error { return extract(zipPath, dest, root) }

// extract unpacks zipPath into dest. When root is set, only entries under
// that folder are extracted, with the folder stripped. Entries that would
// land outside dest (zip slip) fail the whole extraction.
func extract(zipPath, dest, root string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	prefix := ""
	if root != "" {
		prefix = strings.Trim(root, "/") + "/"
	}
	found := 0
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" {
			continue
		}
		clean := path.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.Contains(clean, ":") {
			return fmt.Errorf("unsafe path in archive: %q", f.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(clean))
		found++
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink in archive not allowed: %q", f.Name)
		}
		if err := writeEntry(f, target); err != nil {
			return err
		}
	}
	if found == 0 {
		if prefix != "" {
			return fmt.Errorf("folder %q not found in archive", root)
		}
		return errors.New("archive is empty")
	}
	return nil
}

func writeEntry(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// checkIDs stops runtime names and versions from being used as paths.
func checkIDs(runtime, version string) error {
	if !manifest.ValidName(runtime) {
		return fmt.Errorf("invalid runtime name %q", runtime)
	}
	_, err := manifest.ParseVersion(version)
	return err
}

// Installed lists installed versions of runtime, newest first.
func Installed(root paths.Root, runtime string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root.Bin(), runtime))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return manifest.CompareVersions(out[i], out[j]) > 0 })
	return out, nil
}

// Remove deletes an installed version. inUse reports whether a running
// site or service uses it; if so, Remove refuses (RUN-04).
func Remove(root paths.Root, runtime, version string, inUse func(runtime, version string) bool) error {
	if err := checkIDs(runtime, version); err != nil {
		return err
	}
	dir := root.RuntimeDir(runtime, version)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%s %s is not installed", runtime, version)
	}
	if inUse != nil && inUse(runtime, version) {
		return fmt.Errorf("%w: stop the sites using %s %s first", ErrInUse, runtime, version)
	}
	// Rename first so a half-deleted folder never looks like a valid install.
	trash := filepath.Join(root.Bin(), runtime, "."+version+".removing")
	os.RemoveAll(trash)
	if err := fsutil.Rename(dir, trash); err != nil {
		return err
	}
	return os.RemoveAll(trash)
}
