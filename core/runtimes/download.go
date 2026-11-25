package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
)

// RUN-05: downloads resume. A download writes to <name>.part, with a
// <name>.part.src note naming the URL and checksum it belongs to. If the
// connection drops, Ebur retries a few times, asking the server for the
// rest with an HTTP Range request; a later install resumes the same way.
// The whole file is still checked against its SHA-256 before anything is
// installed, and a file that fails is deleted so the next try starts over.

// downloadRetries is how many times a dropped download is retried within
// one install. retryBackoff is the pause before retry n (tests shorten it).
var (
	downloadRetries = 3
	retryBackoff    = func(n int) time.Duration { return time.Duration(n) * 2 * time.Second }
)

// errRestart means the partial file cannot be continued (the server
// ignored the range or sent the wrong part): start from the beginning.
var errRestart = errors.New("restart download")

func removePart(part string) {
	os.Remove(part)
	os.Remove(part + ".src")
}

func (in *Installer) say(msg string) {
	if in.Say != nil {
		in.Say(msg)
	}
}

func (in *Installer) download(ctx context.Context, rel manifest.Release, part string, progress Progress) error {
	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}
	// A partial file is only continued if it belongs to this exact release.
	src := part + ".src"
	note := rel.URL + "\n" + rel.SHA256 + "\n"
	if b, err := os.ReadFile(src); err != nil || string(b) != note {
		os.Remove(part)
	}
	if err := os.WriteFile(src, []byte(note), 0o644); err != nil {
		return err
	}
	h := sha256.New()
	offset, err := hashFile(part, h)
	if err != nil {
		return err
	}
	if offset > 0 {
		in.say(fmt.Sprintf("Resuming the earlier download at %.1f MB...", float64(offset)/(1<<20)))
	}
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			wait := retryBackoff(attempt)
			in.say(fmt.Sprintf("The connection dropped at %.1f MB; retrying in %v (attempt %d of %d)...", float64(offset)/(1<<20), wait, attempt+1, downloadRetries+1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		n, err := in.fetch(ctx, client, rel.URL, part, offset, h, progress)
		if errors.Is(err, errRestart) {
			h.Reset()
			offset = 0
			os.Remove(part)
			n, err = in.fetch(ctx, client, rel.URL, part, 0, h, progress)
		}
		offset = n
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err() // cancelled: keep the partial file for next time
		}
		if !transient(err) || attempt >= downloadRetries {
			return fmt.Errorf("download %s: %w (the %.1f MB already downloaded is kept; installing again resumes it)", rel.URL, err, float64(offset)/(1<<20))
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != rel.SHA256 {
		removePart(part)
		return fmt.Errorf("%w for %s: expected %s, got %s; the file was deleted and nothing was installed",
			ErrChecksum, path.Base(rel.URL), rel.SHA256, got)
	}
	return nil
}

// fetch downloads from offset to the end, appending to part and feeding
// h. It returns the new length of part.
func (in *Installer) fetch(ctx context.Context, client *http.Client, url, part string, offset int64, h hash.Hash, progress Progress) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return offset, err
	}
	req.Header.Set("User-Agent", "ebur-runtime-manager")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := client.Do(req)
	if err != nil {
		return offset, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0:
		return offset, nil // the partial file is already complete; the checksum decides
	case resp.StatusCode == http.StatusOK && offset > 0:
		return offset, errRestart // the server does not resume: start over
	case resp.StatusCode == http.StatusPartialContent:
		if !strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-") {
			return offset, errRestart
		}
	case resp.StatusCode == http.StatusOK:
	default:
		return offset, httpError{resp.StatusCode, resp.Status}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if offset == 0 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return offset, fileError{err}
	}
	var body io.Reader = resp.Body
	if progress != nil {
		total := int64(-1)
		if resp.ContentLength >= 0 {
			total = offset + resp.ContentLength
		}
		body = &progressReader{r: resp.Body, done: offset, total: total, fn: progress}
	}
	n, copyErr := io.Copy(io.MultiWriter(f, h), body)
	if closeErr := f.Close(); closeErr != nil && copyErr == nil {
		copyErr = fileError{closeErr}
	}
	return offset + n, copyErr
}

// hashFile feeds an existing partial file to h and returns its size.
func hashFile(p string, h hash.Hash) (int64, error) {
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(h, f)
}

type httpError struct {
	code   int
	status string
}

func (e httpError) Error() string { return "server returned " + e.status }

type fileError struct{ err error }

func (e fileError) Error() string { return e.err.Error() }
func (e fileError) Unwrap() error { return e.err }

// transient reports whether a failed download is worth retrying: dropped
// or timed-out connections and server-side hiccups, not "not found" or
// a full disk.
func transient(err error) bool {
	var he httpError
	if errors.As(err, &he) {
		return he.code >= 500 || he.code == http.StatusTooManyRequests
	}
	var fe fileError
	if errors.As(err, &fe) {
		return false
	}
	var ne net.Error
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.As(err, &ne) ||
		strings.Contains(err.Error(), "connection reset") || strings.Contains(err.Error(), "forcibly closed")
}
