// Package php builds the php-cgi worker pool for one PHP version.
//
// php-cgi on Windows serves one request at a time and exits after
// PHP_FCGI_MAX_REQUESTS, so a version is served by several workers on
// consecutive ports behind an Nginx upstream. Each worker gets a
// different request limit: in spikes/phppool, equal limits made the whole
// pool expire at once and Nginx returned 502s.
package php

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ebur/ebur/core/supervisor"
)

// DefaultMaxRequests is the base request limit per worker.
const DefaultMaxRequests = 500

type Pool struct {
	Version     string // e.g. "8.4.28"
	Dir         string // PHP install folder holding php-cgi.exe
	INI         string // php.ini to use (-c); empty uses PHP's own lookup
	BasePort    int
	Size        int
	MaxRequests int    // base limit; 0 means DefaultMaxRequests
	LogDir      string // where worker logs go; empty disables logs
}

// Name is the service and upstream name for the pool, e.g. "php-8.4.28".
func (p Pool) Name() string { return "php-" + p.Version }

// Ports lists the worker ports.
func (p Pool) Ports() []int {
	out := make([]int, p.Size)
	for i := range out {
		out[i] = p.BasePort + i
	}
	return out
}

// Specs returns one supervisor spec per worker.
func (p Pool) Specs() ([]supervisor.Spec, error) {
	if p.Size < 1 || p.Size > 64 {
		return nil, fmt.Errorf("php pool size %d out of range 1-64", p.Size)
	}
	if p.BasePort < 1024 || p.BasePort+p.Size-1 > 65535 {
		return nil, fmt.Errorf("php pool ports %d-%d out of range", p.BasePort, p.BasePort+p.Size-1)
	}
	base := p.MaxRequests
	if base <= 0 {
		base = DefaultMaxRequests
	}
	specs := make([]supervisor.Spec, 0, p.Size)
	for i, port := range p.Ports() {
		args := []string{"-b", "127.0.0.1:" + strconv.Itoa(port)}
		if p.INI != "" {
			args = append(args, "-c", p.INI)
		}
		s := supervisor.Spec{
			Name: fmt.Sprintf("%s#%d", p.Name(), i+1),
			Path: filepath.Join(p.Dir, "php-cgi.exe"),
			Args: args,
			Env: []string{
				// Stagger limits by a quarter of the base per worker so the
				// pool never expires all at once.
				"PHP_FCGI_MAX_REQUESTS=" + strconv.Itoa(base+i*base/4),
			},
			Dir: p.Dir,
			// php-cgi exiting at its request limit is normal, so the crash
			// counter must reset quickly: only rapid repeated exits fail.
			StableAfter: 2 * time.Second,
		}
		if p.LogDir != "" {
			s.LogFile = filepath.Join(p.LogDir, p.Name()+".log")
		}
		specs = append(specs, s)
	}
	return specs, nil
}

// NginxUpstream renders the upstream block for this pool. least_conn
// sends requests to idle workers; max_fails=0 stops Nginx from marking a
// worker dead while it restarts, so it simply tries the next one.
func (p Pool) NginxUpstream() string {
	var b strings.Builder
	fmt.Fprintf(&b, "upstream %s {\n    least_conn;\n", strings.ReplaceAll(p.Name(), ".", "_"))
	for _, port := range p.Ports() {
		fmt.Fprintf(&b, "    server 127.0.0.1:%d max_fails=0;\n", port)
	}
	b.WriteString("}\n")
	return b.String()
}
