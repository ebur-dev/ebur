// Command manifest-update checks the official release sources and brings
// manifest/runtimes.json up to date under Ebur's rules (SEC-08). The
// scheduled workflow .github/workflows/manifest-update.yml runs it and
// opens a pull request; run it by hand from the repository root:
//
//	go run ./build/manifest-update            report only
//	go run ./build/manifest-update -write     also rewrite the manifest
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ebur-dev/ebur/core/manifest"
	"github.com/ebur-dev/ebur/internal/manifestbot"
)

func main() {
	path := flag.String("manifest", "manifest/runtimes.json", "manifest to update")
	write := flag.Bool("write", false, "rewrite the manifest when something changed")
	summary := flag.String("summary", "", "also write the report (Markdown) to this file")
	format := flag.Bool("format", false, "only rewrite the manifest in the bot's layout (no network)")
	flag.Parse()

	raw, err := os.ReadFile(*path)
	if err != nil {
		fail(err)
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		fail(err)
	}
	if *format {
		save(*path, m)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	now := time.Now()
	rep := manifestbot.Run(ctx, m, manifestbot.Defaults(m, now), get, now)
	text := rep.Markdown()
	fmt.Print(text)
	if *summary != "" {
		if err := os.WriteFile(*summary, []byte(text), 0o644); err != nil {
			fail(err)
		}
	}
	if !rep.Changed() || !*write {
		return
	}
	save(*path, m)
	fmt.Fprintf(os.Stderr, "wrote %s (%d change(s))\n", *path, len(rep.Applied))
}

// save writes the manifest in one stable layout (2-space JSON, runtimes in
// name order, newest release first), refusing anything Ebur would reject.
func save(path string, m *manifest.Manifest) {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fail(err)
	}
	out = append(out, '\n')
	if _, err := manifest.Parse(out); err != nil {
		fail(fmt.Errorf("the updated manifest does not validate: %w", err))
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fail(err)
	}
}

var client = &http.Client{Timeout: 60 * time.Second}

// get fetches a URL, authenticating to the GitHub API with GITHUB_TOKEN
// when set (the scheduled job has one; it raises the rate limit).
func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ebur-manifest-bot")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" && strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var b bytes.Buffer
	if _, err := io.Copy(&b, io.LimitReader(resp.Body, 32<<20)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "manifest-update:", err)
	os.Exit(1)
}
