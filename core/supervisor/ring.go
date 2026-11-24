package supervisor

import (
	"strings"
	"sync"
)

// ring keeps the last n complete lines written to it, plus any
// unfinished line, for error reports (SVC-04).
type ring struct {
	mu      sync.Mutex
	n       int
	buf     []string
	partial string
}

func newRing(n int) *ring { return &ring{n: n} }

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.partial + strings.ReplaceAll(string(p), "\r\n", "\n")
	parts := strings.Split(s, "\n")
	r.partial = parts[len(parts)-1]
	if len(r.partial) > 4096 { // cap runaway lines with no newline
		r.partial = r.partial[len(r.partial)-4096:]
	}
	for _, l := range parts[:len(parts)-1] {
		r.buf = append(r.buf, l)
	}
	if over := len(r.buf) - r.n; over > 0 {
		r.buf = append([]string(nil), r.buf[over:]...)
	}
	return len(p), nil
}

func (r *ring) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.buf...)
	if r.partial != "" {
		out = append(out, r.partial)
		if len(out) > r.n {
			out = out[1:]
		}
	}
	return out
}

func (r *ring) reset() {
	r.mu.Lock()
	r.buf, r.partial = nil, ""
	r.mu.Unlock()
}
