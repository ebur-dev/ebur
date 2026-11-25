package runtimes

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/core/paths"
)

// flakyServer serves body, dropping the connection halfway through the
// first `drops` requests. Range requests are honoured unless noRange.
type flakyServer struct {
	body    []byte
	drops   int
	noRange bool
	// stalls: for this many requests, send half and then wait until the
	// client gives up, so a cancel always lands mid-download.
	stalls int

	mu       sync.Mutex
	requests []string // Range header of each request ("" for none)
	sent     int      // body bytes written in total
}

func (s *flakyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Header.Get("Range"))
	drop := s.drops > 0
	if drop {
		s.drops--
	}
	stall := s.stalls > 0
	if stall {
		s.stalls--
	}
	s.mu.Unlock()
	if r.URL.Path == "/missing.zip" {
		http.NotFound(w, r)
		return
	}
	start := 0
	if rg := r.Header.Get("Range"); rg != "" && !s.noRange {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(s.body)-1)+"/"+strconv.Itoa(len(s.body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)-start))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
		w.WriteHeader(http.StatusOK)
	}
	rest := s.body[start:]
	if stall {
		half := rest[:len(rest)/2]
		w.Write(half)
		s.mu.Lock()
		s.sent += len(half)
		s.mu.Unlock()
		w.(http.Flusher).Flush()
		<-r.Context().Done() // until the client cancels
		return
	}
	if drop {
		half := rest[:len(rest)/2]
		w.Write(half)
		s.mu.Lock()
		s.sent += len(half)
		s.mu.Unlock()
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // the connection drops mid-download
	}
	w.Write(rest)
	s.mu.Lock()
	s.sent += len(rest)
	s.mu.Unlock()
}

func bigZip(t *testing.T) []byte {
	// Random-ish content that does not compress away, so "half" is real.
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString(strconv.Itoa(i * 7919))
	}
	return makeZip(t, map[string]string{"bin/tool.exe": b.String()})
}

func flakySetup(t *testing.T, s *flakyServer) (*Installer, string) {
	t.Helper()
	old := retryBackoff
	retryBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryBackoff = old })
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	root, _ := paths.New(t.TempDir())
	return &Installer{Root: root, Client: srv.Client()}, srv.URL
}

func TestResumeAfterDrop(t *testing.T) {
	z := bigZip(t)
	s := &flakyServer{body: z, drops: 1}
	in, url := flakySetup(t, s)
	var said []string
	in.Say = func(m string) { said = append(said, m) }
	if _, err := in.Install(context.Background(), "tool", manifest.Release{Version: "1.0", URL: url + "/t.zip", SHA256: sum(z)}, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 2 || s.requests[0] != "" || s.requests[1] != "bytes="+strconv.Itoa(len(z)/2)+"-" {
		t.Fatalf("requests %q", s.requests)
	}
	if s.sent != len(z) {
		t.Errorf("server sent %d bytes for a %d-byte file: the start was downloaded again", s.sent, len(z))
	}
	if len(said) != 1 || !strings.Contains(said[0], "retrying") {
		t.Errorf("messages %q", said)
	}
	if left, _ := os.ReadDir(in.Root.Downloads()); len(left) != 0 {
		t.Errorf("leftovers %v", left)
	}
}

func TestResumeOnNextInstall(t *testing.T) {
	z := bigZip(t)
	s := &flakyServer{body: z, stalls: 1}
	in, url := flakySetup(t, s)
	rel := manifest.Release{Version: "1.0", URL: url + "/t.zip", SHA256: sum(z)}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := in.Install(ctx, "tool", rel, func(done, total int64) {
		if done > total/3 {
			cancel() // the user pressed Ctrl+C
		}
	})
	if err == nil {
		t.Fatal("cancelled install succeeded")
	}
	part := filepath.Join(in.Root.Downloads(), "tool-1.0.zip.part")
	fi, err := os.Stat(part)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("partial download not kept: %v", err)
	}
	var said []string
	in.Say = func(m string) { said = append(said, m) }
	var first int64 = -1
	if _, err := in.Install(context.Background(), "tool", rel, func(done, total int64) {
		if first < 0 {
			first = done
		}
	}); err != nil {
		t.Fatal(err)
	}
	if last := s.requests[len(s.requests)-1]; !strings.HasPrefix(last, "bytes=") {
		t.Errorf("second install did not resume: %q", s.requests)
	}
	if first < fi.Size() || len(said) == 0 || !strings.Contains(said[0], "Resuming") {
		t.Errorf("progress started at %d (partial was %d), messages %q", first, fi.Size(), said)
	}
}

