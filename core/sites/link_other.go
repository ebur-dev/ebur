//go:build !windows

package sites

import "os"

func makeLink(link, target string) error { return os.Symlink(target, link) }

func longPath(p string) string { return p }

func samePath(a, b string) bool { return a == b }