func TestServerWithoutRangeRestarts(t *testing.T) {
	z := bigZip(t)
	s := &flakyServer{body: z, drops: 1, noRange: true}
	in, url := flakySetup(t, s)
	if _, err := in.Install(context.Background(), "tool", manifest.Release{Version: "1.0", URL: url + "/t.zip", SHA256: sum(z)}, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := Installed(in.Root, "tool"); len(v) != 1 {
		t.Fatal("not installed")
	}
}

func TestPartialOfAnotherReleaseIsDiscarded(t *testing.T) {
	z := bigZip(t)
	s := &flakyServer{body: z}
	in, url := flakySetup(t, s)
	os.MkdirAll(in.Root.Downloads(), 0o755)
	part := filepath.Join(in.Root.Downloads(), "tool-1.0.zip.part")
	os.WriteFile(part, []byte("from another URL"), 0o644)
	os.WriteFile(part+".src", []byte("https://elsewhere/old.zip\nabc\n"), 0o644)
	if _, err := in.Install(context.Background(), "tool", manifest.Release{Version: "1.0", URL: url + "/t.zip", SHA256: sum(z)}, nil); err != nil {
		t.Fatal(err)
	}
	if s.requests[0] != "" {
		t.Errorf("resumed a partial from another release: %q", s.requests)
	}
}

func TestCorruptPartialIsDeleted(t *testing.T) {
	z := bigZip(t)
	s := &flakyServer{body: z}
	in, url := flakySetup(t, s)
	rel := manifest.Release{Version: "1.0", URL: url + "/t.zip", SHA256: sum(z)}
	os.MkdirAll(in.Root.Downloads(), 0o755)
	part := filepath.Join(in.Root.Downloads(), "tool-1.0.zip.part")
	os.WriteFile(part, bytes.Repeat([]byte{'x'}, 1000), 0o644) // right note, wrong bytes
	os.WriteFile(part+".src", []byte(rel.URL+"\n"+rel.SHA256+"\n"), 0o644)
	_, err := in.Install(context.Background(), "tool", rel, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("corrupt partial kept")
	}
	if _, err := in.Install(context.Background(), "tool", rel, nil); err != nil {
		t.Fatalf("fresh retry failed: %v", err)
	}
}

func TestNotFoundIsNotRetried(t *testing.T) {
	s := &flakyServer{body: []byte("x")}
	in, url := flakySetup(t, s)
	_, err := in.Install(context.Background(), "tool", manifest.Release{Version: "1.0", URL: url + "/missing.zip", SHA256: strings.Repeat("0", 64)}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") || len(s.requests) != 1 {
		t.Fatalf("err %v, requests %d", err, len(s.requests))
	}
}

func TestGivesUpAfterRetries(t *testing.T) {
	z := bigZip(t)
	s := &flakyServer{body: z, drops: 99}
	in, url := flakySetup(t, s)
	_, err := in.Install(context.Background(), "tool", manifest.Release{Version: "1.0", URL: url + "/t.zip", SHA256: sum(z)}, nil)
	if err == nil || !strings.Contains(err.Error(), "installing again resumes it") {
		t.Fatalf("err %v", err)
	}
	if len(s.requests) != downloadRetries+1 {
		t.Errorf("%d requests, want %d", len(s.requests), downloadRetries+1)
	}
}
